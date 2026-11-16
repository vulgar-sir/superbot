package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func mockRun(t *testing.T, m *MockProvider, req Request) collected {
	t.Helper()
	stream, err := m.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()
	return collect(t, stream)
}

func userRequest(text string) Request {
	return Request{Model: "mock-model", Messages: []Message{TextMessage(RoleUser, text)}}
}

func TestMockDefaultEcho(t *testing.T) {
	m := NewMock("")
	if m.Name() != "mock" {
		t.Fatalf("name = %q", m.Name())
	}
	got := mockRun(t, m, userRequest("  你好  "))
	if got.text != "Mock 回复：你好" {
		t.Fatalf("text = %q", got.text)
	}
	if !got.done || got.finish != FinishStop || got.err != nil {
		t.Fatalf("got %+v", got)
	}
	if got.usage == nil || got.usage.PromptTokens == 0 || got.usage.CompletionTokens == 0 {
		t.Fatalf("usage = %+v", got.usage)
	}
	if got := mockRun(t, m, Request{}); got.text != "Mock 回复：(empty)" {
		t.Fatalf("empty text = %q", got.text)
	}
}

func TestScriptedMockSequencing(t *testing.T) {
	call := ToolCall{ID: "c1", Name: "search", Arguments: json.RawMessage(`{"q":"x"}`)}
	boom := errors.New("boom")
	midway := errors.New("midway")
	m := NewScriptedMock("scripted",
		MockStep{Reasoning: "hmm", Text: "calling", ToolCalls: []ToolCall{call}, Usage: &Usage{PromptTokens: 7, CachedTokens: 3, CompletionTokens: 9}},
		MockStep{Err: boom},
		MockStep{Text: "abcdefghij", Chunks: 4, FinishReason: FinishLength},
		MockStep{Text: "partial", StreamErr: midway},
	)
	if m.Name() != "scripted" || m.Calls() != 0 {
		t.Fatalf("name=%q calls=%d", m.Name(), m.Calls())
	}

	got := mockRun(t, m, userRequest("one"))
	if got.reasoning != "hmm" || got.text != "calling" || len(got.calls) != 1 || got.calls[0].ID != "c1" {
		t.Fatalf("step 1 = %+v", got)
	}
	if got.finish != FinishToolCalls || *got.usage != (Usage{PromptTokens: 7, CachedTokens: 3, CompletionTokens: 9}) {
		t.Fatalf("step 1 finish=%q usage=%+v", got.finish, got.usage)
	}

	if _, err := m.Stream(context.Background(), userRequest("two")); !errors.Is(err, boom) {
		t.Fatalf("step 2 err = %v", err)
	}

	got = mockRun(t, m, userRequest("three"))
	if got.text != "abcdefghij" || got.deltas != 4 || got.finish != FinishLength || got.usage == nil {
		t.Fatalf("step 3 = %+v", got)
	}

	got = mockRun(t, m, userRequest("four"))
	if got.text != "partial" || !errors.Is(got.err, midway) || got.done {
		t.Fatalf("step 4 = %+v", got)
	}

	// Script exhausted: falls back to the echo behaviour.
	got = mockRun(t, m, userRequest("five"))
	if got.text != "Mock 回复：five" || got.finish != FinishStop {
		t.Fatalf("fallback = %+v", got)
	}

	if m.Calls() != 5 {
		t.Fatalf("calls = %d, want 5", m.Calls())
	}
	reqs := m.Requests()
	if len(reqs) != 5 {
		t.Fatalf("requests = %d", len(reqs))
	}
	for i, want := range []string{"one", "two", "three", "four", "five"} {
		if got := reqs[i].Messages[0].Text(); got != want || reqs[i].Model != "mock-model" {
			t.Fatalf("request %d = %q", i, got)
		}
	}
	// The returned slice is a copy.
	reqs[0] = Request{}
	reqs = append(reqs, Request{})
	if again := m.Requests(); len(again) != 5 || again[0].Messages[0].Text() != "one" {
		t.Fatal("Requests() exposed internal state")
	}
}

