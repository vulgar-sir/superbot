package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"superbot/internal/store"
)

func TestRecallUserID(t *testing.T) {
	owner, sender := uuid.New(), uuid.New()
	direct := store.Conversation{Kind: "direct", DirectUserID: &owner}
	team := store.Conversation{Kind: "team"}
	fromSender := store.Message{SenderType: "user", Metadata: json.RawMessage(`{"sender_user_id":"` + sender.String() + `"}`)}
	anonymous := store.Message{SenderType: "user", Metadata: json.RawMessage(`{}`)}

	if got := recallUserID(direct, fromSender); got == nil || *got != owner {
		t.Fatalf("a direct conversation recalls for its user, got %v", got)
	}
	if got := recallUserID(team, fromSender); got == nil || *got != sender {
		t.Fatalf("a group recalls for the person who wrote the message, got %v", got)
	}
	if got := recallUserID(team, anonymous); got != nil {
		t.Fatalf("a group message without a known sender recalls no personal memories, got %v", got)
	}
	if got := recallUserID(store.Conversation{Kind: "subagent"}, fromSender); got != nil {
		t.Fatalf("a conversation without a user has no personal scope, got %v", got)
	}
}

func TestMemoryRecallBlock(t *testing.T) {
	if memoryRecallBlock(nil) != "" {
		t.Fatal("no memories, no block")
	}
	block := memoryRecallBlock([]store.Memory{{
		ScopeType: "user", Content: "测试服务器的 SSH 端口改成了 2200",
		UpdatedAt: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC),
	}})
	if !strings.HasPrefix(block, "## 相关记忆") || !strings.Contains(block, "- [user，2026-11-02] 测试服务器的 SSH 端口改成了 2200") ||
		!strings.Contains(block, "以较新的为准") {
		t.Fatalf("block = %q", block)
	}
}

func TestParsePersonalIndexes(t *testing.T) {
	got, ok := parsePersonalIndexes("结果：\n```json\n{\"personal\": [2, 9, 0, 3]}\n```", 3)
	if !ok || len(got) != 2 || !got[2] || !got[3] {
		t.Fatalf("got %v %v; want only the in-range indexes 2 and 3", got, ok)
	}
	if got, ok := parsePersonalIndexes(`{"personal": []}`, 3); !ok || len(got) != 0 {
		t.Fatal("an empty verdict is a verdict: nothing is personal")
	}
	for _, reply := range []string{"都不是个人事实", `{"other": [1]}`, `{"personal": `} {
		if _, ok := parsePersonalIndexes(reply, 3); ok {
			t.Fatalf("%q holds no verdict", reply)
		}
	}
}
