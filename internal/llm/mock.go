package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MockProvider is a deterministic provider used by tests and for local UI
// development without burning real tokens. Its provider kind is "mock".
//
// Unscripted, it echoes the latest user message and understands inline
// directives in it so tool flows and failure paths can be exercised by hand:
//
//	[[tool:NAME {json args}]]  reply with that tool call (first step of a turn)
//	[[fail:429]] [[fail:500]] [[fail:overflow]]  fail Stream with an APIError
//	[[long:N]]                 reply with N characters of filler
//	[[length]]                 finish with reason "length"
//	[[slow:MS]]                wait MS milliseconds before replying
//	[[json:{...}]]             the reply to a JSONMode request
type MockProvider struct {
	name string

	mu       sync.Mutex
	steps    []MockStep
	next     int
	requests []Request
	callSeq  int
}

// MockStep scripts the outcome of one Stream call.
type MockStep struct {
	Text      string
	Reasoning string
	ToolCalls []ToolCall
	// Usage defaults to an estimate derived from the request and reply.
	Usage *Usage
	// FinishReason defaults to tool_calls when ToolCalls is set, else stop.
	FinishReason string
	// Err is returned from Stream itself.
	Err error
	// StreamErr is delivered as the terminal EventError after the deltas.
	StreamErr error
	Delay     time.Duration
	// Chunks splits Text into that many deltas (one when <= 1).
	Chunks int
}

func NewMock(name string) *MockProvider {
	if name == "" {
		name = "mock"
	}
	return &MockProvider{name: name}
}

// NewScriptedMock returns a mock whose Stream calls consume steps in order.
// Once they run out it behaves like NewMock.
func NewScriptedMock(name string, steps ...MockStep) *MockProvider {
	m := NewMock(name)
	m.steps = append(m.steps, steps...)
	return m
}

func (m *MockProvider) Name() string { return m.name }

// Requests returns a copy of every request passed to Stream so far.
func (m *MockProvider) Requests() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Request(nil), m.requests...)
}

// Calls returns how many times Stream has been called.
func (m *MockProvider) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

func (m *MockProvider) Stream(ctx context.Context, req Request) (Stream, error) {
	recorded := req
	recorded.Messages = append([]Message(nil), req.Messages...)
	recorded.Tools = append([]ToolDef(nil), req.Tools...)

	m.mu.Lock()
	m.requests = append(m.requests, recorded)
	var step MockStep
	if m.next < len(m.steps) {
		step = m.steps[m.next]
		m.next++
	} else {
		step = m.defaultStep(req)
	}
	m.mu.Unlock()

	if step.Err != nil {
		return nil, step.Err
	}

	streamCtx, cancel := context.WithCancel(ctx)
	ch := make(chan Event, 8)
	go func() {
		defer close(ch)
		emit := func(ev Event) bool {
			select {
			case ch <- ev:
				return true
			case <-streamCtx.Done():
				return false
			}
		}
		if step.Delay > 0 && sleepContext(streamCtx, step.Delay) != nil {
			return
		}
		if streamCtx.Err() != nil {
			return
		}
		if step.Reasoning != "" && !emit(Event{Type: EventReasoningDelta, Text: step.Reasoning}) {
			return
		}
		for _, chunk := range splitChunks(step.Text, step.Chunks) {
			if !emit(Event{Type: EventTextDelta, Text: chunk}) {
				return
			}
		}
		if step.StreamErr != nil {
			emit(Event{Type: EventError, Err: step.StreamErr})
			return
		}
		if len(step.ToolCalls) > 0 && !emit(Event{Type: EventToolCalls, ToolCalls: step.ToolCalls}) {
			return
		}
		usage := step.Usage
		if usage == nil {
			usage = &Usage{
				PromptTokens:     mockPromptSize(req)/2 + 1,
				CompletionTokens: (len(step.Text)+len(step.Reasoning))/2 + 1,
			}
		}
		finish := step.FinishReason
		if finish == "" {
			finish = FinishStop
			if len(step.ToolCalls) > 0 {
				finish = FinishToolCalls
			}
		}
		if emit(Event{Type: EventUsage, Usage: usage}) {
			emit(Event{Type: EventDone, FinishReason: finish})
		}
	}()
	return NewChanStream(ch, cancel), nil
}

// defaultStep derives the reply from the request. Callers hold m.mu.
func (m *MockProvider) defaultStep(req Request) MockStep {
	last := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == RoleUser {
			last = strings.TrimSpace(req.Messages[i].Text())
			break
		}
	}
	d := parseMockDirectives(last)
	step := MockStep{Delay: d.slow}
	if d.length {
		step.FinishReason = FinishLength
	}

	toolResult, afterTool := lastToolResult(req)
	switch {
	case d.fail != "" && !afterTool:
		step.Err = m.failure(d.fail)
	case req.JSONMode:
		step.Text = `{"action":"finish","reason":"mock"}`
		for i := len(req.Messages) - 1; i >= 0; i-- {
			msg := req.Messages[i]
			if msg.Role != RoleUser && msg.Role != RoleSystem {
				continue
			}
			if raw := parseMockDirectives(msg.Text()).json; raw != nil {
				step.Text = string(raw)
				break
			}
		}
	case afterTool:
		step.Text = "Mock 工具结果：" + truncateRunes(toolResult, 200)
	case len(d.tools) > 0:
		for _, call := range d.tools {
			m.callSeq++
			call.ID = fmt.Sprintf("mock_call_%d", m.callSeq)
			step.ToolCalls = append(step.ToolCalls, call)
		}
	case d.long > 0:
		step.Text = mockFiller(d.long)
		step.Chunks = min(d.long/40+1, 50)
	default:
		if last == "" {
			last = "(empty)"
		}
		step.Text = fmt.Sprintf("Mock 回复：%s", last)
	}
	return step
}

