package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"superbot/internal/llm"
	"superbot/internal/store"
)

func toolRound(id, result string) []llm.Message {
	return []llm.Message{
		llm.AssistantMessage("", "", []llm.ToolCall{{ID: id, Name: "shell", Arguments: []byte(`{"command":"ls"}`)}}),
		llm.ToolResultMessage(id, "shell", result, false),
	}
}

func assertNoOrphanToolResults(t *testing.T, messages []llm.Message) {
	t.Helper()
	open := map[string]bool{}
	for _, msg := range messages {
		for _, call := range msg.ToolCalls() {
			open[call.ID] = true
		}
		for _, part := range msg.Parts {
			if part.Type == llm.PartToolResult && !open[part.ToolResult.CallID] {
				t.Fatalf("tool result %q has no preceding tool call", part.ToolResult.CallID)
			}
		}
	}
}

func TestTrimHistoryKeepsPinnedPrefixAndToolPairs(t *testing.T) {
	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, "system"),
		llm.TextMessage(llm.RoleSystem, "checkpoint summary"),
	}
	for i := 0; i < 6; i++ {
		messages = append(messages, llm.TextMessage(llm.RoleUser, strings.Repeat("问题 ", 40)))
		messages = append(messages, toolRound(fmt.Sprintf("call%d", i), strings.Repeat("x", 400))...)
	}
	trimmed := trimHistory(messages, 2, 300, 3)
	if trimmed[0].Text() != "system" || trimmed[1].Text() != "checkpoint summary" {
		t.Fatalf("pinned prefix was dropped: %q %q", trimmed[0].Text(), trimmed[1].Text())
	}
	if len(trimmed) >= len(messages) {
		t.Fatal("expected history to shrink")
	}
	if trimmed[2].Role == llm.RoleTool {
		t.Fatal("kept window starts with an orphaned tool result")
	}
	assertNoOrphanToolResults(t, trimmed)
}

func TestFitLoopMessagesElidesOldToolResultsFirst(t *testing.T) {
	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, "system"),
		llm.TextMessage(llm.RoleUser, "请整理仓库"),
	}
	for i := 0; i < 8; i++ {
		messages = append(messages, toolRound(fmt.Sprintf("c%d", i), strings.Repeat("输出", 3000))...)
	}
	before := EstimateMessagesTokens(messages)
	fitted, changed := fitLoopMessages(messages, 1, before/3)
	if !changed {
		t.Fatal("expected compaction")
	}
	if got := EstimateMessagesTokens(fitted); got > before/3 {
		t.Fatalf("still over budget: %d > %d", got, before/3)
	}
	assertNoOrphanToolResults(t, fitted)
	if fitted[0].Text() != "system" {
		t.Fatal("system prompt lost")
	}
	// The input must not be mutated: the progress checkpoint still holds it.
	if len([]rune(messages[3].Parts[0].ToolResult.Content)) != 6000 {
		t.Fatal("fitLoopMessages mutated its input")
	}
	// Eliding old results alone is enough here, so nothing should be dropped
	// and the newest result must stay intact for the model to act on.
	if len(fitted) != len(messages) {
		t.Fatalf("eliding should have sufficed, but %d messages were dropped", len(messages)-len(fitted))
	}
	if last := fitted[len(fitted)-1].Parts[0].ToolResult.Content; strings.Contains(last, "已省略") {
		t.Fatal("most recent tool result was elided")
	}
	if first := fitted[3].Parts[0].ToolResult.Content; !strings.Contains(first, "已省略") {
		t.Fatal("oldest tool result should have been elided")
	}
}

func TestFitLoopMessagesHardCutsOversizedRecentMessages(t *testing.T) {
	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, "system"),
		llm.TextMessage(llm.RoleUser, strings.Repeat("长", 50_000)),
	}
	fitted, changed := fitLoopMessages(messages, 1, 3000)
	if !changed || EstimateMessagesTokens(fitted) > 3000 {
		t.Fatalf("oversized recent message not cut: %d tokens", EstimateMessagesTokens(fitted))
	}
}

func TestFitLoopMessagesNoopWithinBudget(t *testing.T) {
	messages := []llm.Message{llm.TextMessage(llm.RoleSystem, "s"), llm.TextMessage(llm.RoleUser, "hi")}
	fitted, changed := fitLoopMessages(messages, 1, 10_000)
	if changed || len(fitted) != 2 {
		t.Fatal("small transcript must pass through untouched")
	}
}

func storedMessage(ordinal int64, sender, content string) store.Message {
	return store.Message{
		ID: uuid.New(), Ordinal: ordinal, SenderType: sender, Content: content,
		CreatedAt: time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC),
	}
}

