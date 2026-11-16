package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"superbot/internal/llm"
	"superbot/internal/store"
)

const (
	// historyLoadLimit bounds how many rows one turn reads; the token budget,
	// not this number, decides what the model finally sees.
	historyLoadLimit = 200
	// compactCheckRatio is the share of the prompt budget history may use
	// before older messages are folded into a checkpoint summary.
	compactCheckRatio = 0.6
	// compactKeepRatio is the share of the budget kept verbatim after a
	// checkpoint, so the model still sees the live exchange word for word.
	compactKeepRatio = 0.25
	compactMinKeep   = 6
	compactMaxRows   = 150
	// compactKeepRows caps the verbatim window by count. Models with very
	// large context windows would otherwise keep everything on token
	// grounds while the history query silently drops the oldest rows.
	compactKeepRows       = 40
	summaryInputMaxRunes  = 60_000
	summaryMaxTokens      = 1600
	verbatimBudgetTokens  = 2000
	verbatimLineMaxRunes  = 600
	elidedToolResultRunes = 200
	loopKeepRecent        = 4

	verbatimHeading = "## 用户原话（逐字保留）"
)

// summaryHeadings are the sections a checkpoint must contain. A note missing
// most of them is treated as a failed summary rather than trusted.
var summaryHeadings = []string{
	"目标", "约束与偏好", "进展", "决定", "需保留的事实", "未完成事项", "下一步", "去哪里找",
}

// promptBudget is the token room available for the prompt after reserving
// space for the reply.
func promptBudget(modelRow *store.Model) int {
	contextWindow := defaultContextWindow
	if modelRow != nil && modelRow.ContextWindow > 0 {
		contextWindow = int(modelRow.ContextWindow)
	}
	reserve := 8192
	if modelRow != nil && modelRow.MaxOutput > 0 {
		reserve = int(modelRow.MaxOutput)
	}
	budget := contextWindow - reserve - 2000
	if budget < contextWindow/2 {
		budget = contextWindow / 2
	}
	if budget < 4000 {
		budget = 4000
	}
	return budget
}

// trimHistory drops the oldest unpinned messages until the prompt fits. The
// first `pinned` messages (system prompt, checkpoint summary, attachments)
// and the newest minKeep messages always survive, and the kept window never
// starts with an orphaned tool result.
func trimHistory(messages []llm.Message, pinned, budget, minKeep int) []llm.Message {
	if pinned < 1 {
		pinned = 1
	}
	if minKeep < 2 {
		minKeep = 2
	}
	if pinned > len(messages) {
		pinned = len(messages)
	}
	for len(messages) > pinned+minKeep && EstimateMessagesTokens(messages) > budget {
		drop := 1
		// An assistant message that issued tool calls goes together with its
		// results; providers reject a tool result without its call.
		for pinned+drop < len(messages)-minKeep && messages[pinned+drop].Role == llm.RoleTool {
			drop++
		}
		messages = append(messages[:pinned:pinned], messages[pinned+drop:]...)
	}
	for len(messages) > pinned && messages[pinned].Role == llm.RoleTool {
		messages = append(messages[:pinned:pinned], messages[pinned+1:]...)
	}
	return messages
}

// trimMessagesToBudget keeps the historical single-prefix behaviour.
func trimMessagesToBudget(messages []llm.Message, budget, minKeep int) []llm.Message {
	if len(messages) <= 2 {
		return messages
	}
	return trimHistory(messages, 1, budget, minKeep)
}

// fitLoopMessages keeps a growing tool loop inside the prompt budget. It
// degrades in order of information value: shorten old tool results, drop the
// oldest history, then hard-cut whatever is still too large.
func fitLoopMessages(messages []llm.Message, pinned, budget int) ([]llm.Message, bool) {
	if EstimateMessagesTokens(messages) <= budget {
		return messages, false
	}
	out := make([]llm.Message, len(messages))
	copy(out, messages)

	for i := pinned; i < len(out)-loopKeepRecent; i++ {
		if out[i].Role != llm.RoleTool {
			continue
		}
		out[i] = elideToolResult(out[i], elidedToolResultRunes)
	}
	if EstimateMessagesTokens(out) <= budget {
		return out, true
	}

	out = trimHistory(out, pinned, budget, loopKeepRecent)
	if EstimateMessagesTokens(out) <= budget {
		return out, true
	}

	// Still too large: the recent messages themselves are oversized.
	for limit := 8000; limit >= 500 && EstimateMessagesTokens(out) > budget; limit /= 2 {
		for i := pinned; i < len(out); i++ {
			out[i] = capMessageText(out[i], limit)
		}
	}
	return out, true
}

