package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"superbot/internal/memory"
	"superbot/internal/store"
)

func TestMemoryToolSchemas(t *testing.T) {
	team := uuid.New()
	tools := []Tool{
		NewMemorySaveTool(nil, uuid.New(), uuid.New(), &team),
		NewMemorySearchTool(nil, uuid.New(), uuid.New(), &team),
		NewMemoryForgetTool(nil, uuid.New(), uuid.New(), &team),
	}
	wantNames := []string{"memory_save", "memory_search", "memory_forget"}
	for i, tool := range tools {
		if tool.Name() != wantNames[i] {
			t.Fatalf("tool %d name = %q, want %q", i, tool.Name(), wantNames[i])
		}
		var schema struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(tool.Parameters(), &schema); err != nil || schema.Type != "object" {
			t.Fatalf("%s parameters are not a JSON object schema: %v", tool.Name(), err)
		}
		switch tool.Name() {
		case "memory_save":
			if _, ok := schema.Properties["pinned"]; !ok {
				t.Fatal("memory_save must accept pinned")
			}
		case "memory_forget":
			if _, ok := schema.Properties["id"]; !ok {
				t.Fatal("memory_forget must accept id")
			}
			if _, ok := schema.Properties["content"]; !ok {
				t.Fatal("memory_forget must accept content")
			}
		}
	}
}

// The secret check runs before the service is touched, so a nil service is
// enough to prove nothing is stored.
func TestMemorySaveRefusesSecrets(t *testing.T) {
	tool := NewMemorySaveTool(nil, uuid.New(), uuid.New(), nil)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"content":"OpenAI key: sk-proj-AbCdEf0123456789AbCdEf0123456789"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError || result.Content != memory.ErrSecretContent.Error() {
		t.Fatalf("result = %+v", result)
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"content":"  "}`)); err == nil {
		t.Fatal("blank content must be rejected")
	}
}

func TestSaveMessage(t *testing.T) {
	if got := saveMessage("bot", false); got != "memory saved (bot)" {
		t.Fatalf("saveMessage = %q", got)
	}
	if got := saveMessage("team", true); !strings.HasPrefix(got, "已存在，已更新") || !strings.Contains(got, "team") {
		t.Fatalf("duplicate message = %q", got)
	}
}

func TestMemoryLineMarksPinned(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	plain := memoryLine(store.Memory{ID: id, ScopeType: "bot", Content: " 喜欢简洁回答 "})
	if plain != "- [bot] 喜欢简洁回答 (id: 550e8400-e29b-41d4-a716-446655440000)" {
		t.Fatalf("plain line = %q", plain)
	}
	pinned := memoryLine(store.Memory{ID: id, ScopeType: "team", Content: "周报周五发", Pinned: true})
	if pinned != "- [team] [pinned] 周报周五发 (id: 550e8400-e29b-41d4-a716-446655440000)" {
		t.Fatalf("pinned line = %q", pinned)
	}
}

func TestMemoryForgetValidatesArguments(t *testing.T) {
	tool := NewMemoryForgetTool(nil, uuid.New(), uuid.New(), nil)
	for _, raw := range []string{`{}`, `{"id":"  ","content":" "}`, `{"id":"not-a-uuid"}`, `not json`} {
		if _, err := tool.Execute(context.Background(), json.RawMessage(raw)); err == nil {
			t.Errorf("arguments %s must be rejected", raw)
		}
	}
}
