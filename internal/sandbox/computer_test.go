package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGateTokenIsPerBotAndStable(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	a, b := uuid.New(), uuid.New()
	if GateToken(key, a) != GateToken(key, a) {
		t.Fatal("token must be deterministic: the API and the worker derive it separately")
	}
	if GateToken(key, a) == GateToken(key, b) {
		t.Fatal("two bots must not share a gate token")
	}
	if GateToken(bytes.Repeat([]byte{8}, 32), a) == GateToken(key, a) {
		t.Fatal("token must depend on the server key")
	}
	if got := GateToken(key, a); len(got) != 40 || strings.Trim(got, "0123456789abcdef") != "" {
		t.Fatalf("token %q is not 40 hex characters", got)
	}
	if GateToken(nil, a) != "" {
		t.Fatal("no key must give no token (the gate then refuses everything)")
	}
}

func TestInfoGateHeaders(t *testing.T) {
	if got := (Info{GateToken: "abc"}).GateHeaders().Get(GateHeader); got != "abc" {
		t.Fatalf("header = %q", got)
	}
	if len((Info{}).GateHeaders()) != 0 {
		t.Fatal("no token, no header")
	}
}

func envValue(env []string, key string) (string, bool) {
	for _, item := range env {
		if k, v, ok := strings.Cut(item, "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

func TestCreateOptionsComputerEnvironment(t *testing.T) {
	spec := ContainerSpec{Image: "img", DesktopImage: "desk", BotID: uuid.New(), Profile: "desktop",
		Workspace: "/data/workspaces/x", GateToken: "secret-token"}
	opts := spec.CreateOptions()
	env := opts.Config.Env
	for key, want := range map[string]string{
		"HOME":                      HomeDir,
		"SUPERBOT_GATE_TOKEN":       "secret-token",
		"XDG_CACHE_HOME":            "/tmp/.cache",
		"PIP_BREAK_SYSTEM_PACKAGES": "1",
		"NPM_CONFIG_PREFIX":         HomeDir + "/.local",
	} {
		if got, ok := envValue(env, key); !ok || got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	path, _ := envValue(env, "PATH")
	if !strings.HasPrefix(path, HomeDir+"/.local/bin:"+UserAptRoot+"/usr/bin:") || !strings.Contains(path, ":/usr/bin:") {
		t.Errorf("PATH = %q: the bot's programs must come first and the system's must stay", path)
	}
	if opts.HostConfig.Init == nil || !*opts.HostConfig.Init {
		t.Error("computers run an init as PID 1 so detached programs are reaped")
	}
	if opts.Config.Labels[mountsVersionKey] != mountsVersion || mountsVersion != "5" {
		t.Errorf("mounts label = %q; existing computers must be rebuilt once for the new home and gate", opts.Config.Labels[mountsVersionKey])
	}
	if !strings.Contains(opts.HostConfig.Tmpfs["/tmp"], "exec") {
		t.Error("/tmp must allow exec (AppImages unpack there)")
	}
}

func TestCreateOptionsShellSandboxHasNoGateToken(t *testing.T) {
	spec := ContainerSpec{Image: "img", BotID: uuid.New(), Profile: "none", Workspace: "/w", GateToken: "secret"}
	env := spec.CreateOptions().Config.Env
	if _, ok := envValue(env, "SUPERBOT_GATE_TOKEN"); ok {
		t.Fatal("a computer without network has no gate")
	}
	if home, _ := envValue(env, "HOME"); home != HomeDir {
		t.Fatalf("HOME = %q", home)
	}
}

func TestExecEnvironmentMatchesContainer(t *testing.T) {
	env := computerEnv()
	if home, _ := envValue(env, "HOME"); home != HomeDir {
		t.Fatalf("exec HOME = %q; a stale /tmp home would lose installed programs", home)
	}
	if slices.ContainsFunc(env, func(item string) bool { return strings.HasPrefix(item, "SUPERBOT_GATE_TOKEN=") }) {
		t.Fatal("the gate token belongs to the container, not to every exec")
	}
}

type tarEntry struct {
	name     string
	typeflag byte
	mode     int64
	body     string
	link     string
}

func writeSnapshot(t *testing.T, entries []tarEntry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		header := &tar.Header{Name: e.name, Typeflag: e.typeflag, Mode: e.mode, Linkname: e.link,
			Size: int64(len(e.body)), ModTime: time.Now()}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if e.body != "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "snap.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractSnapshotRestoresLinksAndModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	workspace := t.TempDir()
	outside := t.TempDir()
	archive := writeSnapshot(t, []tarEntry{
		{name: ".home/.local/apt/usr/bin/", typeflag: tar.TypeDir, mode: 0o755},
		{name: ".home/.local/apt/usr/bin/tool", typeflag: tar.TypeReg, mode: 0o755, body: "#!/bin/sh\n"},
		{name: ".home/.local/apt/usr/lib/libx.so.1", typeflag: tar.TypeSymlink, link: "libx.so.1.2"},
		{name: ".home/.local/bin/abs", typeflag: tar.TypeSymlink, link: "/usr/bin/true"},
		// A link to a directory outside, then a file "through" it: the file
		// must never land outside the workspace.
		{name: "escape", typeflag: tar.TypeSymlink, link: outside},
		{name: "escape/owned", typeflag: tar.TypeReg, mode: 0o644, body: "x"},
		{name: "../evil", typeflag: tar.TypeReg, mode: 0o644, body: "x"},
		{name: "notes.txt", typeflag: tar.TypeReg, mode: 0o644, body: "hello"},
	})
	if err := extractSnapshot(archive, workspace); err != nil {
		t.Fatalf("extract: %v", err)
	}
	info, err := os.Stat(filepath.Join(workspace, ".home/.local/apt/usr/bin/tool"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("executable lost its mode: %v %v", info, err)
	}
	if target, err := os.Readlink(filepath.Join(workspace, ".home/.local/apt/usr/lib/libx.so.1")); err != nil || target != "libx.so.1.2" {
		t.Fatalf("library link = %q, %v", target, err)
	}
	if target, err := os.Readlink(filepath.Join(workspace, ".home/.local/bin/abs")); err != nil || target != "/usr/bin/true" {
		t.Fatalf("absolute link = %q, %v", target, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "owned")); err == nil {
		t.Fatal("a file was written through a link outside the workspace")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(workspace), "evil")); err == nil {
		t.Fatal("a .. entry escaped the workspace")
	}
	if data, _ := os.ReadFile(filepath.Join(workspace, "notes.txt")); string(data) != "hello" {
		t.Fatalf("notes = %q", data)
	}
}

func TestJobReplies(t *testing.T) {
	reply, err := parseJobsReply("profile noise\n{\"error\":\"too_many\",\"running\":[\"job-a\",\"job-b\"]}\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := startedJob(reply); err == nil || !strings.Contains(err.Error(), "job-a, job-b") {
		t.Fatalf("too many jobs: %v", err)
	}
	reply, _ = parseJobsReply(`{"job":{"id":"job-1","state":"running","timeout":7200}}`)
	if job, err := startedJob(reply); err != nil || job.ID != "job-1" || job.State != "running" {
		t.Fatalf("started = %+v, %v", job, err)
	}
	reply, _ = parseJobsReply(`{"jobs":[{"id":"job-1","state":"exited","exit_code":3,"log_tail":"done"}]}`)
	jobs, err := listedJobs(reply)
	if err != nil || len(jobs) != 1 || jobs[0].ExitCode == nil || *jobs[0].ExitCode != 3 {
		t.Fatalf("jobs = %+v, %v", jobs, err)
	}
	reply, _ = parseJobsReply(`{"error":"not_found"}`)
	if _, err := listedJobs(reply); err != ErrJobNotFound {
		t.Fatalf("not found = %v", err)
	}
	if _, err := stoppedJob(reply); err != ErrJobNotFound {
		t.Fatalf("stop not found = %v", err)
	}
	if n, err := parseRunningCount("{\"running\": 2}\n"); err != nil || n != 2 {
		t.Fatalf("count = %d, %v", n, err)
	}
	if _, err := parseRunningCount("oops"); err == nil {
		t.Fatal("garbage must not count as zero jobs")
	}
}

func TestJobArguments(t *testing.T) {
	args := startJobArgs("make", "build", 0)
	if args[0] != "start" || !ValidJobID(args[1]) || args[2] != "7200" || args[4] != "build" || args[5] != "make" {
		t.Fatalf("args = %q", args)
	}
	if got := startJobArgs("x", "", 100*time.Hour)[2]; got != "43200" {
		t.Fatalf("timeout not capped at 12h: %s", got)
	}
	for _, bad := range []string{"", "../x", "job id", "A", strings.Repeat("a", 41)} {
		if ValidJobID(bad) {
			t.Errorf("ValidJobID(%q) = true", bad)
		}
	}
	if _, err := statusArgs("../etc", 10); err != ErrJobNotFound {
		t.Fatal("an invalid id must never reach the computer")
	}
	if args, _ := statusArgs("", 10); len(args) != 1 {
		t.Fatalf("list args = %q", args)
	}
}

func TestExecKillSparesJobRunners(t *testing.T) {
	if !strings.Contains(execKillScript, "/tmp/.sbjobs/") || !strings.Contains(execKillScript, `case "$jobs" in`) {
		t.Fatal("the orphan sweep of a cancelled exec must skip background job runners")
	}
	if !strings.Contains(jobsScript, `env.pop("SUPERBOT_EXEC_MARK", None)`) {
		t.Fatal("jobs must not carry the exec marker of the call that started them")
	}
}
