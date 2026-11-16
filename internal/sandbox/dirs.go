package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EnsureDirs makes sure sandbox mount directories exist. In production the
// services may not write to the root-owned parents of workspaces and team
// folders (so they cannot swap a mount source for a link), and the Docker
// proxy creates the directories on request instead.
func (m *Manager) EnsureDirs(ctx context.Context, paths ...string) error {
	var missing []string
	for _, dir := range paths {
		info, err := os.Lstat(dir)
		if err == nil && info.IsDir() {
			continue
		}
		if err == nil {
			return fmt.Errorf("%s exists but is not a directory", dir)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if mkErr := os.MkdirAll(dir, 0o777); mkErr == nil {
			_ = os.Chmod(dir, 0o777)
			continue
		} else if !errors.Is(mkErr, fs.ErrPermission) {
			return fmt.Errorf("create %s: %w", dir, mkErr)
		}
		missing = append(missing, filepath.ToSlash(dir))
	}
	if len(missing) == 0 {
		return nil
	}
	return createDirsViaProxy(ctx, missing)
}

// createDirsViaProxy asks the Docker proxy named by DOCKER_HOST to create
// the directories.
func createDirsViaProxy(ctx context.Context, paths []string) error {
	host := strings.TrimSpace(os.Getenv("DOCKER_HOST"))
	if !strings.HasPrefix(host, "tcp://") {
		return fmt.Errorf("cannot create %s: permission denied and no Docker proxy is configured", strings.Join(paths, ", "))
	}
	endpoint := (&url.URL{Scheme: "http", Host: strings.TrimPrefix(host, "tcp://"), Path: "/superbot/dirs"}).String()
	body, _ := json.Marshal(map[string]any{"paths": paths})
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("ask docker proxy to create directories: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("docker proxy refused to create directories: %s", strings.TrimSpace(string(message)))
	}
	return nil
}

// clearDir removes everything inside dir but keeps dir itself, whose parent
// the services may not be allowed to write to.
func clearDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
