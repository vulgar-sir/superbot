package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRejectedRequestIsDumpedOnlyWhenEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("{\"error\":{\"message\":\"bad shape\"}}"))
	}))
	defer server.Close()
	request := Request{Model: "m", Messages: []Message{TextMessage(RoleUser, "需要被记录的内容")}}

	dir := t.TempDir()
	t.Setenv(DebugDirEnv, "")
	if _, err := NewOpenAICompat("Prov ider", server.URL, "secret-key", time.Second).Stream(context.Background(), request); err == nil {
		t.Fatal("expected the request to be refused")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("nothing may be written when the debug directory is not configured")
	}

	t.Setenv(DebugDirEnv, dir)
	if _, err := NewOpenAICompat("Prov ider", server.URL, "secret-key", time.Second).Stream(context.Background(), request); err == nil {
		t.Fatal("expected the request to be refused")
	}
	requests, _ := filepath.Glob(filepath.Join(dir, "Prov_ider-400-*.request.json"))
	responses, _ := filepath.Glob(filepath.Join(dir, "Prov_ider-400-*.response.txt"))
	if len(requests) != 1 || len(responses) != 1 {
		t.Fatalf("want one request and one response file, got %d and %d", len(requests), len(responses))
	}
	body, _ := os.ReadFile(requests[0])
	if !strings.Contains(string(body), "需要被记录的内容") {
		t.Fatal("the dumped request does not contain the body that was sent")
	}
	if strings.Contains(string(body), "secret-key") {
		t.Fatal("the API key must never be written to disk")
	}
	reply, _ := os.ReadFile(responses[0])
	if !strings.Contains(string(reply), "bad shape") {
		t.Fatal("the provider's reply was not recorded")
	}
}
