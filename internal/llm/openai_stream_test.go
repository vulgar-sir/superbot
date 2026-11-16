package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func oaiTestProvider(url string, timeout time.Duration, opts ...OpenAIOption) (*OpenAICompat, *sleepRecorder) {
	p := NewOpenAICompat("test", url, "test-key", timeout, opts...)
	rec := &sleepRecorder{}
	p.tr.sleep = rec.sleep
	return p, rec
}

var pingRequest = Request{Model: "test-model", Messages: []Message{TextMessage(RoleUser, "ping")}}

const oaiOKBody = "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"

func TestOpenAIFinishReason(t *testing.T) {
	tests := []struct{ wire, want string }{
		{`"stop"`, FinishStop},
		{`"length"`, FinishLength},
		{`"tool_calls"`, FinishToolCalls},
		{`"function_call"`, FinishToolCalls},
		{`"content_filter"`, FinishContentFilter},
		{`"something_new"`, ""},
		{`null`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.wire, func(t *testing.T) {
			body := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n" +
				"data: {\"choices\":[{\"delta\":{},\"finish_reason\":" + tt.wire + "}]}\n\n" +
				"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n" +
				"data: [DONE]\n\n"
			server := newSSEServer(t, sseResponse{body: body})
			p, _ := oaiTestProvider(server.URL, time.Second)
			stream, err := p.Stream(context.Background(), pingRequest)
			if err != nil {
				t.Fatalf("stream: %v", err)
			}
			defer stream.Close()
			got := collect(t, stream)
			if got.err != nil || !got.done {
				t.Fatalf("done=%v err=%v", got.done, got.err)
			}
			if got.finish != tt.want {
				t.Fatalf("finish = %q, want %q", got.finish, tt.want)
			}
		})
	}
}

func TestOpenAIToolCallAccumulation(t *testing.T) {
	// Fragments of two calls interleave, the second call arrives first, and
	// the provider reports "stop" even though it asked for tools.
	body := "" +
		"data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"think\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"id\":\"call_b\",\"type\":\"function\",\"function\":{\"name\":\"noargs\",\"arguments\":\"\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_a\",\"type\":\"function\",\"function\":{\"name\":\"search\",\"arguments\":\"{\\\"q\\\":\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"go\\\"}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	server := newSSEServer(t, sseResponse{body: body})
	p, _ := oaiTestProvider(server.URL, time.Second)
	stream, err := p.Stream(context.Background(), pingRequest)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()
	got := collect(t, stream)
	if got.err != nil {
		t.Fatalf("stream error: %v", got.err)
	}
	if len(got.calls) != 2 {
		t.Fatalf("calls = %+v", got.calls)
	}
	if c := got.calls[0]; c.ID != "call_a" || c.Name != "search" || string(c.Arguments) != `{"q":"go"}` {
		t.Fatalf("call 0 = %+v (%s)", c, c.Arguments)
	}
	if c := got.calls[1]; c.ID != "call_b" || c.Name != "noargs" || string(c.Arguments) != `{}` {
		t.Fatalf("call 1 = %+v (%s)", c, c.Arguments)
	}
	if got.reasoning != "think" {
		t.Fatalf("reasoning = %q", got.reasoning)
	}
	if got.finish != FinishToolCalls {
		t.Fatalf("finish = %q, want tool_calls", got.finish)
	}
}

func TestOpenAIRetryOn429ThenSuccess(t *testing.T) {
	server := newSSEServer(t,
		sseResponse{status: 429, header: map[string]string{"Retry-After": "2"}, body: `{"error":{"message":"slow down"}}`},
		sseResponse{body: oaiOKBody},
	)
	p, rec := oaiTestProvider(server.URL, time.Second)
	stream, err := p.Stream(context.Background(), pingRequest)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()
	got := collect(t, stream)
	if got.text != "OK" || got.finish != FinishStop {
		t.Fatalf("got %+v", got)
	}
	if n := server.attempts(); n != 2 {
		t.Fatalf("attempts = %d, want 2", n)
	}
	if len(rec.delays) != 1 || rec.delays[0] != 2*time.Second {
		t.Fatalf("sleeps = %v, want [2s]", rec.delays)
	}
	if string(server.body(0)) != string(server.body(1)) || len(server.body(1)) == 0 {
		t.Fatal("retry did not resend the same body")
	}
}

