package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"superbot/internal/store"
)

func antTestProvider(url string, timeout time.Duration) (*Anthropic, *sleepRecorder) {
	p := NewAnthropic("claude", url, "sk-test", timeout)
	rec := &sleepRecorder{}
	p.tr.sleep = rec.sleep
	return p, rec
}

func antSSE(events ...string) string {
	var b strings.Builder
	for _, ev := range events {
		var head struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(ev), &head)
		b.WriteString("event: " + head.Type + "\ndata: " + ev + "\n\n")
	}
	return b.String()
}

var antTextStream = antSSE(
	`{"type":"message_start","message":{"id":"msg_1","role":"assistant","content":[],"usage":{"input_tokens":10,"cache_read_input_tokens":30,"cache_creation_input_tokens":5,"output_tokens":1}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`{"type":"ping"}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":12}}`,
	`{"type":"message_stop"}`,
)

func TestAnthropicRequestMapping(t *testing.T) {
	server := newSSEServer(t, sseResponse{body: antTextStream})
	p, _ := antTestProvider(server.URL, time.Second)
	temp := 0.3
	req := Request{
		Model:       "claude-test",
		Temperature: &temp,
		Tools: []ToolDef{
			{Name: "search", Description: "Search the web", Parameters: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)},
			{Name: "now"},
		},
		Messages: []Message{
			TextMessage(RoleSystem, "You are helpful."),
			TextMessage(RoleUser, "first"),
			TextMessage(RoleUser, "second"),
			AssistantMessage("Let me look.", "hidden reasoning", []ToolCall{
				{ID: "toolu_1", Name: "search", Arguments: json.RawMessage(`{"q":"go"}`)},
				{ID: "toolu_2", Name: "now"},
			}),
			ToolResultMessage("toolu_1", "search", "found it", false),
			ToolResultMessage("toolu_2", "now", "clock broken", true),
			{Role: RoleUser, Parts: []Part{
				{Type: PartText, Text: "and this picture"},
				{Type: PartImage, Image: &Image{MIMEType: "image/png", Data: []byte{1, 2, 3}}},
			}},
			TextMessage(RoleSystem, "Answer in Chinese."),
		},
	}
	stream, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	collect(t, stream)

	header, path := server.request(0)
	if path != "/v1/messages" {
		t.Fatalf("path = %q", path)
	}
	if header.Get("x-api-key") != "sk-test" || header.Get("anthropic-version") != "2023-06-01" {
		t.Fatalf("headers = %v", header)
	}
	if header.Get("Authorization") != "" {
		t.Fatalf("unexpected Authorization header %q", header.Get("Authorization"))
	}

	var got any
	if err := json.Unmarshal(server.body(0), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	const wantJSON = `{
		"model": "claude-test",
		"max_tokens": 4096,
		"system": "You are helpful.\n\nAnswer in Chinese.",
		"temperature": 0.3,
		"stream": true,
		"tools": [
			{"name": "search", "description": "Search the web", "input_schema": {"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}},
			{"name": "now", "input_schema": {"type":"object","properties":{}}}
		],
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "first"},
				{"type": "text", "text": "second"}
			]},
			{"role": "assistant", "content": [
				{"type": "text", "text": "Let me look."},
				{"type": "tool_use", "id": "toolu_1", "name": "search", "input": {"q": "go"}},
				{"type": "tool_use", "id": "toolu_2", "name": "now", "input": {}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "toolu_1", "content": "found it"},
				{"type": "tool_result", "tool_use_id": "toolu_2", "content": "clock broken", "is_error": true},
				{"type": "text", "text": "and this picture"},
				{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "AQID"}}
			]}
		]
	}`
	var want any
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	gotNorm, _ := json.MarshalIndent(got, "", "  ")
	wantNorm, _ := json.MarshalIndent(want, "", "  ")
	if string(gotNorm) != string(wantNorm) {
		t.Fatalf("request body mismatch\n got: %s\nwant: %s", gotNorm, wantNorm)
	}
}