func TestPlanCompaction(t *testing.T) {
	var history []store.Message
	for i := int64(1); i <= 40; i++ {
		sender := "user"
		if i%2 == 0 {
			sender = "bot"
		}
		history = append(history, storedMessage(i, sender, strings.Repeat("内容", 200)))
	}
	if plan := planCompaction(history[:4], 100_000); plan != nil {
		t.Fatal("short history must not be compacted")
	}
	if plan := planCompaction(history, 1_000_000); plan != nil {
		t.Fatal("history far below the check line must not be compacted")
	}
	plan := planCompaction(history, 8000)
	if plan == nil {
		t.Fatal("expected a compaction plan")
	}
	if len(plan.Summarize)+len(plan.Keep) != len(history) {
		t.Fatal("plan lost messages")
	}
	if len(plan.Keep) < compactMinKeep {
		t.Fatalf("kept only %d messages", len(plan.Keep))
	}
	if plan.Keep[0].Ordinal != plan.Summarize[len(plan.Summarize)-1].Ordinal+1 {
		t.Fatal("plan is not a clean cut")
	}
}

func TestPlanCompactionByRowCountOnHugeContext(t *testing.T) {
	// A 1M-token model never hits the token check line with short messages,
	// but the history query is bounded by rows, so the count must trigger a
	// fold that really folds something.
	var history []store.Message
	for i := int64(1); i <= 171; i++ {
		history = append(history, storedMessage(i, "user", "一条很短的消息"))
	}
	plan := planCompaction(history, 1_000_000)
	if plan == nil {
		t.Fatal("more rows than one turn loads must be compacted even on a huge context window")
	}
	if len(plan.Keep) > compactKeepRows+compactMinKeep*2 || len(plan.Keep) < compactMinKeep {
		t.Fatalf("kept %d rows verbatim, want about %d", len(plan.Keep), compactKeepRows)
	}
	if len(plan.Summarize) < 100 {
		t.Fatalf("only %d rows were folded", len(plan.Summarize))
	}
	if plan := planCompaction(history[:compactMaxRows], 1_000_000); plan != nil {
		t.Fatal("at the row limit nothing needs folding yet")
	}
}

func TestValidSummary(t *testing.T) {
	var full strings.Builder
	for _, heading := range summaryHeadings {
		full.WriteString("## " + heading + "\n内容内容内容\n")
	}
	if !validSummary(full.String()) {
		t.Fatal("complete note rejected")
	}
	if validSummary("## 目标\n只写了一个标题，其余全部缺失，这不是一份可用的检查点笔记，应该被拒绝并重试。") {
		t.Fatal("note missing most headings accepted")
	}
	if validSummary("") {
		t.Fatal("empty note accepted")
	}
}

func TestVerbatimLinesCarryForwardWithinBudget(t *testing.T) {
	previous := composeCheckpoint(1, "## 目标\n旧目标", []string{"- [#3 2026-11-01] 最早的原话"})
	messages := []store.Message{
		storedMessage(10, "user", "部署包必须放在 /srv/releases"),
		storedMessage(11, "bot", "好的"),
		storedMessage(12, "user", "不要用假数据"),
	}
	lines := verbatimLines(previous, messages, 2000)
	if len(lines) != 3 {
		t.Fatalf("want 3 lines (1 carried + 2 new), got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "最早的原话") || !strings.Contains(lines[2], "不要用假数据") {
		t.Fatalf("wrong order or content: %v", lines)
	}
	for _, line := range lines {
		if strings.Contains(line, "好的") {
			t.Fatal("bot text leaked into the verbatim lines")
		}
	}
	tight := verbatimLines(previous, messages, 14)
	if len(tight) != 1 || !strings.Contains(tight[0], "不要用假数据") {
		t.Fatalf("under a tight budget only the newest line should survive: %v", tight)
	}
	if strings.Contains(stripVerbatim(previous), "最早的原话") {
		t.Fatal("stripVerbatim left verbatim lines in the body")
	}
}

func TestFallbackSummaryMentionsMessages(t *testing.T) {
	text := fallbackSummary("", []store.Message{storedMessage(7, "user", "记住端口是 32608")}, nil)
	if !strings.Contains(text, "#7") || !strings.Contains(text, "32608") {
		t.Fatalf("fallback lost content: %s", text)
	}
}

func TestIsPermanent(t *testing.T) {
	if !isPermanent(permanent(fmt.Errorf("bot has no provider"))) {
		t.Fatal("marked error must be permanent")
	}
	if !isPermanent(fmt.Errorf("wrapped: %w", permanent(fmt.Errorf("budget")))) {
		t.Fatal("wrapping must not hide a permanent error")
	}
	if isPermanent(fmt.Errorf("connection reset")) {
		t.Fatal("unclassified errors stay retryable")
	}
	if !isPermanent(&llm.APIError{Provider: "x", Status: 401, Message: "bad key"}) {
		t.Fatal("auth failures must not be retried")
	}
	if isPermanent(&llm.APIError{Provider: "x", Status: 503, Message: "busy"}) {
		t.Fatal("5xx must stay retryable")
	}
}

func TestFlattenTurns(t *testing.T) {
	image := llm.Part{Type: llm.PartImage, Image: &llm.Image{MIMEType: "image/png", Data: []byte{1}}}
	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, "system"),
		llm.TextMessage(llm.RoleSystem, "checkpoint"),
		llm.TextMessage(llm.RoleUser, "第一问"),
		llm.TextMessage(llm.RoleAssistant, "第一答"),
		{Role: llm.RoleUser, Parts: []llm.Part{{Type: llm.PartText, Text: "第二问"}, image}},
		llm.TextMessage(llm.RoleSystem, "trailing memory block"),
	}
	flat, ok := flattenTurns(messages, 2)
	if !ok {
		t.Fatal("a transcript without tool calls should be flattenable")
	}
	if len(flat) != 4 || flat[0].Text() != "system" || flat[1].Text() != "checkpoint" {
		t.Fatalf("pinned prefix must be kept as is: %d messages", len(flat))
	}
	for _, msg := range flat {
		if msg.Role == llm.RoleAssistant {
			t.Fatal("flattened transcript must not contain assistant messages")
		}
	}
	merged := flat[2]
	if merged.Role != llm.RoleUser || !strings.Contains(merged.Text(), "第一答") ||
		!strings.HasSuffix(merged.Text(), "第二问") {
		t.Fatalf("history and question must end up in one user message: %q", merged.Text())
	}
	if len(merged.Parts) != 2 || merged.Parts[1].Type != llm.PartImage {
		t.Fatal("images on the current message must survive")
	}
	if flat[3].Text() != "trailing memory block" {
		t.Fatal("blocks after the current message must be kept")
	}

	inLoop := append(messages[:5:5], toolRound("c1", "result")...)
	if _, ok := flattenTurns(inLoop, 2); ok {
		t.Fatal("a tool loop in progress must not be flattened")
	}
	if _, ok := flattenTurns(messages[:3], 2); ok {
		t.Fatal("nothing to flatten when the question is the only message")
	}
}