func TestOpenAIRetryExhausted(t *testing.T) {
	server := newSSEServer(t, sseResponse{status: 503, body: "overloaded"})
	p, rec := oaiTestProvider(server.URL, time.Second)
	_, err := p.Stream(context.Background(), pingRequest)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 503 || apiErr.Provider != "test" || apiErr.Message != "overloaded" {
		t.Fatalf("err = %v", err)
	}
	if n := server.attempts(); n != 3 {
		t.Fatalf("attempts = %d, want 3", n)
	}
	if len(rec.delays) != 2 {
		t.Fatalf("sleeps = %v, want 2", rec.delays)
	}
	for i, ceil := range []time.Duration{500 * time.Millisecond, time.Second} {
		if d := rec.delays[i]; d < ceil/2 || d > ceil {
			t.Fatalf("backoff %d = %v, want within [%v, %v]", i, d, ceil/2, ceil)
		}
	}
	if !IsRetryable(err) {
		t.Fatal("503 should stay retryable for the caller")
	}
}

func TestOpenAINoRetryOnClientErrors(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		auth     bool
		overflow bool
	}{
		{"400", 400, `{"error":{"message":"bad request"}}`, false, false},
		{"400 overflow", 400, `{"error":{"code":"context_length_exceeded","message":"This model's maximum context length is 8192 tokens"}}`, false, true},
		{"401", 401, `{"error":{"message":"bad key"}}`, true, false},
		{"403", 403, `forbidden`, true, false},
		{"404", 404, `no such model`, false, false},
		{"422", 422, `unprocessable`, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newSSEServer(t, sseResponse{status: tt.status, header: map[string]string{"Retry-After": "1"}, body: tt.body})
			p, rec := oaiTestProvider(server.URL, time.Second)
			_, err := p.Stream(context.Background(), pingRequest)
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Status != tt.status {
				t.Fatalf("err = %v", err)
			}
			if n := server.attempts(); n != 1 {
				t.Fatalf("attempts = %d, want 1", n)
			}
			if len(rec.delays) != 0 {
				t.Fatalf("unexpected sleeps %v", rec.delays)
			}
			if IsRetryable(err) {
				t.Fatal("must not be retryable")
			}
			if IsAuthError(err) != tt.auth {
				t.Fatalf("IsAuthError = %v, want %v", IsAuthError(err), tt.auth)
			}
			if IsContextOverflow(err) != tt.overflow {
				t.Fatalf("IsContextOverflow = %v, want %v", IsContextOverflow(err), tt.overflow)
			}
		})
	}
}

func TestOpenAIRetryOnDroppedConnection(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, oaiOKBody)
	}))
	defer server.Close()
	p, rec := oaiTestProvider(server.URL, time.Second)
	stream, err := p.Stream(context.Background(), pingRequest)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()
	if got := collect(t, stream); got.text != "OK" {
		t.Fatalf("got %+v", got)
	}
	if attempts.Load() != 2 || len(rec.delays) != 1 {
		t.Fatalf("attempts = %d sleeps = %v", attempts.Load(), rec.delays)
	}
}

func TestOpenAICancelDuringBackoff(t *testing.T) {
	server := newSSEServer(t, sseResponse{status: 500, body: "boom"})
	p := NewOpenAICompat("test", server.URL, "", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.tr.sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return sleepContext(ctx, time.Minute)
	}
	start := time.Now()
	_, err := p.Stream(ctx, pingRequest)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if IsRetryable(err) {
		t.Fatal("cancellation must not be retryable")
	}
	if n := server.attempts(); n != 1 {
		t.Fatalf("attempts = %d, want 1", n)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancellation was not prompt")
	}
}

func TestOpenAIIdleTimeoutDetectsStall(t *testing.T) {
	server := stallingServer(t, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
	p, _ := oaiTestProvider(server.URL, 100*time.Millisecond)
	start := time.Now()
	stream, err := p.Stream(context.Background(), pingRequest)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()
	got := collect(t, stream)
	if got.text != "partial" {
		t.Fatalf("text = %q", got.text)
	}
	if got.done {
		t.Fatal("stalled stream must not report done")
	}
	if !errors.Is(got.err, ErrStreamStalled) {
		t.Fatalf("err = %v, want ErrStreamStalled", got.err)
	}
	if !IsRetryable(got.err) {
		t.Fatal("stall must be retryable")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("stall detection took %v", elapsed)
	}
}

func TestOpenAIIdleTimeoutDefault(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		if got := NewOpenAICompat("x", "http://x", "", d).tr.idle; got != 120*time.Second {
			t.Fatalf("idle = %v for timeout %v, want 120s", got, d)
		}
	}
	if got := NewOpenAICompat("x", "http://x", "", 5*time.Second).tr.idle; got != 5*time.Second {
		t.Fatalf("idle = %v, want 5s", got)
	}
}

// A slow consumer must not be mistaken for a stalled provider: the watchdog
// only runs while a read is blocked.
func TestOpenAISlowConsumerIsNotAStall(t *testing.T) {
	server := newSSEServer(t, sseResponse{body: oaiOKBody})
	p, _ := oaiTestProvider(server.URL, 50*time.Millisecond)
	stream, err := p.Stream(context.Background(), pingRequest)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()
	time.Sleep(200 * time.Millisecond)
	got := collect(t, stream)
	if got.err != nil || got.text != "OK" || !got.done {
		t.Fatalf("got %+v", got)
	}
}