func TestAnthropicRequestDetails(t *testing.T) {
	t.Run("explicit max_tokens and no system", func(t *testing.T) {
		out := buildAnthropicRequest(Request{Model: "m", MaxTokens: 256, Messages: []Message{TextMessage(RoleUser, "hi")}})
		raw, _ := json.Marshal(out)
		if out.MaxTokens != 256 || strings.Contains(string(raw), `"system"`) || strings.Contains(string(raw), `"temperature"`) {
			t.Fatalf("body = %s", raw)
		}
	})
	t.Run("tool results sort before text merged ahead of them", func(t *testing.T) {
		out := buildAnthropicRequest(Request{Messages: []Message{
			TextMessage(RoleUser, "go"),
			AssistantMessage("", "", []ToolCall{{ID: "t1", Name: "a"}}),
			TextMessage(RoleUser, "note"),
			ToolResultMessage("t1", "a", "r", false),
		}})
		last := out.Messages[len(out.Messages)-1]
		if len(out.Messages) != 3 || last.Content[0].Type != "tool_result" || last.Content[1].Type != "text" {
			t.Fatalf("messages = %+v", out.Messages)
		}
	})
	t.Run("invalid tool arguments become an empty object", func(t *testing.T) {
		for _, args := range []string{"", "not json", `"str"`, `[1]`, `{"a":`} {
			if got := string(anthropicToolInput(json.RawMessage(args))); got != "{}" {
				t.Errorf("anthropicToolInput(%q) = %s", args, got)
			}
		}
	})
	t.Run("empty messages are skipped", func(t *testing.T) {
		out := buildAnthropicRequest(Request{Messages: []Message{
			TextMessage(RoleUser, "a"),
			AssistantMessage("", "only reasoning", nil),
			TextMessage(RoleUser, "b"),
		}})
		if len(out.Messages) != 1 || len(out.Messages[0].Content) != 2 {
			t.Fatalf("messages = %+v", out.Messages)
		}
	})
}

func TestAnthropicEndpoint(t *testing.T) {
	tests := []struct{ base, want string }{
		{"", "https://api.anthropic.com/v1/messages"},
		{"  ", "https://api.anthropic.com/v1/messages"},
		{"https://proxy.example/", "https://proxy.example/v1/messages"},
		{"https://proxy.example/v1", "https://proxy.example/v1/messages"},
		{"https://proxy.example/v1/", "https://proxy.example/v1/messages"},
		{"https://proxy.example/v1/messages", "https://proxy.example/v1/messages"},
		{"https://proxy.example/anthropic", "https://proxy.example/anthropic/v1/messages"},
	}
	for _, tt := range tests {
		if got := NewAnthropic("a", tt.base, "", 0).endpoint(); got != tt.want {
			t.Errorf("endpoint(%q) = %q, want %q", tt.base, got, tt.want)
		}
	}
}

func TestAnthropicStreamText(t *testing.T) {
	server := newSSEServer(t, sseResponse{body: antTextStream})
	p, _ := antTestProvider(server.URL, time.Second)
	stream, err := p.Stream(context.Background(), pingRequest)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()
	got := collect(t, stream)
	if got.err != nil || !got.done {
		t.Fatalf("done=%v err=%v", got.done, got.err)
	}
	if got.text != "Hello" || got.deltas != 2 {
		t.Fatalf("text = %q in %d deltas", got.text, got.deltas)
	}
	if got.finish != FinishStop {
		t.Fatalf("finish = %q", got.finish)
	}
	want := Usage{PromptTokens: 45, CachedTokens: 30, CompletionTokens: 12}
	if got.usage == nil || *got.usage != want {
		t.Fatalf("usage = %+v, want %+v", got.usage, want)
	}
}