func (m *MockProvider) failure(kind string) error {
	switch kind {
	case "overflow":
		return &APIError{
			Provider: m.name,
			Status:   http.StatusBadRequest,
			Message:  "mock: this model's maximum context length was exceeded (context_length_exceeded)",
		}
	case "429":
		return &APIError{Provider: m.name, Status: http.StatusTooManyRequests, Message: "mock: rate limited", RetryAfter: time.Second}
	}
	status, err := strconv.Atoi(kind)
	if err != nil || status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	return &APIError{Provider: m.name, Status: status, Message: "mock: injected failure"}
}

// lastToolResult reports whether the request ends with a tool result, i.e.
// the model is being asked to continue after a tool ran.
func lastToolResult(req Request) (string, bool) {
	if len(req.Messages) == 0 {
		return "", false
	}
	parts := req.Messages[len(req.Messages)-1].Parts
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i].Type == PartToolResult && parts[i].ToolResult != nil {
			return parts[i].ToolResult.Content, true
		}
	}
	return "", false
}

type mockDirectives struct {
	tools  []ToolCall
	fail   string
	long   int
	length bool
	slow   time.Duration
	json   json.RawMessage
}

const mockMaxLong = 1_000_000

func parseMockDirectives(text string) mockDirectives {
	var d mockDirectives
	rest := text
	for {
		start := strings.Index(rest, "[[")
		if start < 0 {
			return d
		}
		rest = rest[start+2:]
		switch {
		case strings.HasPrefix(rest, "tool:"):
			body := rest[len("tool:"):]
			nameEnd := strings.IndexAny(body, " \t\n{]")
			if nameEnd <= 0 {
				continue
			}
			call := ToolCall{Name: body[:nameEnd], Arguments: json.RawMessage("{}")}
			body = strings.TrimLeft(body[nameEnd:], " \t\n")
			if !strings.HasPrefix(body, "]]") {
				raw, after, ok := cutMockJSON(body)
				if !ok {
					continue
				}
				call.Arguments, body = raw, after
			}
			d.tools = append(d.tools, call)
			rest = body
		case strings.HasPrefix(rest, "json:"):
			if raw, after, ok := cutMockJSON(rest[len("json:"):]); ok {
				d.json, rest = raw, after
			}
		default:
			end := strings.Index(rest, "]]")
			if end < 0 {
				return d
			}
			key, arg, _ := strings.Cut(rest[:end], ":")
			arg = strings.TrimSpace(arg)
			switch key {
			case "fail":
				d.fail = arg
			case "length":
				d.length = true
			case "long":
				if n, err := strconv.Atoi(arg); err == nil && n > 0 {
					d.long = min(n, mockMaxLong)
				}
			case "slow":
				if ms, err := strconv.Atoi(arg); err == nil && ms > 0 {
					d.slow = time.Duration(ms) * time.Millisecond
				}
			default:
				// Not a directive (e.g. a wiki link); keep scanning inside it.
				continue
			}
			rest = rest[end+2:]
		}
	}
}

// cutMockJSON reads one JSON value followed by "]]". A decoder is used rather
// than searching for "]]" because the JSON itself may contain brackets.
func cutMockJSON(s string) (json.RawMessage, string, bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, s, false
	}
	after := strings.TrimLeft(s[dec.InputOffset():], " \t\n")
	if !strings.HasPrefix(after, "]]") {
		return nil, s, false
	}
	return raw, after[2:], true
}

func mockFiller(n int) string {
	const unit = "这是一段用于测试的模拟长文本。"
	runes := []rune(unit)
	out := make([]rune, n)
	for i := range out {
		out[i] = runes[i%len(runes)]
	}
	return string(out)
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func splitChunks(text string, chunks int) []string {
	if text == "" {
		return nil
	}
	runes := []rune(text)
	if chunks <= 1 || len(runes) <= 1 {
		return []string{text}
	}
	chunks = min(chunks, len(runes))
	out := make([]string, 0, chunks)
	for i := range chunks {
		out = append(out, string(runes[i*len(runes)/chunks:(i+1)*len(runes)/chunks]))
	}
	return out
}

func mockPromptSize(req Request) int {
	size := 0
	for _, msg := range req.Messages {
		for _, part := range msg.Parts {
			size += len(part.Text)
			if part.ToolCall != nil {
				size += len(part.ToolCall.Arguments)
			}
			if part.ToolResult != nil {
				size += len(part.ToolResult.Content)
			}
		}
	}
	return size
}