func elideToolResult(msg llm.Message, keepRunes int) llm.Message {
	parts := make([]llm.Part, len(msg.Parts))
	copy(parts, msg.Parts)
	for i, part := range parts {
		if part.Type != llm.PartToolResult || part.ToolResult == nil {
			continue
		}
		runes := []rune(part.ToolResult.Content)
		if len(runes) <= keepRunes*2 {
			continue
		}
		result := *part.ToolResult
		result.Content = string(runes[:keepRunes]) +
			"\n…[较早的工具结果已省略以节省上下文；完整内容保存在工作区 artifacts 目录，可按需重新读取]"
		parts[i].ToolResult = &result
	}
	msg.Parts = parts
	return msg
}

func capMessageText(msg llm.Message, limit int) llm.Message {
	parts := make([]llm.Part, len(msg.Parts))
	copy(parts, msg.Parts)
	for i, part := range parts {
		switch part.Type {
		case llm.PartText, llm.PartReasoning:
			if runes := []rune(part.Text); len(runes) > limit {
				parts[i].Text = string(runes[:limit]) + "\n…[内容过长已截断]"
			}
		case llm.PartToolResult:
			if part.ToolResult == nil {
				continue
			}
			if runes := []rune(part.ToolResult.Content); len(runes) > limit {
				result := *part.ToolResult
				result.Content = string(runes[:limit]) + "\n…[内容过长已截断]"
				parts[i].ToolResult = &result
			}
		}
	}
	msg.Parts = parts
	return msg
}

// compactionPlan says which stored messages a checkpoint should absorb.
type compactionPlan struct {
	Summarize []store.Message
	Keep      []store.Message
}

// planCompaction decides whether the history after the last checkpoint has
// outgrown its share of the budget and, if so, where to cut. It returns nil
// when nothing needs to be folded.
func planCompaction(history []store.Message, budget int) *compactionPlan {
	if len(history) <= compactMinKeep {
		return nil
	}
	total := 0
	for _, msg := range history {
		total += EstimateTokens(msg.Content) + 4
	}
	checkLine := int(float64(budget) * compactCheckRatio)
	if total <= checkLine && len(history) <= compactMaxRows {
		return nil
	}
	keepBudget := int(float64(budget) * compactKeepRatio)
	keepFrom := len(history)
	used := 0
	for i := len(history) - 1; i >= 0; i-- {
		cost := EstimateTokens(history[i].Content) + 4
		kept := len(history) - i
		if kept > compactMinKeep && (used+cost > keepBudget || kept > compactKeepRows) {
			break
		}
		used += cost
		keepFrom = i
	}
	// Never cut between a user message and the reply it is still waiting on:
	// the kept window should start at a user message when one is near.
	for keepFrom > 0 && keepFrom < len(history) && history[keepFrom].SenderType != "user" &&
		len(history)-keepFrom < compactMinKeep*2 {
		keepFrom--
	}
	if keepFrom <= 0 {
		return nil
	}
	return &compactionPlan{Summarize: history[:keepFrom], Keep: history[keepFrom:]}
}

func summarySystemPrompt() string {
	var b strings.Builder
	b.WriteString("你在为一个长期运行的 AI Bot 写「上下文检查点」。较早的对话即将从它的上下文中移除，它之后只能看到你写的这份笔记。\n")
	b.WriteString("严格使用下面 8 个二级标题，逐项填写；没有内容的写「无」。只写事实，不要寒暄，不要编造，不要给新的指令。\n\n")
	for _, heading := range summaryHeadings {
		fmt.Fprintf(&b, "## %s\n", heading)
	}
	b.WriteString("\n要求：\n")
	b.WriteString("- 「目标」写用户真正要达成的事；「约束与偏好」写用户明确提过的限制、口味、禁止事项。\n")
	b.WriteString("- 「进展」「决定」写已经做完和已经定下的；「未完成事项」「下一步」写还欠着的。\n")
	b.WriteString("- 「需保留的事实」写具体的名称、路径、数字、链接、账号名（不要写密码或密钥）。\n")
	b.WriteString("- 「去哪里找」写产物文件路径、文档、消息序号（形如 #123），方便之后回查。\n")
	b.WriteString("- 如果提供了「上一份检查点」，把仍然有效的内容合并进来，过时的删掉。\n")
	return b.String()
}

