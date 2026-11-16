package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	anthropicDefaultBaseURL   = "https://api.anthropic.com"
	anthropicVersion          = "2023-06-01"
	anthropicDefaultMaxTokens = 4096
)

// Anthropic talks to the Anthropic Messages API with SSE streaming.
type Anthropic struct {
	name    string
	baseURL string
	apiKey  string
	tr      *transport
}

// NewAnthropic builds a provider. An empty baseURL selects the public API;
// timeout is the stream idle timeout (120s when <= 0).
func NewAnthropic(name, baseURL, apiKey string, timeout time.Duration) *Anthropic {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = anthropicDefaultBaseURL
	}
	return &Anthropic{
		name:    name,
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(apiKey),
		tr:      newTransport(name, timeout),
	}
}

func (p *Anthropic) Name() string { return p.name }

func (p *Anthropic) endpoint() string {
	switch {
	case strings.HasSuffix(p.baseURL, "/v1/messages"):
		return p.baseURL
	case strings.HasSuffix(p.baseURL, "/v1"):
		return p.baseURL + "/messages"
	default:
		return p.baseURL + "/v1/messages"
	}
}

func (p *Anthropic) Stream(ctx context.Context, req Request) (Stream, error) {
	payload, err := json.Marshal(buildAnthropicRequest(req))
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	newReq := func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(), bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("anthropic-version", anthropicVersion)
		if p.apiKey != "" {
			httpReq.Header.Set("x-api-key", p.apiKey)
		}
		return httpReq, nil
	}
	return p.tr.stream(ctx, newReq, p.parseStream)
}

type antRequest struct {
	Model       string       `json:"model"`
	MaxTokens   int          `json:"max_tokens"`
	System      string       `json:"system,omitempty"`
	Messages    []antMessage `json:"messages"`
	Tools       []antTool    `json:"tools,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
	Stream      bool         `json:"stream"`
}

type antMessage struct {
	Role    string     `json:"role"`
	Content []antBlock `json:"content"`
}

type antBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Source    *antSource      `json:"source,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type antSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type antTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

func buildAnthropicRequest(req Request) antRequest {
	out := antRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Messages:    []antMessage{},
		Temperature: req.Temperature,
		Stream:      true,
	}
	if out.MaxTokens <= 0 {
		out.MaxTokens = anthropicDefaultMaxTokens
	}
	for _, tool := range req.Tools {
		schema := tool.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out.Tools = append(out.Tools, antTool{Name: tool.Name, Description: tool.Description, InputSchema: schema})
	}

	var system []string
	for _, msg := range req.Messages {
		if msg.Role == RoleSystem {
			if text := msg.Text(); text != "" {
				system = append(system, text)
			}
			continue
		}
		role := "user"
		if msg.Role == RoleAssistant {
			role = "assistant"
		}
		blocks := anthropicBlocks(msg)
		if len(blocks) == 0 {
			continue
		}
		// The API requires alternating roles, so consecutive same-role
		// messages (e.g. several tool results, or a tool result followed by a
		// user note) collapse into one.
		if n := len(out.Messages); n > 0 && out.Messages[n-1].Role == role {
			out.Messages[n-1].Content = append(out.Messages[n-1].Content, blocks...)
			continue
		}
		out.Messages = append(out.Messages, antMessage{Role: role, Content: blocks})
	}
	for i := range out.Messages {
		if out.Messages[i].Role == "user" {
			// tool_result blocks must precede any other content in the user
			// message that answers a tool_use turn.
			sort.SliceStable(out.Messages[i].Content, func(a, b int) bool {
				c := out.Messages[i].Content
				return c[a].Type == "tool_result" && c[b].Type != "tool_result"
			})
		}
	}
	out.System = strings.Join(system, "\n\n")
	return out
}

func anthropicBlocks(msg Message) []antBlock {
	var blocks []antBlock
	for _, part := range msg.Parts {
		switch part.Type {
		case PartText:
			// Empty text blocks are rejected by the API.
			if part.Text != "" {
				blocks = append(blocks, antBlock{Type: "text", Text: part.Text})
			}
		case PartImage:
			if part.Image == nil {
				continue
			}
			blocks = append(blocks, antBlock{Type: "image", Source: &antSource{
				Type:      "base64",
				MediaType: part.Image.MIMEType,
				Data:      base64.StdEncoding.EncodeToString(part.Image.Data),
			}})
		case PartToolCall:
			if part.ToolCall == nil || msg.Role != RoleAssistant {
				continue
			}
			blocks = append(blocks, antBlock{
				Type:  "tool_use",
				ID:    part.ToolCall.ID,
				Name:  part.ToolCall.Name,
				Input: anthropicToolInput(part.ToolCall.Arguments),
			})
		case PartToolResult:
			if part.ToolResult == nil || msg.Role == RoleAssistant {
				continue
			}
			blocks = append(blocks, antBlock{
				Type:      "tool_result",
				ToolUseID: part.ToolResult.CallID,
				Content:   part.ToolResult.Content,
				IsError:   part.ToolResult.IsError,
			})
		}
		// PartReasoning is dropped: thinking blocks can only be replayed with
		// the provider's signature, which our message model does not keep.
	}
	return blocks
}

