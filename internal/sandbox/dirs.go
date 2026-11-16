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
	return dirsViaProxy(ctx, http.MethodPost, missing)
}

// RemoveDirs deletes the workspace directories of deleted bots. Their
// contents belong to the sandbox user, so the worker clears them; the empty
// directory sits in a root-owned parent, so the Docker proxy removes it.
func (m *Manager) RemoveDirs(ctx context.Context, paths ...string) error {
	var leftover []string
	for _, dir := range paths {
		if err := clearDir(dir); err != nil {
			return err
		}
		err := os.Remove(dir)
		switch {
		case err == nil, errors.Is(err, fs.ErrNotExist):
		case errors.Is(err, fs.ErrPermission):
			leftover = append(leftover, filepath.ToSlash(dir))
		default:
			return err
		}
	}
	if len(leftover) == 0 {
		return nil
	}
	return dirsViaProxy(ctx, http.MethodDelete, leftover)
}

// dirsViaProxy asks the Docker proxy named by DOCKER_HOST to create (POST)
// or remove (DELETE) the directories.
func dirsViaProxy(ctx context.Context, method string, paths []string) error {
	verb := "create"
	if method == http.MethodDelete {
		verb = "remove"
	}
	host := strings.TrimSpace(os.Getenv("DOCKER_HOST"))
	if !strings.HasPrefix(host, "tcp://") {
		return fmt.Errorf("cannot %s %s: permission denied and no Docker proxy is configured", verb, strings.Join(paths, ", "))
	}
	endpoint := (&url.URL{Scheme: "http", Host: strings.TrimPrefix(host, "tcp://"), Path: "/superbot/dirs"}).String()
	body, _ := json.Marshal(map[string]any{"paths": paths})
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("ask docker proxy to %s directories: %w", verb, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("docker proxy refused to %s directories: %s", verb, strings.TrimSpace(string(message)))
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
		if err := forceRemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// forceRemoveAll is os.RemoveAll that also removes read-only directories the
// caller owns, such as a Go module cache a bot downloaded.
func forceRemoveAll(target string) error {
	err := os.RemoveAll(target)
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		return err
	}
	_ = filepath.WalkDir(target, func(item string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
			_ = os.Chmod(item, 0o700)
		}
		return nil
	})
	return os.RemoveAll(target)
}