func TestIsReasoningProtocolError(t *testing.T) {
	refused := &llm.APIError{Provider: "DeepSeek", Status: 400,
		Message: "The `reasoning_content` in the thinking mode must be passed back to the API."}
	if !isReasoningProtocolError(fmt.Errorf("wrapped: %w", refused)) {
		t.Fatal("the provider's refusal must be recognised")
	}
	if isReasoningProtocolError(&llm.APIError{Status: 400, Message: "model not found"}) ||
		isReasoningProtocolError(&llm.APIError{Status: 500, Message: "reasoning_content"}) ||
		isReasoningProtocolError(fmt.Errorf("reasoning_content")) {
		t.Fatal("unrelated errors must not trigger the fallback")
	}
}

func TestOrderByDeliveryMovesUnseenArrivalsAfterReply(t *testing.T) {
	bot := uuid.New()
	other := uuid.New()
	reply := storedMessage(13, "bot", "已发出，等回复")
	reply.SenderBotID = &bot
	reply.Metadata = []byte(`{"seen_ordinal":11}`)
	teammate := storedMessage(12, "system", "队友回复：391")
	question := storedMessage(11, "user", "去问队友")
	older := storedMessage(5, "bot", "很早的回复")
	older.SenderBotID = &bot
	older.Metadata = []byte(`{"seen_ordinal":4}`)
	peer := storedMessage(14, "bot", "别的 Bot")
	peer.SenderBotID = &other

	got := orderByDelivery([]store.Message{older, question, teammate, reply, peer}, bot)
	want := []int64{5, 11, 13, 12, 14}
	for i, row := range got {
		if row.Ordinal != want[i] {
			t.Fatalf("order = %v, want %v", ordinals(got), want)
		}
	}

	// Replies without the marker (older data) keep stored order.
	plain := storedMessage(13, "bot", "旧数据")
	plain.SenderBotID = &bot
	if got := orderByDelivery([]store.Message{question, teammate, plain}, bot); got[2].Ordinal != 13 {
		t.Fatalf("unmarked replies must not be reordered: %v", ordinals(got))
	}
	// A reply never jumps over another reply of the same bot.
	first := storedMessage(20, "bot", "第一段")
	first.SenderBotID = &bot
	first.Metadata = []byte(`{"seen_ordinal":19}`)
	second := storedMessage(21, "bot", "第二段")
	second.SenderBotID = &bot
	second.Metadata = []byte(`{"seen_ordinal":1}`)
	if got := orderByDelivery([]store.Message{first, second}, bot); got[0].Ordinal != 20 {
		t.Fatalf("own replies must keep their order: %v", ordinals(got))
	}
}

func ordinals(rows []store.Message) []int64 {
	out := make([]int64, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Ordinal)
	}
	return out
}

func TestEndWithUserTurn(t *testing.T) {
	endsAssistant := []llm.Message{
		llm.TextMessage(llm.RoleSystem, "s"),
		llm.TextMessage(llm.RoleUser, "q"),
		llm.TextMessage(llm.RoleAssistant, "a"),
		llm.TextMessage(llm.RoleSystem, "memory"),
	}
	fixed := endWithUserTurn(endsAssistant)
	if len(fixed) != 5 || fixed[3].Role != llm.RoleUser || fixed[4].Text() != "memory" {
		t.Fatalf("a user turn must be inserted after the trailing assistant message: %+v", fixed)
	}
	fine := endsAssistant[:2]
	if got := endWithUserTurn(fine); len(got) != 2 {
		t.Fatal("a transcript that already ends with the user must be left alone")
	}
}
