package agent

import (
	"testing"

	"superbot/internal/llm"
)

func TestTrimMessagesToBudget(t *testing.T) {
	messages := []llm.Message{llm.TextMessage(llm.RoleSystem, "system prefix")}
	for i := 0; i < 10; i++ {
		messages = append(messages, llm.TextMessage(llm.RoleUser, "message with some words to count tokens"))
	}
	kept := trimMessagesToBudget(messages, 1_000_000, 6)
	if len(kept) != len(messages) {
		t.Fatalf("large budget should keep all messages, got %d", len(kept))
	}
	trimmed := trimMessagesToBudget(messages, 5, 4)
	if len(trimmed) != 5 {
		t.Fatalf("expected system + 4 recent messages, got %d", len(trimmed))
	}
	if len(trimmed[0].Parts) == 0 || trimmed[0].Parts[0].Text != "system prefix" {
		t.Fatalf("system prefix was dropped: %#v", trimmed[0].Parts)
	}
	if len(trimmed[len(trimmed)-1].Parts) == 0 ||
		trimmed[len(trimmed)-1].Parts[0].Text != messages[len(messages)-1].Parts[0].Text {
		t.Fatal("newest message should be preserved")
	}
}