func TestOpenAICancelUnblocksReader(t *testing.T) {
	// More events than the channel buffers, so the reader goroutine is parked
	// on a send when the consumer walks away.
	prefix := strings.Repeat("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n", 200)
	stop := map[string]func(context.CancelFunc, Stream){
		"ctx cancel": func(cancel context.CancelFunc, _ Stream) { cancel() },
		"close":      func(_ context.CancelFunc, s Stream) { _ = s.Close() },
	}
	for name, stopFn := range stop {
		t.Run(name, func(t *testing.T) {
			server := stallingServer(t, prefix)
			p, _ := oaiTestProvider(server.URL, time.Minute)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, err := p.Stream(ctx, pingRequest)
			if err != nil {
				t.Fatalf("stream: %v", err)
			}
			time.Sleep(100 * time.Millisecond)
			stopFn(cancel, stream)

			closed := make(chan collected, 1)
			go func() {
				var c collected
				for ev := range stream.Events() {
					switch ev.Type {
					case EventError:
						c.err = ev.Err
					case EventDone:
						c.done = true
					}
				}
				closed <- c
			}()
			select {
			case c := <-closed:
				if c.done || c.err != nil {
					t.Fatalf("cancelled stream reported done=%v err=%v", c.done, c.err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("reader goroutine still blocked after cancellation")
			}
		})
	}
}

func TestOpenAIMidStreamDisconnectIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "4096")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"par\"}}]}\n\n")
	}))
	defer server.Close()
	p, rec := oaiTestProvider(server.URL, time.Second)
	stream, err := p.Stream(context.Background(), pingRequest)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()
	got := collect(t, stream)
	if got.err == nil || got.done {
		t.Fatalf("got %+v, want a stream error", got)
	}
	if !IsRetryable(got.err) {
		t.Fatalf("truncated body should be retryable: %v", got.err)
	}
	if len(rec.delays) != 0 {
		t.Fatal("provider must not retry once the stream has started")
	}
}

func TestOpenAIReasoningEcho(t *testing.T) {
	call := ToolCall{ID: "call_1", Name: "search", Arguments: json.RawMessage(`{"q":"go"}`)}
	req := Request{Model: "m", Messages: []Message{
		TextMessage(RoleUser, "hi"),
		AssistantMessage("", "I should search", []ToolCall{call}),
		ToolResultMessage("call_1", "search", "result", false),
		AssistantMessage("answer", "no tools here", nil),
		TextMessage(RoleUser, "more"),
	}}
	tests := []struct {
		name string
		opts []OpenAIOption
		want any
	}{
		{"off by default", nil, nil},
		{"explicitly off", []OpenAIOption{WithEchoReasoning(false)}, nil},
		{"on", []OpenAIOption{WithEchoReasoning(true)}, "I should search"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newSSEServer(t, sseResponse{body: oaiOKBody})
			p, _ := oaiTestProvider(server.URL, time.Second, tt.opts...)
			stream, err := p.Stream(context.Background(), req)
			if err != nil {
				t.Fatalf("stream: %v", err)
			}
			collect(t, stream)

			var body struct {
				Messages []map[string]any `json:"messages"`
			}
			if err := json.Unmarshal(server.body(0), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if len(body.Messages) != 5 {
				t.Fatalf("messages = %d, want 5", len(body.Messages))
			}
			toolTurn := body.Messages[1]
			got, present := toolTurn["reasoning_content"]
			if tt.want == nil && present {
				t.Fatalf("reasoning_content sent while disabled: %v", got)
			}
			if tt.want != nil && got != tt.want {
				t.Fatalf("reasoning_content = %v, want %v", got, tt.want)
			}
			if _, ok := toolTurn["tool_calls"]; !ok {
				t.Fatalf("tool_calls missing: %v", toolTurn)
			}
			// Only tool-call turns echo reasoning.
			if _, ok := body.Messages[3]["reasoning_content"]; ok {
				t.Fatalf("plain assistant turn echoed reasoning: %v", body.Messages[3])
			}
		})
	}

	t.Run("field toggle", func(t *testing.T) {
		p := NewOpenAICompat("x", "http://x", "", 0)
		if p.EchoReasoning {
			t.Fatal("EchoReasoning must default to false")
		}
		p.EchoReasoning = true
		msgs := p.buildRequest(req).Messages
		if msgs[1].ReasoningContent == nil || *msgs[1].ReasoningContent != "I should search" {
			t.Fatalf("reasoning not echoed: %+v", msgs[1])
		}
	})
}