func TestAnthropicStreamToolCalls(t *testing.T) {
	body := antSSE(
		`{"type":"message_start","message":{"usage":{"input_tokens":20,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"need "}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"tools"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Checking."}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_a","name":"search","input":{}}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":""}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"q\": \"go"}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"lang\", \"n\": 3}"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_b","name":"now","input":{}}}`,
		`{"type":"content_block_stop","index":3}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":40}}`,
		`{"type":"message_stop"}`,
	)
	server := newSSEServer(t, sseResponse{body: body})
	p, _ := antTestProvider(server.URL, time.Second)
	stream, err := p.Stream(context.Background(), pingRequest)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()
	got := collect(t, stream)
	if got.err != nil {
		t.Fatalf("stream error: %v", got.err)
	}
	if got.reasoning != "need tools" || got.text != "Checking." {
		t.Fatalf("reasoning=%q text=%q", got.reasoning, got.text)
	}
	if len(got.calls) != 2 {
		t.Fatalf("calls = %+v", got.calls)
	}
	if c := got.calls[0]; c.ID != "toolu_a" || c.Name != "search" || string(c.Arguments) != `{"q": "golang", "n": 3}` {
		t.Fatalf("call 0 = %+v (%s)", c, c.Arguments)
	}
	if c := got.calls[1]; c.ID != "toolu_b" || c.Name != "now" || string(c.Arguments) != `{}` {
		t.Fatalf("call 1 = %+v (%s)", c, c.Arguments)
	}
	if _, err := ValidateToolArguments(got.calls[0].Arguments); err != nil {
		t.Fatalf("accumulated arguments invalid: %v", err)
	}
	if got.finish != FinishToolCalls {
		t.Fatalf("finish = %q", got.finish)
	}
	want := Usage{PromptTokens: 20, CompletionTokens: 40}
	if got.usage == nil || *got.usage != want {
		t.Fatalf("usage = %+v, want %+v", got.usage, want)
	}
}

func TestAnthropicStopReasonMapping(t *testing.T) {
	tests := []struct{ wire, want string }{
		{"end_turn", FinishStop},
		{"stop_sequence", FinishStop},
		{"max_tokens", FinishLength},
		{"tool_use", FinishToolCalls},
		{"refusal", FinishContentFilter},
		{"pause_turn", ""},
		{"brand_new_reason", ""},
	}
	for _, tt := range tests {
		t.Run(tt.wire, func(t *testing.T) {
			body := antSSE(
				`{"type":"message_start","message":{"usage":{"input_tokens":1}}}`,
				`{"type":"message_delta","delta":{"stop_reason":"`+tt.wire+`"},"usage":{"output_tokens":1}}`,
				`{"type":"message_stop"}`,
			)
			server := newSSEServer(t, sseResponse{body: body})
			p, _ := antTestProvider(server.URL, time.Second)
			stream, err := p.Stream(context.Background(), pingRequest)
			if err != nil {
				t.Fatalf("stream: %v", err)
			}
			defer stream.Close()
			got := collect(t, stream)
			if !got.done || got.finish != tt.want {
				t.Fatalf("done=%v finish=%q, want %q", got.done, got.finish, tt.want)
			}
		})
	}
}

func TestAnthropicStreamErrorEvent(t *testing.T) {
	tests := []struct {
		errType   string
		status    int
		retryable bool
	}{
		{"overloaded_error", 529, true},
		{"rate_limit_error", 429, true},
		{"api_error", 500, true},
		{"invalid_request_error", 400, false},
		{"authentication_error", 401, false},
	}
	for _, tt := range tests {
		t.Run(tt.errType, func(t *testing.T) {
			body := antSSE(
				`{"type":"message_start","message":{"usage":{"input_tokens":1}}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"par"}}`,
				`{"type":"error","error":{"type":"`+tt.errType+`","message":"it broke"}}`,
			)
			server := newSSEServer(t, sseResponse{body: body})
			p, rec := antTestProvider(server.URL, time.Second)
			stream, err := p.Stream(context.Background(), pingRequest)
			if err != nil {
				t.Fatalf("stream: %v", err)
			}
			defer stream.Close()
			got := collect(t, stream)
			if got.done || got.text != "par" {
				t.Fatalf("got %+v", got)
			}
			var apiErr *APIError
			if !errors.As(got.err, &apiErr) {
				t.Fatalf("err = %v, want APIError", got.err)
			}
			if apiErr.Status != tt.status || apiErr.Provider != "claude" || !strings.Contains(apiErr.Message, "it broke") {
				t.Fatalf("apiErr = %+v", apiErr)
			}
			if IsRetryable(got.err) != tt.retryable {
				t.Fatalf("IsRetryable = %v, want %v", IsRetryable(got.err), tt.retryable)
			}
			if server.attempts() != 1 || len(rec.delays) != 0 {
				t.Fatal("mid-stream errors must not be retried by the provider")
			}
		})
	}
}

