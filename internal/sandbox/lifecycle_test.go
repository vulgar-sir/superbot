package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaxRunningComputersHonoursOverride(t *testing.T) {
	t.Setenv("SUPERBOT_MAX_RUNNING_COMPUTERS", "3")
	if got := maxRunningComputers(); got != 3 {
		t.Fatalf("want 3, got %d", got)
	}
	t.Setenv("SUPERBOT_MAX_RUNNING_COMPUTERS", "0")
	if got := maxRunningComputers(); got != 0 {
		t.Fatalf("0 disables the cap, got %d", got)
	}
	t.Setenv("SUPERBOT_MAX_RUNNING_COMPUTERS", "")
	if got := maxRunningComputers(); got < 2 || got > 32 {
		t.Fatalf("derived cap out of range: %d", got)
	}
}

func TestDirSizeCountsFilesAndStopsAtLimit(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, size := range map[string]int{"one": 100, "a/two": 200, "a/b/three": 300} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	size, err := dirSize(dir, 1000)
	if err != nil || size != 600 {
		t.Fatalf("want 600, got %d (%v)", size, err)
	}
	if size, err := dirSize(dir, 2); err != nil || size >= 600 {
		t.Fatalf("the walk must stop at the entry limit, got %d (%v)", size, err)
	}
	if _, err := dirSize(filepath.Join(dir, "missing"), 10); !os.IsNotExist(err) {
		t.Fatalf("missing dir: want not-exist, got %v", err)
	}
}

func TestClearDirRemovesReadOnlyTrees(t *testing.T) {
	dir := t.TempDir()
	locked := filepath.Join(dir, "gomodcache", "pkg")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "mod.go"), []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := clearDir(dir); err != nil {
		t.Fatalf("clearDir: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("dir should be empty, has %d entries", len(entries))
	}
}
