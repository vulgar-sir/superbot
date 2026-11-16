//go:build integration

package agent

// These tests look at the exact bytes the engine sends to an
// OpenAI-compatible provider. A local HTTP server plays a thinking model
// (reasoning deltas, tool calls) and records every request, so protocol rules
// such as "reasoning must be passed back within a tool loop" can be checked
// without a real provider.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"superbot/internal/store"
)

type wireMessage struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	ReasoningContent *string         `json:"reasoning_content"`
	ToolCalls        []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
	ToolCallID string `json:"tool_call_id"`
}

type wireRequest struct {
	Model    string        `json:"model"`
	Messages []wireMessage `json:"messages"`
}

func (m wireMessage) text() string {
	var text string
	if json.Unmarshal(m.Content, &text) == nil {
		return text
	}
	return string(m.Content)
}

// thinkingServer answers like a reasoning model. The reply for each request
// is chosen by the script function from the request it received.
type thinkingServer struct {
	mu       sync.Mutex
	requests []wireRequest
	script   func(req wireRequest) (reasoning, text string, tool *[2]string)
	server   *httptest.Server
	paranoid bool
}

func newThinkingServer(t *testing.T, script func(req wireRequest) (string, string, *[2]string)) *thinkingServer {
	t.Helper()
	ts := &thinkingServer{script: script}
	ts.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req wireRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ts.mu.Lock()
		ts.requests = append(ts.requests, req)
		index := len(ts.requests)
		ts.mu.Unlock()

		// The rules a thinking provider enforces: an assistant message that
		// comes after the last user message, or that does not directly
		// answer a user message, is an unfinished step and must carry its
		// reasoning. In paranoid mode the first request is refused for any
		// assistant message at all, to exercise the flattening fallback.
		lastUser := -1
		for i, msg := range req.Messages {
			if msg.Role == "user" {
				lastUser = i
			}
		}
		for i, msg := range req.Messages {
			unfinished := i > lastUser || (i > 0 && req.Messages[i-1].Role != "user")
			if ts.paranoid && index == 1 {
				unfinished = true
			}
			if msg.Role == "assistant" && unfinished && msg.ReasoningContent == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":{"message":"The reasoning_content in the thinking mode must be passed back to the API."}}`)
				return
			}
		}

		reasoning, text, tool := ts.script(req)
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(delta map[string]any, finish string) {
			choice := map[string]any{"index": 0, "delta": delta}
			if finish != "" {
				choice["finish_reason"] = finish
			}
			chunk, _ := json.Marshal(map[string]any{"choices": []any{choice}})
			fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		if reasoning != "" {
			send(map[string]any{"reasoning_content": reasoning}, "")
		}
		if text != "" {
			send(map[string]any{"content": text}, "")
		}
		if tool != nil {
			send(map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": fmt.Sprintf("call_%d", index), "type": "function",
				"function": map[string]any{"name": tool[0], "arguments": tool[1]},
			}}}, "tool_calls")
		} else {
			send(map[string]any{}, "stop")
		}
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(ts.server.Close)
	return ts
}

func (ts *thinkingServer) recorded() []wireRequest {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]wireRequest(nil), ts.requests...)
}

func outline(req wireRequest) string {
	var b strings.Builder
	for i, msg := range req.Messages {
		reasoning := "-"
		if msg.ReasoningContent != nil {
			reasoning = fmt.Sprintf("%d", len(*msg.ReasoningContent))
		}
		fmt.Fprintf(&b, "  [%d] %-9s tools=%d reasoning=%s %q\n", i, msg.Role, len(msg.ToolCalls), reasoning,
			truncateRunes(strings.ReplaceAll(msg.text(), "\n", " "), 50))
	}
	return b.String()
}

// thinkingProvider registers the fake server as a DeepSeek-style provider.
func (f *fixture) thinkingProvider(ts *thinkingServer) store.Provider {
	f.t.Helper()
	ctx := context.Background()
	provider, err := f.db.CreateProvider(ctx, store.CreateProviderParams{
		OwnerID: f.account, Type: "llm", Kind: "openai_compat", Name: "thinking-" + f.provider.Name,
		BaseUrl: ts.server.URL, Config: json.RawMessage(`{"preset":"deepseek"}`), Enabled: true,
	})
	if err != nil {
		f.t.Fatalf("create provider: %v", err)
	}
	if _, err := f.db.CreateModel(ctx, store.CreateModelParams{
		ProviderID: provider.ID, Name: "mock-model", ContextWindow: 128_000, MaxOutput: 8_000, SupportsTools: true,
	}); err != nil {
		f.t.Fatalf("create model: %v", err)
	}
	return provider
}