func TestAnthropicTruncatedStream(t *testing.T) {
	body := antSSE(
		`{"type":"message_start","message":{"usage":{"input_tokens":1}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"par"}}`,
	)
	server := newSSEServer(t, sseResponse{body: body})
	p, _ := antTestProvider(server.URL, time.Second)
	stream, err := p.Stream(context.Background(), pingRequest)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()
	got := collect(t, stream)
	if got.done || !errors.Is(got.err, io.ErrUnexpectedEOF) || !IsRetryable(got.err) {
		t.Fatalf("got %+v", got)
	}
}

func TestAnthropicRetryAndTypedErrors(t *testing.T) {
	t.Run("529 then success", func(t *testing.T) {
		server := newSSEServer(t,
			sseResponse{status: 529, header: map[string]string{"Retry-After": "90"}, body: `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`},
			sseResponse{body: antTextStream},
		)
		p, rec := antTestProvider(server.URL, time.Second)
		stream, err := p.Stream(context.Background(), pingRequest)
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		defer stream.Close()
		if got := collect(t, stream); got.text != "Hello" {
			t.Fatalf("got %+v", got)
		}
		// Retry-After above the cap is clamped to 30s.
		if server.attempts() != 2 || len(rec.delays) != 1 || rec.delays[0] != 30*time.Second {
			t.Fatalf("attempts = %d sleeps = %v", server.attempts(), rec.delays)
		}
	})
	t.Run("401 is not retried", func(t *testing.T) {
		server := newSSEServer(t, sseResponse{status: 401, body: `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`})
		p, _ := antTestProvider(server.URL, time.Second)
		_, err := p.Stream(context.Background(), pingRequest)
		if !IsAuthError(err) || IsRetryable(err) || server.attempts() != 1 {
			t.Fatalf("err = %v attempts = %d", err, server.attempts())
		}
	})
	t.Run("overflow", func(t *testing.T) {
		server := newSSEServer(t, sseResponse{status: 400, body: `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 210000 tokens > 200000 maximum"}}`})
		p, _ := antTestProvider(server.URL, time.Second)
		_, err := p.Stream(context.Background(), pingRequest)
		if !IsContextOverflow(err) || IsRetryable(err) || server.attempts() != 1 {
			t.Fatalf("err = %v attempts = %d", err, server.attempts())
		}
	})
}

func TestAnthropicIdleTimeoutDetectsStall(t *testing.T) {
	server := stallingServer(t, antSSE(
		`{"type":"message_start","message":{"usage":{"input_tokens":1}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`,
	))
	p, _ := antTestProvider(server.URL, 100*time.Millisecond)
	stream, err := p.Stream(context.Background(), pingRequest)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()
	got := collect(t, stream)
	if got.text != "partial" || got.done || !errors.Is(got.err, ErrStreamStalled) || !IsRetryable(got.err) {
		t.Fatalf("got %+v", got)
	}
}

func TestFactoryBuildsAnthropic(t *testing.T) {
	// No key is stored, so the secrets box is never consulted.
	provider, err := NewFactory(nil).Build(store.Provider{Kind: "anthropic", Name: "claude"}, time.Second)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	ant, ok := provider.(*Anthropic)
	if !ok {
		t.Fatalf("provider type = %T", provider)
	}
	if ant.Name() != "claude" || ant.endpoint() != "https://api.anthropic.com/v1/messages" {
		t.Fatalf("name=%q endpoint=%q", ant.Name(), ant.endpoint())
	}
	if mock, err := NewFactory(nil).Build(store.Provider{Kind: "mock", Name: "m"}, 0); err != nil || mock.Name() != "m" {
		t.Fatalf("mock build: %v", err)
	}
}