// summaryTranscript renders stored messages for the summarizer, newest part
// preserved when the transcript has to be cut.
func summaryTranscript(messages []store.Message, names map[string]string, maxRunes int) string {
	lines := make([]string, 0, len(messages))
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if runes := []rune(content); len(runes) > 2000 {
			content = string(runes[:2000]) + "…"
		}
		lines = append(lines, fmt.Sprintf("#%d %s: %s", msg.Ordinal, speakerLabel(msg, names), content))
	}
	text := strings.Join(lines, "\n\n")
	if runes := []rune(text); len(runes) > maxRunes {
		text = "…[更早的记录已截断]\n" + string(runes[len(runes)-maxRunes:])
	}
	return text
}

func speakerLabel(msg store.Message, names map[string]string) string {
	switch msg.SenderType {
	case "user":
		return "用户"
	case "bot":
		if msg.SenderBotID != nil {
			if name, ok := names[msg.SenderBotID.String()]; ok {
				return "[" + name + "]"
			}
		}
		return "[Bot]"
	default:
		return "系统"
	}
}

// validSummary reports whether the model produced a usable checkpoint.
func validSummary(text string) bool {
	text = strings.TrimSpace(text)
	if len([]rune(text)) < 40 {
		return false
	}
	found := 0
	for _, heading := range summaryHeadings {
		if strings.Contains(text, "## "+heading) || strings.Contains(text, "##"+heading) {
			found++
		}
	}
	return found >= len(summaryHeadings)-2
}

// fallbackSummary is used when the model cannot write a checkpoint. It is
// deliberately mechanical: better a blunt excerpt than a lost conversation.
func fallbackSummary(previous string, messages []store.Message, names map[string]string) string {
	var b strings.Builder
	b.WriteString("（自动摘要失败，以下为较早对话的机械摘录）\n")
	if body := stripVerbatim(previous); strings.TrimSpace(body) != "" {
		b.WriteString("\n### 上一份检查点\n")
		b.WriteString(truncateRunes(strings.TrimSpace(body), 3000))
		b.WriteString("\n")
	}
	b.WriteString("\n### 摘录\n")
	start := 0
	if len(messages) > 30 {
		start = len(messages) - 30
	}
	for _, msg := range messages[start:] {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		fmt.Fprintf(&b, "- #%d %s: %s\n", msg.Ordinal, speakerLabel(msg, names), truncateRunes(content, 300))
	}
	return b.String()
}

// verbatimLines keeps the user's own words next to the summary, newest first
// within the budget, then restores chronological order. Earlier verbatim
// lines carried by the previous checkpoint compete for the same budget.
func verbatimLines(previous string, messages []store.Message, budgetTokens int) []string {
	candidates := extractVerbatim(previous)
	for _, msg := range messages {
		if msg.SenderType != "user" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		content = strings.ReplaceAll(truncateRunes(content, verbatimLineMaxRunes), "\n", " ")
		candidates = append(candidates, fmt.Sprintf("- [#%d %s] %s",
			msg.Ordinal, msg.CreatedAt.Format(time.DateOnly), content))
	}
	used := 0
	from := len(candidates)
	for i := len(candidates) - 1; i >= 0; i-- {
		cost := EstimateTokens(candidates[i]) + 2
		if used+cost > budgetTokens {
			break
		}
		used += cost
		from = i
	}
	return candidates[from:]
}

func extractVerbatim(summary string) []string {
	index := strings.Index(summary, verbatimHeading)
	if index < 0 {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(summary[index+len(verbatimHeading):], "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- [#") {
			lines = append(lines, line)
		}
	}
	return lines
}

func stripVerbatim(summary string) string {
	if index := strings.Index(summary, verbatimHeading); index >= 0 {
		return summary[:index]
	}
	return summary
}

func composeCheckpoint(number int, body string, verbatim []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[检查点 %d]\n", number)
	b.WriteString(strings.TrimSpace(body))
	if len(verbatim) > 0 {
		b.WriteString("\n\n")
		b.WriteString(verbatimHeading)
		b.WriteString("\n")
		b.WriteString(strings.Join(verbatim, "\n"))
	}
	return b.String()
}

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

