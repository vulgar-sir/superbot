package llm

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DebugDirEnv names a directory where requests a provider refused with a
// 4xx are written, body and reply side by side. A refused request is the one
// case where the provider's message alone rarely explains what was wrong
// with what was sent. Bodies hold conversation content but never the API
// key, which travels in a header. Unset (the default) disables it.
const DebugDirEnv = "SUPERBOT_LLM_DEBUG_DIR"

const maxDebugDumpBytes = 8 << 20

func dumpRejected(req *http.Request, apiErr *APIError) {
	dir := os.Getenv(DebugDirEnv)
	if dir == "" || req == nil || req.GetBody == nil {
		return
	}
	if apiErr.Status < 400 || apiErr.Status >= 500 || apiErr.Status == http.StatusTooManyRequests {
		return
	}
	body, err := req.GetBody()
	if err != nil {
		return
	}
	defer body.Close()
	payload, err := io.ReadAll(io.LimitReader(body, maxDebugDumpBytes))
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	base := filepath.Join(dir, fmt.Sprintf("%s-%d-%d", safeDebugName(apiErr.Provider), apiErr.Status, time.Now().UnixNano()))
	_ = os.WriteFile(base+".request.json", payload, 0o600)
	_ = os.WriteFile(base+".response.txt", []byte(apiErr.Message), 0o600)
}

func safeDebugName(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "provider"
	}
	return string(out)
}
