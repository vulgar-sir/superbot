package events

import (
	"reflect"
	"testing"
	"time"
)

func TestDefaultRetention(t *testing.T) {
	policy := DefaultRetention()
	if policy.DeltaMaxAge != 7*24*time.Hour {
		t.Fatalf("DeltaMaxAge = %v", policy.DeltaMaxAge)
	}
	if policy.KeepLastPerConversation != 2000 {
		t.Fatalf("KeepLastPerConversation = %d", policy.KeepLastPerConversation)
	}
}

func TestPrunableTypes(t *testing.T) {
	got := PrunableTypes()
	if want := []string{"text_delta", "reasoning_delta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PrunableTypes = %q, want %q", got, want)
	}
	got[0] = "message.created"
	if PrunableTypes()[0] != TypeTextDelta {
		t.Fatal("PrunableTypes must return a fresh slice")
	}
	for _, typ := range PrunableTypes() {
		if !IsPrunableType(typ) {
			t.Fatalf("IsPrunableType(%q) = false", typ)
		}
	}
	durable := []string{
		TypeMessageCreated, TypeCycleUpdated, TypeRouteDecision, TypeToolCall, TypeToolResult,
		TypeToolApproval, TypeStatus, TypeUsage, TypeResync, TypeError, "",
	}
	for _, typ := range durable {
		if IsPrunableType(typ) {
			t.Fatalf("IsPrunableType(%q) = true", typ)
		}
	}
}

func TestRetentionCutoff(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	cutoff, ok := DefaultRetention().Cutoff(now)
	if !ok || !cutoff.Equal(time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("Cutoff = %v, %v", cutoff, ok)
	}
	if _, ok := (RetentionPolicy{}).Cutoff(now); ok {
		t.Fatal("zero policy must disable pruning")
	}
	if _, ok := (RetentionPolicy{DeltaMaxAge: -time.Hour}).Cutoff(now); ok {
		t.Fatal("negative age must disable pruning")
	}
}

func TestShouldPrune(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	old := now.Add(-8 * 24 * time.Hour)
	recent := now.Add(-6 * 24 * time.Hour)
	policy := DefaultRetention()
	const latest = 10000

	cases := []struct {
		name      string
		policy    RetentionPolicy
		typ       string
		seq       int64
		createdAt time.Time
		want      bool
	}{
		{"old delta outside keep window", policy, TypeTextDelta, 100, old, true},
		{"old reasoning delta", policy, TypeReasoningDelta, 100, old, true},
		{"recent delta", policy, TypeTextDelta, 100, recent, false},
		{"delta exactly at cutoff", policy, TypeTextDelta, 100, now.Add(-7 * 24 * time.Hour), false},
		{"old non-delta", policy, TypeMessageCreated, 100, old, false},
		{"old tool result", policy, TypeToolResult, 100, old, false},
		{"old delta, last one outside keep window", policy, TypeTextDelta, latest - 2000, old, true},
		{"old delta, first one inside keep window", policy, TypeTextDelta, latest - 1999, old, false},
		{"old delta, newest event", policy, TypeTextDelta, latest, old, false},
		{"pruning disabled", RetentionPolicy{KeepLastPerConversation: 10}, TypeTextDelta, 100, old, false},
		{"no keep window", RetentionPolicy{DeltaMaxAge: time.Hour}, TypeTextDelta, latest, old, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.policy.ShouldPrune(tc.typ, tc.seq, latest, tc.createdAt, now); got != tc.want {
				t.Fatalf("ShouldPrune = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestShouldPruneShortConversationKeepsEverything(t *testing.T) {
	now := time.Now()
	old := now.Add(-30 * 24 * time.Hour)
	for seq := int64(1); seq <= 1500; seq += 499 {
		if DefaultRetention().ShouldPrune(TypeTextDelta, seq, 1500, old, now) {
			t.Fatalf("seq %d pruned from a conversation shorter than the keep window", seq)
		}
	}
}
