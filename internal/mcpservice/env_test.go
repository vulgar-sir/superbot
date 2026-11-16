package mcpservice

import (
	"reflect"
	"testing"
)

func TestChildEnvNameAllowed(t *testing.T) {
	allowed := []string{
		"PATH", "Path", "HOME", "USER", "USERNAME", "LANG", "LC_ALL", "LC_CTYPE", "TZ",
		"TMP", "TEMP", "TMPDIR", "SystemRoot", "SYSTEMROOT", "ComSpec", "PATHEXT",
		"APPDATA", "LOCALAPPDATA", "USERPROFILE", "HOMEDRIVE", "HOMEPATH",
		"ProgramFiles", "ProgramFiles(x86)", "ProgramFilesW6432",
		"NODE_PATH", "PYTHONPATH",
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
	}
	for _, name := range allowed {
		if !childEnvNameAllowed(name) {
			t.Errorf("%s should be allowed", name)
		}
	}
	denied := []string{
		"", "SUPERBOT_MASTER_KEY", "SUPERBOT_DATA_DIR", "superbot_admin_password",
		"DATABASE_URL", "POSTGRES_PASSWORD", "ADMIN_PASSWORD", "OPENAI_API_KEY",
		"ANTHROPIC_API_KEY", "GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY", "AWS_ACCESS_KEY_ID",
		"PG_DSN", "SSH_PRIVATE_KEY", "MY_APIKEY", "DOCKER_HOST", "GOPATH", "SSH_AUTH_SOCK",
		// Allowlist prefixes must not rescue a credential-shaped name.
		"LC_SECRET", "ProgramFiles_Token", "PATH_PASSWORD",
	}
	for _, name := range denied {
		if childEnvNameAllowed(name) {
			t.Errorf("%s should be denied", name)
		}
	}
}

func TestBuildChildEnvFiltersParent(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin",
		"SUPERBOT_MASTER_KEY=masterkey",
		"DATABASE_URL=postgres://u:p@h/db",
		"HOME=/home/worker",
		"OPENAI_API_KEY=sk-live",
		"LC_ALL=C.UTF-8",
		"https_proxy=http://proxy:3128",
		"ADMIN_PASSWORD=hunter2",
		"malformed-entry",
		"=C:=C:\\work",
		"EDITOR=vim",
	}
	got := buildChildEnvFor(parent, nil, false)
	want := []string{"PATH=/usr/bin", "HOME=/home/worker", "LC_ALL=C.UTF-8", "https_proxy=http://proxy:3128"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestBuildChildEnvConfiguredWins(t *testing.T) {
	parent := []string{"PATH=/usr/bin", "HOME=/home/worker", "SUPERBOT_MASTER_KEY=masterkey"}
	configured := map[string]string{
		"PATH":         "/opt/mcp/bin",
		"GITHUB_TOKEN": "ghp_configured",
		"SUPERBOT_X":   "explicit",
		"BAD=NAME":     "dropped",
		"":             "dropped",
	}
	got := buildChildEnvFor(parent, configured, false)
	want := []string{
		"HOME=/home/worker",
		"GITHUB_TOKEN=ghp_configured",
		"PATH=/opt/mcp/bin",
		"SUPERBOT_X=explicit",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestBuildChildEnvCaseFolding(t *testing.T) {
	parent := []string{"Path=C:\\Windows", "SystemRoot=C:\\Windows"}
	configured := map[string]string{"PATH": "C:\\mcp"}

	// Windows: Path and PATH are one variable, so the parent's copy must go.
	if got, want := buildChildEnvFor(parent, configured, true), []string{"SystemRoot=C:\\Windows", "PATH=C:\\mcp"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("windows: got %q want %q", got, want)
	}
	// POSIX: they are distinct variables.
	if got, want := buildChildEnvFor(parent, configured, false), []string{"Path=C:\\Windows", "SystemRoot=C:\\Windows", "PATH=C:\\mcp"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("posix: got %q want %q", got, want)
	}
}

func TestBuildChildEnvEmpty(t *testing.T) {
	got := buildChildEnv([]string{"SUPERBOT_MASTER_KEY=masterkey"}, nil)
	if len(got) != 0 {
		t.Fatalf("got %q", got)
	}
	// exec.Cmd treats a nil Env as "inherit everything".
	if got == nil {
		t.Fatal("empty result must be non-nil")
	}
}
