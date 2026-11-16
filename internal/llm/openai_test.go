package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAIStreamTextAndUsage(t *testing.T) {
	const body = "" +
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":null,\"reasoning_content\":\"\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"OK\",\"reasoning_content\":null}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":35,\"completion_tokens\":2,\"prompt_cache_hit_tokens\":0}}\n\n" +
		"data: [DONE]\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	provider := NewOpenAICompat("test", server.URL, "test-key", time.Second)
	stream, err := provider.Stream(context.Background(), Request{
		Model:    "test-model",
		Messages: []Message{TextMessage(RoleUser, "ping")},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()

	var text string
	var usage *Usage
	for event := range stream.Events() {
		switch event.Type {
		case EventTextDelta:
			text += event.Text
		case EventUsage:
			usage = event.Usage
		case EventError:
			t.Fatalf("stream error: %v", event.Err)
		}
	}
	if text != "OK" {
		t.Fatalf("text = %q, want OK", text)
	}
	if usage == nil || usage.PromptTokens != 35 || usage.CompletionTokens != 2 {
		t.Fatalf("usage = %+v", usage)
	}
}
