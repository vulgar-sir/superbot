package memory

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"superbot/internal/store"
)

func TestSameNumbers(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"测试服务器的 SSH 端口是 2222", "测试服务器 SSH 用 2222 端口", true},
		{"测试服务器的 SSH 端口是 2222", "测试服务器的 SSH 端口改成了 2200", false},
		{"目标体重 70 公斤", "目标体重调整为 68 公斤", false},
		{"用户不吃香菜", "用户不喜欢吃香菜", true},
		{"保留 14 天，凌晨 3 点备份", "凌晨 3 点备份，保留 14 天", true},
		{"每天早上 7 点", "每天早上七点", false},
	}
	for _, tc := range cases {
		if got := sameNumbers(tc.a, tc.b); got != tc.want {
			t.Errorf("sameNumbers(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}

func TestMergeDecisionIsConservative(t *testing.T) {
	space := e5Space("intfloat/multilingual-e5-large")
	existing := store.Memory{ID: uuid.New(), Content: "用户不吃香菜"}
	// "不吃香菜" vs "不吃葱" scored 0.975 on the real model: no merge.
	if got := mergeDecision(space, "用户不吃葱", []scoredMemory{{memory: existing, similarity: 0.975}}); got.DuplicateOf != uuid.Nil {
		t.Fatal("a different fact below MergeMin must not be merged")
	}
	if got := mergeDecision(space, "用户不喜欢吃香菜", []scoredMemory{{memory: existing, similarity: 0.988}}); got.DuplicateOf != existing.ID {
		t.Fatal("a near-verbatim restatement is merged")
	}
	port := store.Memory{ID: uuid.New(), Content: "端口是 2222"}
	if got := mergeDecision(space, "端口是 2200", []scoredMemory{{memory: port, similarity: 0.995}}); got.DuplicateOf != uuid.Nil {
		t.Fatal("different numbers are never merged without judgment")
	}
	if got := mergeDecision(space, "x", nil); got.DuplicateOf != uuid.Nil || got.Supersedes != nil {
		t.Fatal("nothing related, nothing decided")
	}
}

func TestParseConflictDecision(t *testing.T) {
	related := []store.Memory{{ID: uuid.New()}, {ID: uuid.New()}, {ID: uuid.New()}}
	got := ParseConflictDecision("```json\n"+`{"relations":[
		{"id":"M1","relation":"replaces"},
		{"id":"m2","relation":"same"},
		{"id":"M3","relation":"different"},
		{"id":"M9","relation":"replaces"},
		{"id":"M1","relation":"same"}]}`+"\n```", related)
	if got.DuplicateOf != related[1].ID {
		t.Fatalf("duplicate = %v, want M2", got.DuplicateOf)
	}
	if !slices.Equal(got.Supersedes, []uuid.UUID{related[0].ID}) {
		t.Fatalf("supersedes = %v, want only M1 (unknown labels and repeats ignored)", got.Supersedes)
	}
	for _, reply := range []string{"", "不确定", `{"relations":"all"}`, `{"relations":[{"id":"M1","relation":"delete"}]}`} {
		if got := ParseConflictDecision(reply, related); got.DuplicateOf != uuid.Nil || len(got.Supersedes) != 0 {
			t.Fatalf("reply %q must keep everything, got %+v", reply, got)
		}
	}
}

type scriptedExtractor struct {
	reply string
	err   error
	last  ExtractRequest
}

func (s *scriptedExtractor) Extract(_ context.Context, req ExtractRequest) (string, error) {
	s.last = req
	return s.reply, s.err
}

func TestExtractorJudgeBillsTheConversation(t *testing.T) {
	conv := uuid.New()
	extractor := &scriptedExtractor{reply: `{"relations":[{"id":"M1","relation":"replaces"}]}`}
	judge := NewExtractorJudge(extractor, ExtractRequest{BotID: uuid.New(), ConversationID: conv})
	old := store.Memory{ID: uuid.New(), Content: "测试服务器的 SSH 端口是 2222"}
	decision, err := judge.JudgeConflict(context.Background(), "测试服务器的 SSH 端口改成了 2200", []store.Memory{old})
	if err != nil || !slices.Equal(decision.Supersedes, []uuid.UUID{old.ID}) {
		t.Fatalf("decision = %+v, %v", decision, err)
	}
	if extractor.last.ConversationID != conv || !strings.Contains(extractor.last.User, "M1") ||
		!strings.Contains(extractor.last.User, "2200") || !strings.Contains(extractor.last.System, "replaces") {
		t.Fatalf("judge request = %+v", extractor.last)
	}
	extractor.err = errors.New("budget exhausted")
	if _, err := judge.JudgeConflict(context.Background(), "x", []store.Memory{old}); err == nil {
		t.Fatal("a failed judge call must surface so the save falls back")
	}
	if decision, err := judge.JudgeConflict(context.Background(), "x", nil); err != nil || decision.DuplicateOf != uuid.Nil {
		t.Fatal("no related memories: no call, no decision")
	}
}