func (f *fixture) usingProvider(provider store.Provider, bot store.Bot) store.Bot {
	f.t.Helper()
	if _, err := f.db.Pool.Exec(context.Background(), `UPDATE bots SET provider_id = $1 WHERE id = $2`, provider.ID, bot.ID); err != nil {
		f.t.Fatalf("switch provider: %v", err)
	}
	bot.ProviderID = &provider.ID
	return bot
}

func TestIntegrationWireReasoningEchoedInsideToolLoop(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ts := newThinkingServer(t, func(req wireRequest) (string, string, *[2]string) {
		last := req.Messages[len(req.Messages)-1]
		if last.Role == "tool" {
			return "结果拿到了", "查完了", nil
		}
		return "需要先查历史", "我先查一下", &[2]string{"search_chat_history", `{"query":"zzz"}`}
	})
	bot := f.usingProvider(f.thinkingProvider(ts), f.bot("Thinker", true))
	conv := f.direct(bot)
	sent := f.send(conv, "帮我查一下")
	f.drain(1)
	if cycle := f.cycle(sent.Cycle.ID); cycle.Status != "done" {
		t.Fatalf("cycle status = %s (%s)", cycle.Status, cycle.Error)
	}
	requests := ts.recorded()
	if len(requests) != 2 {
		t.Fatalf("want 2 requests (tool step + final), got %d", len(requests))
	}
	var echoed *string
	for _, msg := range requests[1].Messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			echoed = msg.ReasoningContent
		}
	}
	if echoed == nil || *echoed != "需要先查历史" {
		t.Fatalf("the tool-call turn's reasoning was not passed back:\n%s", outline(requests[1]))
	}
}

func TestIntegrationWireAfterDeliveredReply(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	var helperName string
	ts := newThinkingServer(t, func(req wireRequest) (string, string, *[2]string) {
		last := req.Messages[len(req.Messages)-1]
		all := ""
		for _, msg := range req.Messages {
			all += msg.text() + "\n"
		}
		switch {
		case last.Role == "tool":
			return "已经发出去了", "已发送，本轮结束", nil
		case strings.Contains(last.text(), "回复了你的私信"):
			return "对方回复了", "对方的答案是 391", nil
		case strings.Contains(all, "Bot 私信线程"):
			return "这是队友来问", "391", nil
		default:
			args, _ := json.Marshal(map[string]string{"bot_name": helperName, "message": "17 乘以 23 等于多少"})
			return "需要问队友", "我去问一下", &[2]string{"send_to_agent", string(args)}
		}
	})
	provider := f.thinkingProvider(ts)
	asker := f.usingProvider(provider, f.bot("WireAsker", true))
	helper := f.usingProvider(provider, f.bot("WireHelper", true))
	helperName = helper.Name
	conv := f.direct(asker)
	f.send(conv, "问一下队友 17 乘以 23")
	f.drain(6)

	requests := ts.recorded()
	for index, req := range requests {
		t.Logf("request %d:\n%s", index+1, outline(req))
	}
	replies := botReplies(f.messages(conv))
	if len(replies) != 2 || !strings.Contains(replies[1].Content, "391") {
		var errors []string
		rows, _ := f.db.Pool.Query(context.Background(), `SELECT status, coalesce(error,'') FROM cycles WHERE conversation_id = $1 ORDER BY created_at`, conv.ID)
		for rows.Next() {
			var status, message string
			_ = rows.Scan(&status, &message)
			errors = append(errors, status+": "+message)
		}
		rows.Close()
		t.Fatalf("the asker did not continue with the delivered reply; cycles: %v", errors)
	}
}

func TestIntegrationWireNoSystemMessagesMidTranscript(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ts := newThinkingServer(t, func(req wireRequest) (string, string, *[2]string) {
		return "想好了", "好的", nil
	})
	bot := f.usingProvider(f.thinkingProvider(ts), f.bot("Plain", true))
	conv := f.direct(bot)
	ctx := context.Background()
	f.send(conv, "第一句")
	f.drain(1)
	// Cards and notices land between turns in real conversations.
	for _, meta := range []string{
		`{"kind":"agent_thread","direction":"sent","preview":"x"}`,
		`{"approval_id":"00000000-0000-0000-0000-000000000000","tool_name":"shell"}`,
		`{"kind":"subagent_result","inject":"true","inject_text":"子代理结果：42"}`,
	} {
		if _, err := f.db.CreateMessage(ctx, store.CreateMessageParams{
			ConversationID: conv.ID, SenderType: "system", Content: "卡片或通知", Metadata: json.RawMessage(meta),
		}); err != nil {
			t.Fatalf("seed notice: %v", err)
		}
	}
	second := f.send(conv, "第二句")
	f.drain(1)
	if cycle := f.cycle(second.Cycle.ID); cycle.Status != "done" {
		t.Fatalf("cycle status = %s (%s)", cycle.Status, cycle.Error)
	}
	requests := ts.recorded()
	last := requests[len(requests)-1]
	text := ""
	for index, msg := range last.Messages {
		text += msg.text() + "\n"
		if msg.Role == "system" && index > 0 && index < len(last.Messages)-1 && last.Messages[index-1].Role != "system" {
			t.Fatalf("system message in the middle of the transcript:\n%s", outline(last))
		}
	}
	if !strings.Contains(text, "子代理结果：42") {
		t.Fatalf("a delivered result must reach the model:\n%s", outline(last))
	}
	if strings.Count(text, "卡片或通知") != 1 {
		t.Fatalf("the display-only card should be skipped and the approval notice kept:\n%s", outline(last))
	}
}