// isReasoningProtocolError recognises a thinking model refusing a request
// because it believes an earlier reasoning step was not passed back.
func isReasoningProtocolError(err error) bool {
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		return false
	}
	return strings.Contains(strings.ToLower(apiErr.Message), "reasoning_content")
}

// flattenTurns restates everything before the current user message as one
// plain-text record inside that message, leaving no assistant message in
// the request. It only applies before the first tool round: once a tool
// loop is under way its call/result pairs must stay structured.
func flattenTurns(messages []llm.Message, pinned int) ([]llm.Message, bool) {
	if pinned < 1 || pinned >= len(messages) {
		return messages, false
	}
	lastUser := -1
	for i := pinned; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role == llm.RoleTool || len(msg.ToolCalls()) > 0 {
			return messages, false
		}
		if msg.Role == llm.RoleUser {
			lastUser = i
		}
	}
	if lastUser <= pinned {
		return messages, false
	}
	var record strings.Builder
	record.WriteString("以下是此前的对话记录（按时间顺序，仅供参考）：\n")
	for _, msg := range messages[pinned:lastUser] {
		text := strings.TrimSpace(msg.Text())
		if text == "" {
			continue
		}
		speaker := "用户"
		switch msg.Role {
		case llm.RoleAssistant:
			speaker = "你"
		case llm.RoleSystem:
			speaker = "系统"
		}
		fmt.Fprintf(&record, "\n%s：%s\n", speaker, text)
	}
	record.WriteString("\n———\n当前消息：\n")

	current := messages[lastUser]
	parts := []llm.Part{{Type: llm.PartText, Text: record.String() + current.Text()}}
	for _, part := range current.Parts {
		if part.Type == llm.PartImage {
			parts = append(parts, part)
		}
	}
	out := make([]llm.Message, 0, pinned+1+len(messages)-lastUser)
	out = append(out, messages[:pinned]...)
	out = append(out, llm.Message{Role: llm.RoleUser, Parts: parts})
	out = append(out, messages[lastUser+1:]...)
	return out, true
}

// orderByDelivery moves messages that reached the conversation while one of
// the bot's own replies was being written, and that the reply never saw, to
// after that reply. Ordinals record when a row was stored, not what the bot
// had read: a teammate's answer can be stored a moment before the reply that
// asked for it, and a transcript that puts it first both misleads the model
// and ends on an assistant message, which providers read as an unfinished
// turn.
func orderByDelivery(rows []store.Message, botID uuid.UUID) []store.Message {
	out := make([]store.Message, 0, len(rows))
	for _, row := range rows {
		seen, ok := replySeenOrdinal(row, botID)
		if !ok {
			out = append(out, row)
			continue
		}
		split := len(out)
		for split > 0 && out[split-1].Ordinal > seen && !isOwnReply(out[split-1], botID) {
			split--
		}
		if split == len(out) {
			out = append(out, row)
			continue
		}
		late := append([]store.Message(nil), out[split:]...)
		out = append(append(out[:split], row), late...)
	}
	return out
}

func isOwnReply(row store.Message, botID uuid.UUID) bool {
	return row.SenderType == "bot" && row.SenderBotID != nil && *row.SenderBotID == botID
}

func replySeenOrdinal(row store.Message, botID uuid.UUID) (int64, bool) {
	if !isOwnReply(row, botID) || len(row.Metadata) == 0 {
		return 0, false
	}
	var meta struct {
		SeenOrdinal int64 `json:"seen_ordinal"`
	}
	if err := json.Unmarshal(row.Metadata, &meta); err != nil || meta.SeenOrdinal <= 0 {
		return 0, false
	}
	return meta.SeenOrdinal, true
}

// endWithUserTurn guarantees the request ends on a user message (trailing
// system blocks aside). A request that ends on an assistant message asks the
// model to continue that message, which thinking models reject and other
// models treat as a prefill.
func endWithUserTurn(messages []llm.Message) []llm.Message {
	for i := len(messages) - 1; i >= 0; i-- {
		switch messages[i].Role {
		case llm.RoleSystem:
			continue
		case llm.RoleAssistant:
			out := make([]llm.Message, 0, len(messages)+1)
			out = append(out, messages[:i+1]...)
			out = append(out, llm.TextMessage(llm.RoleUser, "【系统】请根据以上对话继续。"))
			return append(out, messages[i+1:]...)
		default:
			return messages
		}
	}
	return messages
}
