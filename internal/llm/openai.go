package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAICompat talks to any provider that implements the OpenAI Chat
// Completions streaming API (DeepSeek, Moonshot, Qwen compatible mode, GLM,
// Volcengine Ark, OpenRouter, Ollama, vLLM, ...).
type OpenAICompat struct {
	name    string
	baseURL string
	apiKey  string
	tr      *transport

	// EchoReasoning sends the reasoning of assistant messages that carry tool
	// calls back as `reasoning_content`. Some thinking models (DeepSeek, ...)
	// reject a tool-call turn without it; most other providers reject or
	// ignore the field, so it is off by default.
	EchoReasoning bool
}

type OpenAIOption func(*OpenAICompat)

func WithEchoReasoning(on bool) OpenAIOption {
	return func(p *OpenAICompat) { p.EchoReasoning = on }
}

// NewOpenAICompat builds a provider. timeout is the stream idle timeout: the
// stream is aborted when no bytes arrive for that long (120s when <= 0).
func NewOpenAICompat(name, baseURL, apiKey string, timeout time.Duration, opts ...OpenAIOption) *OpenAICompat {
	p := &OpenAICompat{
		name:    name,
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		tr:      newTransport(name, timeout),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *OpenAICompat) Name() string { return p.name }

func (p *OpenAICompat) endpoint() string {
	if strings.HasSuffix(p.baseURL, "/chat/completions") {
		return p.baseURL
	}
	return p.baseURL + "/chat/completions"
}

func (p *OpenAICompat) Stream(ctx context.Context, req Request) (Stream, error) {
	payload, err := json.Marshal(p.buildRequest(req))
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
		if p.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
		return httpReq, nil
	}
	return p.tr.stream(ctx, newReq, parseOpenAIStream)
}

func parseOpenAIStream(ctx context.Context, body io.Reader, emit func(Event)) error {
	toolCalls := map[int]*ToolCall{}
	var usage *Usage
	finish := ""

	err := scanSSE(body, func(data string) (bool, error) {
		if data == "[DONE]" {
			return true, nil
		}
		var chunk oaiChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return false, fmt.Errorf("decode: %w", err)
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			return false, fmt.Errorf("provider error: %s", chunk.Error.Message)
		}
		if chunk.Usage != nil {
			usage = &Usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CachedTokens:     chunk.Usage.cachedTokens(),
				CompletionTokens: chunk.Usage.CompletionTokens,
			}
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				finish = normalizeOpenAIFinish(choice.FinishReason)
			}
			delta := choice.Delta
			if delta.Content != "" {
				emit(Event{Type: EventTextDelta, Text: delta.Content})
			}
			if delta.ReasoningContent != "" {
				emit(Event{Type: EventReasoningDelta, Text: delta.ReasoningContent})
			}
			for _, tc := range delta.ToolCalls {
				cur, ok := toolCalls[tc.Index]
				if !ok {
					cur = &ToolCall{}
					toolCalls[tc.Index] = cur
				}
				if tc.ID != "" {
					cur.ID = tc.ID
				}
				if tc.Function.Name != "" {
					cur.Name = tc.Function.Name
				}
				cur.Arguments = append(cur.Arguments, tc.Function.Arguments...)
			}
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}

	calls := finalToolCalls(toolCalls)
	if len(calls) > 0 {
		emit(Event{Type: EventToolCalls, ToolCalls: calls})
		// Several compatible providers report "stop" (or nothing) on a
		// tool-call turn.
		if finish == "" || finish == FinishStop {
			finish = FinishToolCalls
		}
	}
	if usage != nil {
		emit(Event{Type: EventUsage, Usage: usage})
	}
	emit(Event{Type: EventDone, FinishReason: finish})
	return nil
}

func normalizeOpenAIFinish(reason string) string {
	switch reason {
	case "stop":
		return FinishStop
	case "length":
		return FinishLength
	case "tool_calls", "function_call":
		return FinishToolCalls
	case "content_filter":
		return FinishContentFilter
	default:
		return ""
	}
}

