package dockerproxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testBot = "11111111-2222-3333-4444-555555555555"

func dirsProxy(t *testing.T) (*Proxy, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bind roots are Linux host paths")
	}
	root := t.TempDir()
	proxy, err := New(Config{
		Socket: filepath.Join(t.TempDir(), "docker.sock"), Listen: "127.0.0.1:0",
		Images: defaultImages, Networks: defaultNetworks, BindRoot: root,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}
	return proxy, root
}

func postDirs(proxy *Proxy, paths ...string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]any{"paths": paths})
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, dirsPath, bytes.NewReader(body)))
	return recorder
}

func TestDirsCreatesOnlySandboxDirectories(t *testing.T) {
	proxy, root := dirsProxy(t)
	workspace := root + "/workspaces/" + testBot
	shared := root + "/teams/" + testBot + "/shared"
	drive := root + "/shared/" + testBot
	if res := postDirs(proxy, workspace, shared, drive); res.Code != http.StatusOK {
		t.Fatalf("status %d: %s", res.Code, res.Body.String())
	}
	for path, mode := range map[string]os.FileMode{
		workspace:                  0o777,
		drive:                      0o777,
		root + "/shared":           0o755,
		shared:                     0o777,
		root + "/teams/" + testBot: 0o755,
		root + "/workspaces":       0o755,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s was not created: %v", path, err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("%s mode = %v, want %v", path, info.Mode().Perm(), mode)
		}
	}
	if res := postDirs(proxy, workspace); res.Code != http.StatusOK {
		t.Fatalf("creating an existing directory must be a no-op, got %d", res.Code)
	}
	for _, bad := range []string{
		root + "/library",
		root + "/workspaces/not-a-uuid",
		root + "/workspaces/" + testBot + "/sub",
		root + "/teams/" + testBot,
		root + "/workspaces/../etc",
		"/etc/" + testBot,
	} {
		if res := postDirs(proxy, bad); res.Code != http.StatusForbidden {
			t.Fatalf("%s: status %d, want 403", bad, res.Code)
		}
	}
	res := httptest.NewRecorder()
	proxy.ServeHTTP(res, httptest.NewRequest(http.MethodGet, dirsPath, nil))
	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET %s = %d", dirsPath, res.Code)
	}
}

func TestBindSourceWithSymlinkIsRefused(t *testing.T) {
	proxy, root := dirsProxy(t)
	if err := os.MkdirAll(root+"/workspaces", 0o755); err != nil {
		t.Fatal(err)
	}
	// A workspace replaced by a link to the host root.
	if err := os.Symlink("/", root+"/workspaces/"+testBot); err != nil {
		t.Fatal(err)
	}
	if err := proxy.policy.checkSource(root + "/workspaces/" + testBot); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("a linked workspace must be refused, got %v", err)
	}
	if res := postDirs(proxy, root+"/workspaces/"+testBot); res.Code != http.StatusForbidden {
		t.Fatalf("the directory endpoint must not accept a link, got %d", res.Code)
	}
	// A linked parent is refused as well.
	if err := os.Symlink("/tmp", root+"/teams"); err != nil {
		t.Fatal(err)
	}
	if err := proxy.policy.checkSource(root + "/teams/" + testBot + "/shared"); err == nil {
		t.Fatal("a source below a linked parent must be refused")
	}
	if err := proxy.policy.checkSource(root + "/workspaces/22222222-2222-3333-4444-555555555555"); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("a missing source must be refused rather than created by Docker, got %v", err)
	}
}

func deleteDirs(proxy *Proxy, paths ...string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]any{"paths": paths})
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, dirsPath, bytes.NewReader(body)))
	return recorder
}

func TestRemoveDirsRemovesOnlyEmptyWorkspaces(t *testing.T) {
	proxy, root := dirsProxy(t)
	workspace := root + "/workspaces/" + testBot
	drive := root + "/shared/" + testBot
	shared := root + "/teams/" + testBot + "/shared"
	if res := postDirs(proxy, workspace, drive, shared); res.Code != http.StatusOK {
		t.Fatalf("create: %d %s", res.Code, res.Body.String())
	}

	// A workspace with files is never removed: the worker empties it first.
	if err := os.WriteFile(workspace+"/keep.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := deleteDirs(proxy, workspace); res.Code != http.StatusConflict {
		t.Fatalf("non-empty workspace: want 409, got %d %s", res.Code, res.Body.String())
	}
	if _, err := os.Stat(workspace + "/keep.txt"); err != nil {
		t.Fatalf("files must survive: %v", err)
	}

	// Drives, team folders, parents and traversal are refused.
	for _, target := range []string{drive, shared, root + "/workspaces", workspace + "/../" + testBot, "/etc"} {
		if res := deleteDirs(proxy, target); res.Code != http.StatusForbidden {
			t.Fatalf("%s: want 403, got %d %s", target, res.Code, res.Body.String())
		}
	}

	if err := os.Remove(workspace + "/keep.txt"); err != nil {
		t.Fatal(err)
	}
	if res := deleteDirs(proxy, workspace); res.Code != http.StatusOK {
		t.Fatalf("empty workspace: %d %s", res.Code, res.Body.String())
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("workspace should be gone: %v", err)
	}
	// Removing again is not an error.
	if res := deleteDirs(proxy, workspace); res.Code != http.StatusOK {
		t.Fatalf("second remove: %d %s", res.Code, res.Body.String())
	}
	// A symlink in place of a workspace is refused, not followed.
	if err := os.Symlink(drive, workspace); err != nil {
		t.Fatal(err)
	}
	if res := deleteDirs(proxy, workspace); res.Code != http.StatusForbidden {
		t.Fatalf("symlink: want 403, got %d %s", res.Code, res.Body.String())
	}
	if _, err := os.Stat(drive); err != nil {
		t.Fatalf("drive must survive: %v", err)
	}
}