func TestIntegrationWireFlattensWhenProviderRejectsShape(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ts := newThinkingServer(t, func(req wireRequest) (string, string, *[2]string) {
		return "想好了", "收到：" + req.Messages[len(req.Messages)-1].text(), nil
	})
	bot := f.usingProvider(f.thinkingProvider(ts), f.bot("Flat", true))
	conv := f.direct(bot)
	ctx := context.Background()
	for i, sender := range []string{"user", "bot", "user", "bot"} {
		params := store.CreateMessageParams{
			ConversationID: conv.ID, SenderType: sender, Metadata: json.RawMessage(`{}`),
			Content: fmt.Sprintf("历史第 %d 句", i+1),
		}
		if sender == "bot" {
			params.SenderBotID = &bot.ID
		}
		if _, err := f.db.CreateMessage(ctx, params); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	ts.paranoid = true
	sent := f.send(conv, "现在的问题")
	f.drain(1)
	if cycle := f.cycle(sent.Cycle.ID); cycle.Status != "done" {
		t.Fatalf("the engine did not recover from the provider's refusal: %s (%s)", cycle.Status, cycle.Error)
	}
	requests := ts.recorded()
	if len(requests) != 2 {
		t.Fatalf("want the refused request and one flattened retry, got %d", len(requests))
	}
	retry := requests[1]
	for _, msg := range retry.Messages {
		if msg.Role == "assistant" {
			t.Fatalf("flattened retry still contains an assistant message:\n%s", outline(retry))
		}
	}
	final := retry.Messages[len(retry.Messages)-1].text()
	if !strings.Contains(final, "历史第 2 句") || !strings.Contains(final, "现在的问题") {
		t.Fatalf("flattened retry lost history or the question: %q", final)
	}
}

func TestIntegrationWireReplyArrivingDuringTurnComesAfterIt(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	ts := newThinkingServer(t, func(req wireRequest) (string, string, *[2]string) {
		last := req.Messages[len(req.Messages)-1]
		if strings.Contains(last.text(), "慢一点") {
			time.Sleep(1200 * time.Millisecond)
			return "想好了", "我已经去问了，等回复", nil
		}
		return "看到回复了", "队友说是 391", nil
	})
	bot := f.usingProvider(f.thinkingProvider(ts), f.bot("Racer", true))
	conv := f.direct(bot)
	f.send(conv, "慢一点，去问队友 17 乘以 23")

	done := make(chan struct{})
	go func() {
		defer close(done)
		f.step(ctx)
	}()
	time.Sleep(300 * time.Millisecond)
	// The teammate's answer is stored while the bot is still writing.
	meta, _ := json.Marshal(map[string]any{
		"kind": "agent_thread", "direction": "received", "inject": "true",
		"inject_text": "队友回复了你的私信：391", "for_bot_id": bot.ID.String(),
	})
	card, err := f.db.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: conv.ID, SenderType: "system", Content: "来自 队友 的消息", Metadata: meta,
	})
	if err != nil {
		t.Fatalf("create card: %v", err)
	}
	f.engine.wakeForResult(ctx, conv.ID, card)
	<-done
	f.drain(2)

	replies := botReplies(f.messages(conv))
	if len(replies) != 2 || !strings.Contains(replies[1].Content, "391") {
		t.Fatalf("the late reply should get its own turn: %+v", replies)
	}
	requests := ts.recorded()
	last := requests[len(requests)-1]
	tail := last.Messages[len(last.Messages)-1]
	for i := len(last.Messages) - 1; i >= 0 && tail.Role == "system"; i-- {
		tail = last.Messages[i]
	}
	if tail.Role != "user" || !strings.Contains(tail.text(), "391") {
		t.Fatalf("the follow-up must end with the delivered reply as a user turn:\n%s", outline(last))
	}
}
