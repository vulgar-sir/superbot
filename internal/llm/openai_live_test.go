package llm

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestOpenAICompatLive is opt-in because it spends real tokens:
//
//	$env:DEEPSEEK_API_KEY="..." ; go test ./internal/llm -run TestOpenAICompatLive -v
func TestOpenAICompatLive(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY is not set")
	}
	provider := NewOpenAICompat("deepseek", "https://api.deepseek.com", key, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stream, err := provider.Stream(ctx, Request{
		Model:     "deepseek-flash",
		Messages:  []Message{TextMessage(RoleUser, "ping. Reply with exactly: OK")},
		MaxTokens: 128,
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()
	var text, reasoning string
	for event := range stream.Events() {
		switch event.Type {
		case EventTextDelta:
			text += event.Text
			t.Logf("text delta: %q", event.Text)
		case EventReasoningDelta:
			reasoning += event.Text
		case EventUsage:
			t.Logf("usage: %+v", event.Usage)
		case EventError:
			t.Fatalf("stream error: %v", event.Err)
		}
	}
	t.Logf("final text=%q reasoning=%q", text, reasoning)
	if text == "" {
		t.Fatalf("empty final text")
	}
}
