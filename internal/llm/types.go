package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Role is the canonical chat role used by every provider adapter.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type PartType string

const (
	PartText       PartType = "text"
	PartReasoning  PartType = "reasoning"
	PartToolCall   PartType = "tool_call"
	PartToolResult PartType = "tool_result"
	PartImage      PartType = "image"
)

type Part struct {
	Type       PartType    `json:"type"`
	Text       string      `json:"text,omitempty"`
	ToolCall   *ToolCall   `json:"tool_call,omitempty"`
	ToolResult *ToolResult `json:"tool_result,omitempty"`
	Image      *Image      `json:"image,omitempty"`
}

type Message struct {
	Role  Role   `json:"role"`
	Name  string `json:"name,omitempty"`
	Parts []Part `json:"parts"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolResult struct {
	CallID  string `json:"call_id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

type Image struct {
	MIMEType string `json:"mime_type"`
	Data     []byte `json:"data"`
}

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type Request struct {
	Model       string
	Messages    []Message
	Tools       []ToolDef
	Temperature *float64
	MaxTokens   int
	JSONMode    bool
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CachedTokens     int `json:"cached_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type EventType string

const (
	EventTextDelta      EventType = "text_delta"
	EventReasoningDelta EventType = "reasoning_delta"
	EventToolCalls      EventType = "tool_calls"
	EventUsage          EventType = "usage"
	EventDone           EventType = "done"
	EventError          EventType = "error"
)

// Normalized finish reasons carried by the EventDone event. An empty string
// means the provider did not report one (or reported something unknown).
const (
	FinishStop          = "stop"
	FinishLength        = "length"
	FinishToolCalls     = "tool_calls"
	FinishContentFilter = "content_filter"
)

type Event struct {
	Type      EventType
	Text      string
	ToolCalls []ToolCall
	Usage     *Usage
	Err       error
	// FinishReason is set on EventDone only.
	FinishReason string
}

// Stream is a provider-agnostic stream of events. The channel is closed after
// the final event (done or error).
type Stream interface {
	Events() <-chan Event
	Close() error
}

type Provider interface {
	Name() string
	Stream(ctx context.Context, req Request) (Stream, error)
}

func TextMessage(role Role, text string) Message {
	return Message{Role: role, Parts: []Part{{Type: PartText, Text: text}}}
}

func AssistantMessage(text, reasoning string, calls []ToolCall) Message {
	msg := Message{Role: RoleAssistant}
	if text != "" {
		msg.Parts = append(msg.Parts, Part{Type: PartText, Text: text})
	}
	if reasoning != "" {
		msg.Parts = append(msg.Parts, Part{Type: PartReasoning, Text: reasoning})
	}
	for i := range calls {
		c := calls[i]
		msg.Parts = append(msg.Parts, Part{Type: PartToolCall, ToolCall: &c})
	}
	return msg
}

func ToolResultMessage(callID, name, content string, isError bool) Message {
	return Message{
		Role: RoleTool,
		Parts: []Part{{
			Type:       PartToolResult,
			ToolResult: &ToolResult{CallID: callID, Name: name, Content: content, IsError: isError},
		}},
	}
}

func (m Message) Text() string {
	var out string
	for _, p := range m.Parts {
		if p.Type == PartText {
			out += p.Text
		}
	}
	return out
}

func (m Message) ToolCalls() []ToolCall {
	var out []ToolCall
	for _, p := range m.Parts {
		if p.Type == PartToolCall && p.ToolCall != nil {
			out = append(out, *p.ToolCall)
		}
	}
	return out
}

func (m Message) Reasoning() string {
	var out string
	for _, p := range m.Parts {
		if p.Type == PartReasoning {
			out += p.Text
		}
	}
	return out
}

var ErrStreamClosed = errors.New("stream closed")

// ChanStream adapts a channel to the Stream interface.
type ChanStream struct {
	ch     <-chan Event
	cancel context.CancelFunc
	once   func()
}

func NewChanStream(ch <-chan Event, cancel context.CancelFunc) *ChanStream {
	return &ChanStream{ch: ch, cancel: cancel}
}

func (s *ChanStream) Events() <-chan Event { return s.ch }

func (s *ChanStream) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func ValidateToolArguments(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("tool arguments must be a JSON object: %w", err)
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}