func TestMockRecordsRequestSnapshot(t *testing.T) {
	m := NewMock("m")
	msgs := []Message{TextMessage(RoleUser, "original")}
	mockRun(t, m, Request{Messages: msgs})
	msgs[0] = TextMessage(RoleUser, "mutated")
	if got := m.Requests()[0].Messages[0].Text(); got != "original" {
		t.Fatalf("recorded request changed with the caller's slice: %q", got)
	}
}

func TestMockConcurrentUse(t *testing.T) {
	m := NewScriptedMock("m", MockStep{Text: "a"}, MockStep{Text: "b"})
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stream, err := m.Stream(context.Background(), userRequest("x"))
			if err != nil {
				t.Errorf("stream: %v", err)
				return
			}
			for range stream.Events() {
			}
			_ = m.Requests()
		}()
	}
	wg.Wait()
	if m.Calls() != 20 {
		t.Fatalf("calls = %d", m.Calls())
	}
}

func TestMockToolDirective(t *testing.T) {
	m := NewMock("m")
	user := TextMessage(RoleUser, `please [[tool:search {"q":"go","tags":[["a"]]}]] and [[tool:now]]`)
	got := mockRun(t, m, Request{Messages: []Message{user}})
	if len(got.calls) != 2 || got.text != "" || got.finish != FinishToolCalls || got.usage == nil {
		t.Fatalf("first step = %+v", got)
	}
	if c := got.calls[0]; c.Name != "search" || string(c.Arguments) != `{"q":"go","tags":[["a"]]}` || c.ID == "" {
		t.Fatalf("call 0 = %+v (%s)", c, c.Arguments)
	}
	if c := got.calls[1]; c.Name != "now" || string(c.Arguments) != `{}` || c.ID == got.calls[0].ID {
		t.Fatalf("call 1 = %+v (%s)", c, c.Arguments)
	}

	// Second step of the same turn: the tool result is last, so the mock
	// answers in text instead of calling the tool again.
	long := strings.Repeat("结", 250)
	followUp := Request{Messages: []Message{
		user,
		AssistantMessage("", "", got.calls),
		ToolResultMessage(got.calls[0].ID, "search", "ignored", false),
		ToolResultMessage(got.calls[1].ID, "now", long, false),
	}}
	got = mockRun(t, m, followUp)
	want := "Mock 工具结果：" + strings.Repeat("结", 200)
	if got.text != want || len(got.calls) != 0 || got.finish != FinishStop {
		t.Fatalf("second step text = %q calls = %v finish = %q", got.text, got.calls, got.finish)
	}

	// A later turn without a directive must not replay the old tool call.
	next := followUp
	next.Messages = append(append([]Message(nil), followUp.Messages...), AssistantMessage(want, "", nil), TextMessage(RoleUser, "thanks"))
	if got := mockRun(t, m, next); got.text != "Mock 回复：thanks" || len(got.calls) != 0 {
		t.Fatalf("next turn = %+v", got)
	}
}

func TestMockFailDirectives(t *testing.T) {
	tests := []struct {
		directive string
		status    int
		retryable bool
		overflow  bool
	}{
		{"[[fail:429]]", 429, true, false},
		{"[[fail:500]]", 500, true, false},
		{"[[fail:overflow]]", 400, false, true},
		{"[[fail:401]]", 401, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.directive, func(t *testing.T) {
			m := NewMock("m")
			_, err := m.Stream(context.Background(), userRequest("do it "+tt.directive))
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Status != tt.status || apiErr.Provider != "m" {
				t.Fatalf("err = %v", err)
			}
			if IsRetryable(err) != tt.retryable || IsContextOverflow(err) != tt.overflow {
				t.Fatalf("retryable=%v overflow=%v", IsRetryable(err), IsContextOverflow(err))
			}
			if m.Calls() != 1 {
				t.Fatalf("failed call not recorded: %d", m.Calls())
			}
		})
	}
}

