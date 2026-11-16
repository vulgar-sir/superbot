package mcpservice

import (
	"runtime"
	"sort"
	"strings"
)

// Variables a child process needs to start and behave normally. Matched
// case-insensitively because Windows environment names are.
var childEnvAllowed = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "USERNAME": true,
	"LANG": true, "TZ": true,
	"TMP": true, "TEMP": true, "TMPDIR": true,
	"SYSTEMROOT": true, "COMSPEC": true, "PATHEXT": true,
	"APPDATA": true, "LOCALAPPDATA": true, "USERPROFILE": true,
	"HOMEDRIVE": true, "HOMEPATH": true,
	"NODE_PATH": true, "PYTHONPATH": true,
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true,
}

var childEnvAllowedPrefixes = []string{"LC_", "PROGRAMFILES"}

// Checked before the allowlist so a future allowlist entry can never
// reintroduce a credential.
var childEnvDeniedFragments = []string{
	"SECRET", "PASSWORD", "TOKEN", "API_KEY", "APIKEY", "DATABASE_URL", "DSN", "PRIVATE_KEY",
}

func childEnvNameAllowed(name string) bool {
	upper := strings.ToUpper(name)
	if upper == "" || strings.HasPrefix(upper, "SUPERBOT_") {
		return false
	}
	for _, fragment := range childEnvDeniedFragments {
		if strings.Contains(upper, fragment) {
			return false
		}
	}
	if childEnvAllowed[upper] {
		return true
	}
	for _, prefix := range childEnvAllowedPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// buildChildEnv returns the environment for a stdio MCP server: the allowlisted
// part of parent (KEY=VALUE entries, as from os.Environ) with the server's own
// configured variables layered on top. The worker's environment holds the
// master key, database DSN and provider keys, so nothing else is inherited.
// Configured variables are explicit admin choices and always win, whatever
// their name.
func buildChildEnv(parent []string, configured map[string]string) []string {
	return buildChildEnvFor(parent, configured, runtime.GOOS == "windows")
}

func buildChildEnvFor(parent []string, configured map[string]string, foldCase bool) []string {
	identity := func(name string) string {
		if foldCase {
			return strings.ToUpper(name)
		}
		return name
	}
	overridden := make(map[string]bool, len(configured))
	for name := range configured {
		overridden[identity(name)] = true
	}
	env := make([]string, 0, len(parent)+len(configured))
	for _, entry := range parent {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || !childEnvNameAllowed(name) || overridden[identity(name)] {
			continue
		}
		env = append(env, entry)
	}
	names := make([]string, 0, len(configured))
	for name := range configured {
		if name == "" || strings.ContainsAny(name, "=\x00") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		env = append(env, name+"="+configured[name])
	}
	return env
}
