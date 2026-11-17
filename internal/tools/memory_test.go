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
		NewMemorySaveTool(nil, MemoryContext{OwnerID: uuid.New(), BotID: uuid.New(), TeamID: &team}),
		NewMemorySearchTool(nil, MemoryContext{OwnerID: uuid.New(), BotID: uuid.New(), TeamID: &team}),
		NewMemoryForgetTool(nil, MemoryContext{OwnerID: uuid.New(), BotID: uuid.New(), TeamID: &team}),
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
	tool := NewMemorySaveTool(nil, MemoryContext{OwnerID: uuid.New(), BotID: uuid.New()})
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
	tool := NewMemoryForgetTool(nil, MemoryContext{OwnerID: uuid.New(), BotID: uuid.New()})
	for _, raw := range []string{`{}`, `{"id":"  ","content":" "}`, `{"id":"not-a-uuid"}`, `not json`} {
		if _, err := tool.Execute(context.Background(), json.RawMessage(raw)); err == nil {
			t.Errorf("arguments %s must be rejected", raw)
		}
	}
}

func TestMemoryScopesStayWithTheCurrentPerson(t *testing.T) {
	bot, team, user := uuid.New(), uuid.New(), uuid.New()
	withPerson := MemoryContext{BotID: bot, TeamID: &team, UserID: &user}
	scopes := withPerson.scopes()
	if len(scopes) != 3 || scopes[0] != (memory.Scope{Type: "bot", ID: bot}) ||
		scopes[1] != (memory.Scope{Type: "user", ID: user}) || scopes[2] != (memory.Scope{Type: "team", ID: team}) {
		t.Fatalf("scopes = %+v", scopes)
	}
	for requested, want := range map[string]memory.Scope{
		"":     {Type: "user", ID: user},
		"user": {Type: "user", ID: user},
		"USER": {Type: "user", ID: user},
		"bot":  {Type: "bot", ID: bot},
		"team": {Type: "team", ID: team},
	} {
		got, err := withPerson.scope(requested)
		if err != nil || got != want {
			t.Errorf("scope(%q) = %+v, %v; want %+v", requested, got, err, want)
		}
	}
	if _, err := withPerson.scope("global"); err == nil {
		t.Error("an unknown scope must be refused")
	}

	// Without a known person (a routine, an old conversation) there is no
	// user scope at all: nobody else's memories can be reached.
	nobody := MemoryContext{BotID: bot}
	if got, err := nobody.scope(""); err != nil || got != (memory.Scope{Type: "bot", ID: bot}) {
		t.Fatalf("default without a person = %+v, %v", got, err)
	}
	if _, err := nobody.scope("user"); err == nil {
		t.Fatal("scope=user without a person must be refused")
	}
	if _, err := nobody.scope("team"); err == nil {
		t.Fatal("scope=team outside a group must be refused")
	}
	if got := nobody.scopes(); len(got) != 1 || got[0].Type != "bot" {
		t.Fatalf("scopes without a person = %+v", got)
	}
}

func TestMemorySaveRefusesUnavailableScope(t *testing.T) {
	tool := NewMemorySaveTool(nil, MemoryContext{OwnerID: uuid.New(), BotID: uuid.New()})
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"content":"用户喜欢简洁回答","scope":"user"}`))
	if err != nil || !result.IsError || !strings.Contains(result.Content, "scope=user") {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestSaveReportIsTruthful(t *testing.T) {
	saved := memory.SaveResult{Memory: store.Memory{ID: uuid.New()}, Pending: true}
	report := saveReport("user", saved)
	if !strings.HasPrefix(report, "memory saved (user)") || !strings.Contains(report, "stored") || strings.Contains(strings.ToLower(report), "error") {
		t.Fatalf("a pending save is a save: %q", report)
	}
	old := store.Memory{ID: uuid.New(), ScopeType: "user", Content: "测试服务器的 SSH 端口是 2222"}
	pinned := store.Memory{ID: uuid.New(), ScopeType: "user", Content: "用户住在西湖区", Pinned: true}
	report = saveReport("user", memory.SaveResult{Superseded: []store.Memory{old}, PinnedConflicts: []store.Memory{pinned}})
	if !strings.Contains(report, old.ID.String()) || !strings.Contains(report, "Retired") ||
		!strings.Contains(report, pinned.ID.String()) || !strings.Contains(report, "only the user") {
		t.Fatalf("report must name what was retired and what needs the user: %q", report)
	}
	if got := saveReport("bot", memory.SaveResult{Duplicate: true}); got != saveMessage("bot", true) {
		t.Fatalf("a plain duplicate = %q", got)
	}
}