func TestMockLongAndLengthDirectives(t *testing.T) {
	m := NewMock("m")
	got := mockRun(t, m, userRequest("[[long:1234]]"))
	if n := utf8.RuneCountInString(got.text); n != 1234 {
		t.Fatalf("long reply has %d characters, want 1234", n)
	}
	if got.deltas < 2 || got.finish != FinishStop {
		t.Fatalf("deltas=%d finish=%q", got.deltas, got.finish)
	}

	got = mockRun(t, m, userRequest("cut me off [[length]]"))
	if got.finish != FinishLength || !got.done || got.text == "" {
		t.Fatalf("length = %+v", got)
	}

	got = mockRun(t, m, userRequest("[[long:50]] [[length]]"))
	if utf8.RuneCountInString(got.text) != 50 || got.finish != FinishLength {
		t.Fatalf("combined = %+v", got)
	}
}

func TestMockSlowDirective(t *testing.T) {
	m := NewMock("m")
	start := time.Now()
	got := mockRun(t, m, userRequest("[[slow:150]] hi"))
	if elapsed := time.Since(start); elapsed < 140*time.Millisecond {
		t.Fatalf("reply arrived after %v, want >= 150ms", elapsed)
	}
	if !got.done {
		t.Fatalf("got %+v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := m.Stream(ctx, userRequest("[[slow:60000]]"))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	start = time.Now()
	cancel()
	got = collect(t, stream)
	if got.done || got.text != "" || time.Since(start) > 2*time.Second {
		t.Fatalf("cancelled slow reply = %+v after %v", got, time.Since(start))
	}
}

func TestMockCancelUnblocksEmitter(t *testing.T) {
	// More deltas than the channel buffers and nobody reading.
	m := NewScriptedMock("m", MockStep{Text: strings.Repeat("x", 500), Chunks: 500})
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := m.Stream(ctx, userRequest("x"))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	if got := collect(t, stream); got.done {
		t.Fatalf("cancelled stream reported done: %+v", got)
	}
}

func TestMockJSONMode(t *testing.T) {
	m := NewMock("m")
	tests := []struct {
		name string
		msgs []Message
		want string
	}{
		{"default", []Message{TextMessage(RoleUser, "decide")}, `{"action":"finish","reason":"mock"}`},
		{"user directive", []Message{TextMessage(RoleUser, `decide [[json:{"action":"continue","list":[[1,2]]}]] now`)}, `{"action":"continue","list":[[1,2]]}`},
		{"system directive", []Message{TextMessage(RoleSystem, `[[json:{"from":"system"}]]`), TextMessage(RoleUser, "decide")}, `{"from":"system"}`},
		{"latest wins", []Message{TextMessage(RoleUser, `[[json:{"n":1}]]`), TextMessage(RoleUser, `[[json:{"n":2}]]`)}, `{"n":2}`},
		{"tool directive ignored", []Message{TextMessage(RoleUser, `[[tool:search {"q":"x"}]]`)}, `{"action":"finish","reason":"mock"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mockRun(t, m, Request{JSONMode: true, Messages: tt.msgs})
			if got.text != tt.want || !json.Valid([]byte(got.text)) {
				t.Fatalf("text = %q, want %q", got.text, tt.want)
			}
			if len(got.calls) != 0 || got.finish != FinishStop || got.usage == nil {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestMockIgnoresUnknownBrackets(t *testing.T) {
	m := NewMock("m")
	for _, text := range []string{"see [[wiki link]] here", "[[tool:]]", `[[tool:x {"broken"]]`, "[[long:abc]]", "[[unterminated"} {
		got := mockRun(t, m, userRequest(text))
		if got.text != "Mock 回复："+text || len(got.calls) != 0 {
			t.Errorf("%q -> %+v", text, got)
		}
	}
	// A directive after a non-directive bracket pair is still found.
	if got := mockRun(t, m, userRequest("[[note]] [[length]]")); got.finish != FinishLength {
		t.Errorf("directive after unknown bracket was missed: %+v", got)
	}
}