type oaiRequest struct {
	Model          string             `json:"model"`
	Messages       []oaiMessage       `json:"messages"`
	Tools          []oaiTool          `json:"tools,omitempty"`
	Temperature    *float64           `json:"temperature,omitempty"`
	MaxTokens      int                `json:"max_tokens,omitempty"`
	Stream         bool               `json:"stream"`
	StreamOptions  *oaiStreamOpts     `json:"stream_options,omitempty"`
	ResponseFormat *oaiResponseFormat `json:"response_format,omitempty"`
}

type oaiStreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

type oaiResponseFormat struct {
	Type string `json:"type"`
}

type oaiMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
	Name    string `json:"name,omitempty"`
	// Pointer so an empty reasoning string is still sent when echoing.
	ReasoningContent *string       `json:"reasoning_content,omitempty"`
	ToolCalls        []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string        `json:"tool_call_id,omitempty"`
}

type oaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type oaiContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *oaiImageURL `json:"image_url,omitempty"`
}

type oaiImageURL struct {
	URL string `json:"url"`
}

func (p *OpenAICompat) buildRequest(req Request) oaiRequest {
	out := oaiRequest{
		Model:         req.Model,
		Messages:      make([]oaiMessage, 0, len(req.Messages)),
		Temperature:   req.Temperature,
		MaxTokens:     req.MaxTokens,
		Stream:        true,
		StreamOptions: &oaiStreamOpts{IncludeUsage: true},
	}
	if req.JSONMode {
		out.ResponseFormat = &oaiResponseFormat{Type: "json_object"}
	}
	for _, tool := range req.Tools {
		params := tool.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out.Tools = append(out.Tools, oaiTool{
			Type: "function",
			Function: oaiFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  params,
			},
		})
	}
	for _, msg := range req.Messages {
		out.Messages = append(out.Messages, p.serializeMessage(msg)...)
	}
	return out
}

func (p *OpenAICompat) serializeMessage(msg Message) []oaiMessage {
	switch msg.Role {
	case RoleTool:
		var out []oaiMessage
		for _, part := range msg.Parts {
			if part.Type != PartToolResult || part.ToolResult == nil {
				continue
			}
			out = append(out, oaiMessage{
				Role:       "tool",
				ToolCallID: part.ToolResult.CallID,
				Content:    part.ToolResult.Content,
			})
		}
		return out
	case RoleAssistant:
		calls := msg.ToolCalls()
		oai := oaiMessage{Role: "assistant", Content: msg.Text()}
		for _, call := range calls {
			args := string(call.Arguments)
			if args == "" {
				args = "{}"
			}
			tc := oaiToolCall{ID: call.ID, Type: "function"}
			tc.Function.Name = call.Name
			tc.Function.Arguments = args
			oai.ToolCalls = append(oai.ToolCalls, tc)
		}
		if p.EchoReasoning && len(oai.ToolCalls) > 0 {
			reasoning := msg.Reasoning()
			oai.ReasoningContent = &reasoning
		}
		return []oaiMessage{oai}
	default:
		content := p.serializeContent(msg)
		return []oaiMessage{{Role: string(msg.Role), Content: content, Name: msg.Name}}
	}
}

func (p *OpenAICompat) serializeContent(msg Message) any {
	hasImage := false
	for _, part := range msg.Parts {
		if part.Type == PartImage && part.Image != nil {
			hasImage = true
			break
		}
	}
	if !hasImage {
		return msg.Text()
	}
	parts := make([]oaiContentPart, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		switch part.Type {
		case PartText:
			parts = append(parts, oaiContentPart{Type: "text", Text: part.Text})
		case PartImage:
			if part.Image == nil {
				continue
			}
			parts = append(parts, oaiContentPart{
				Type: "image_url",
				ImageURL: &oaiImageURL{
					URL: fmt.Sprintf("data:%s;base64,%s", part.Image.MIMEType, base64.StdEncoding.EncodeToString(part.Image.Data)),
				},
			})
		}
	}
	return parts
}

type oaiChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *oaiUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type oaiUsage struct {
	PromptTokens          int `json:"prompt_tokens"`
	CompletionTokens      int `json:"completion_tokens"`
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
	PromptTokensDetails   *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func (u *oaiUsage) cachedTokens() int {
	if u == nil {
		return 0
	}
	if u.PromptCacheHitTokens > 0 {
		return u.PromptCacheHitTokens
	}
	if u.PromptTokensDetails != nil {
		return u.PromptTokensDetails.CachedTokens
	}
	return 0
}
