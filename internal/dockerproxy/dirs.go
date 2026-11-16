package dockerproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// The only host directories a sandbox may mount, relative to the bind root:
// a bot's workspace, a team's shared folder and the account's shared drive.
var allowedSources = []*regexp.Regexp{
	regexp.MustCompile(`^/workspaces/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`),
	regexp.MustCompile(`^/teams/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/shared$`),
	regexp.MustCompile(`^/shared/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`),
}

const dirsPath = "/superbot/dirs"

// removableSource is the only directory shape the worker may ask to remove:
// the workspace of a deleted bot, once it has emptied it.
var removableSource = regexp.MustCompile(`^/workspaces/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (p *policy) allowedSource(src string) bool {
	rel := strings.TrimPrefix(src, p.bindRoot)
	for _, pattern := range allowedSources {
		if pattern.MatchString(rel) {
			return true
		}
	}
	return false
}

// checkNoLinks refuses a bind source when any path component below the bind
// root is a symbolic link. Docker resolves the source on the host every time
// it starts the container, including automatic restarts that never pass
// through this proxy, so a link planted in place of a workspace would mount
// arbitrary host paths. The parent directories are root-owned and not
// writable by the services, so a path that is link-free now stays so.
func (p *policy) checkNoLinks(src string) error {
	if !p.verifyPaths {
		return nil
	}
	current := p.bindRoot
	for _, part := range strings.Split(strings.TrimPrefix(src, p.bindRoot+"/"), "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return deny("bind source %q does not exist; create it through %s first", src, dirsPath)
		}
		if err != nil {
			return fmt.Errorf("inspect bind source: %w", err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return deny("bind source %q contains a symbolic link", src)
		}
		if !info.IsDir() {
			return deny("bind source %q is not a directory", src)
		}
	}
	return nil
}

// handleDirs creates sandbox mount directories on behalf of the worker,
// which cannot write to the root-owned parents itself. Only the two allowed
// shapes are accepted, and the leaf is made writable for the sandbox user.
func (p *Proxy) handleDirs(w http.ResponseWriter, r *http.Request) {
	if !p.policy.verifyPaths {
		writeError(w, http.StatusServiceUnavailable, "superbot docker proxy: the data directory is not mounted in the proxy")
		return
	}
	var req struct {
		Paths []string `json:"paths"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil || json.Unmarshal(body, &req) != nil || len(req.Paths) == 0 || len(req.Paths) > 64 {
		writeError(w, http.StatusBadRequest, "superbot docker proxy: expected {\"paths\": [...]}")
		return
	}
	created := 0
	for _, src := range req.Paths {
		if path.Clean(src) != src || !strings.HasPrefix(src, p.policy.bindRoot+"/") || !p.policy.allowedSource(src) {
			p.logger.Warn("docker proxy refused directory", "path", src)
			writeError(w, http.StatusForbidden, fmt.Sprintf("superbot docker proxy: %q is not a sandbox directory", src))
			return
		}
		made, err := p.policy.makeSourceDir(src)
		if err != nil {
			var d *denial
			if errors.As(err, &d) {
				writeError(w, http.StatusForbidden, "superbot docker proxy: "+d.reason)
				return
			}
			writeError(w, http.StatusInternalServerError, "superbot docker proxy: "+err.Error())
			return
		}
		created += made
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"created": created})
}

// handleRemoveDirs removes empty workspace directories of deleted bots. The
// worker empties them itself (it owns the contents) but cannot remove the
// directory from its root-owned parent. Only empty directories go: os.Remove
// refuses anything else, so this cannot delete a bot's files.
func (p *Proxy) handleRemoveDirs(w http.ResponseWriter, r *http.Request) {
	if !p.policy.verifyPaths {
		writeError(w, http.StatusServiceUnavailable, "superbot docker proxy: the data directory is not mounted in the proxy")
		return
	}
	var req struct {
		Paths []string `json:"paths"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil || json.Unmarshal(body, &req) != nil || len(req.Paths) == 0 || len(req.Paths) > 64 {
		writeError(w, http.StatusBadRequest, "superbot docker proxy: expected {\"paths\": [...]}")
		return
	}
	removed := 0
	for _, src := range req.Paths {
		rel := strings.TrimPrefix(src, p.policy.bindRoot)
		if path.Clean(src) != src || !strings.HasPrefix(src, p.policy.bindRoot+"/") || !removableSource.MatchString(rel) {
			p.logger.Warn("docker proxy refused to remove directory", "path", src)
			writeError(w, http.StatusForbidden, fmt.Sprintf("superbot docker proxy: %q is not a workspace directory", src))
			return
		}
		info, err := os.Lstat(src)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "superbot docker proxy: "+err.Error())
			return
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			writeError(w, http.StatusForbidden, fmt.Sprintf("superbot docker proxy: %q is not a plain directory", src))
			return
		}
		if err := os.Remove(src); err != nil && !errors.Is(err, fs.ErrNotExist) {
			writeError(w, http.StatusConflict, "superbot docker proxy: "+err.Error())
			return
		}
		removed++
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"removed": removed})
}

// makeSourceDir creates the missing components of src. Intermediate
// directories stay root-owned and read-only to others; the leaf is open to
// the sandbox user and the services.
func (p *policy) makeSourceDir(src string) (int, error) {
	parts := strings.Split(strings.TrimPrefix(src, p.bindRoot+"/"), "/")
	current := p.bindRoot
	created := 0
	for index, part := range parts {
		current = filepath.Join(current, part)
		leaf := index == len(parts)-1
		info, err := os.Lstat(current)
		switch {
		case err == nil:
			if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
				return created, deny("%q is not a plain directory", current)
			}
			continue
		case !errors.Is(err, fs.ErrNotExist):
			return created, err
		}
		mode := fs.FileMode(0o755)
		if leaf {
			mode = 0o777
		}
		if err := os.Mkdir(current, mode); err != nil && !errors.Is(err, fs.ErrExist) {
			return created, err
		}
		if err := os.Chmod(current, mode); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}