// anthropicToolInput guarantees a JSON object, which tool_use.input requires.
func anthropicToolInput(args json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(args)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return json.RawMessage("{}")
	}
	return json.RawMessage(trimmed)
}

type antEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		Usage *antUsage `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *antUsage `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type antUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
}

func (p *Anthropic) parseStream(ctx context.Context, body io.Reader, emit func(Event)) error {
	toolCalls := map[int]*ToolCall{}
	var raw antUsage
	sawUsage := false
	finish := ""
	stopped := false

	// message_delta usage is cumulative and may omit the input counters, so
	// only non-zero values overwrite what message_start reported.
	mergeUsage := func(u *antUsage) {
		if u == nil {
			return
		}
		sawUsage = true
		if u.InputTokens > 0 {
			raw.InputTokens = u.InputTokens
		}
		if u.CacheReadInputTokens > 0 {
			raw.CacheReadInputTokens = u.CacheReadInputTokens
		}
		if u.CacheCreationInputTokens > 0 {
			raw.CacheCreationInputTokens = u.CacheCreationInputTokens
		}
		if u.OutputTokens > 0 {
			raw.OutputTokens = u.OutputTokens
		}
	}

	err := scanSSE(body, func(data string) (bool, error) {
		var ev antEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return false, fmt.Errorf("decode: %w", err)
		}
		switch ev.Type {
		case "message_start":
			if ev.Message != nil {
				mergeUsage(ev.Message.Usage)
			}
		case "content_block_start":
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
				toolCalls[ev.Index] = &ToolCall{ID: ev.ContentBlock.ID, Name: ev.ContentBlock.Name}
			}
		case "content_block_delta":
			if ev.Delta == nil {
				break
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" {
					emit(Event{Type: EventTextDelta, Text: ev.Delta.Text})
				}
			case "thinking_delta":
				if ev.Delta.Thinking != "" {
					emit(Event{Type: EventReasoningDelta, Text: ev.Delta.Thinking})
				}
			case "input_json_delta":
				if call, ok := toolCalls[ev.Index]; ok {
					call.Arguments = append(call.Arguments, ev.Delta.PartialJSON...)
				}
			}
		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				finish = normalizeAnthropicStop(ev.Delta.StopReason)
			}
			mergeUsage(ev.Usage)
		case "message_stop":
			stopped = true
			return true, nil
		case "error":
			return false, p.streamError(ev)
		}
		// ping, content_block_stop and unknown event types need no action.
		return false, nil
	})
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}
	if !stopped {
		return fmt.Errorf("ended before message_stop: %w", io.ErrUnexpectedEOF)
	}

	if calls := finalToolCalls(toolCalls); len(calls) > 0 {
		emit(Event{Type: EventToolCalls, ToolCalls: calls})
	}
	if sawUsage {
		emit(Event{Type: EventUsage, Usage: &Usage{
			PromptTokens:     raw.InputTokens + raw.CacheReadInputTokens + raw.CacheCreationInputTokens,
			CachedTokens:     raw.CacheReadInputTokens,
			CompletionTokens: raw.OutputTokens,
		}})
	}
	emit(Event{Type: EventDone, FinishReason: finish})
	return nil
}

func normalizeAnthropicStop(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return FinishStop
	case "max_tokens", "model_context_window_exceeded":
		return FinishLength
	case "tool_use":
		return FinishToolCalls
	case "refusal":
		return FinishContentFilter
	default:
		return ""
	}
}

// streamError maps an in-stream error event to the HTTP status the same
// failure would have had as a response, so IsRetryable and friends apply.
func (p *Anthropic) streamError(ev antEvent) error {
	apiErr := &APIError{Provider: p.name, Message: "unknown stream error"}
	if ev.Error == nil {
		return apiErr
	}
	apiErr.Message = ev.Error.Type + ": " + ev.Error.Message
	switch ev.Error.Type {
	case "invalid_request_error":
		apiErr.Status = http.StatusBadRequest
	case "authentication_error":
		apiErr.Status = http.StatusUnauthorized
	case "permission_error":
		apiErr.Status = http.StatusForbidden
	case "not_found_error":
		apiErr.Status = http.StatusNotFound
	case "request_too_large":
		apiErr.Status = http.StatusRequestEntityTooLarge
	case "rate_limit_error":
		apiErr.Status = http.StatusTooManyRequests
	case "timeout_error":
		apiErr.Status = http.StatusGatewayTimeout
	case "overloaded_error":
		apiErr.Status = 529
	default:
		apiErr.Status = http.StatusInternalServerError
	}
	return apiErr
}
