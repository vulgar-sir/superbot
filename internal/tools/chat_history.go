package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"superbot/internal/store"
)

const (
	historySearchLimit   = 12
	historyAroundWindow  = 2000
	historyAroundEach    = 6
	historySnippetRunes  = 600
	historyAroundRunes   = 1500
	historyAroundMaxRows = 400
)

// chatHistoryTool lets a bot look up what has left its context: older
// messages are folded into a checkpoint summary, but the originals stay in
// the database and can be found by keyword or reopened around a message.
type chatHistoryTool struct {
	db     *store.DB
	convID uuid.UUID
}

func NewChatHistoryTool(db *store.DB, convID uuid.UUID) Tool {
	return &chatHistoryTool{db: db, convID: convID}
}

func (t *chatHistoryTool) Name() string { return "search_chat_history" }

func (t *chatHistoryTool) Description() string {
	return "在当前对话的完整历史里回查原文。较早的消息可能已被整理成检查点而不在你的上下文里；" +
		"用 query 按关键词搜索，或用 around 给出消息序号（检查点里形如 #123）查看它前后的原文。只能查当前对话。"
}

func (t *chatHistoryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "要搜索的关键词或短语（子串匹配，不区分大小写）"},
			"around": {"type": "integer", "description": "消息序号；返回该消息前后各几条原文"}
		}
	}`)
}

func (t *chatHistoryTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Query  string `json:"query"`
		Around int64  `json:"around"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	query := strings.TrimSpace(input.Query)
	switch {
	case input.Around > 0:
		return t.around(ctx, input.Around)
	case query != "":
		return t.search(ctx, query)
	default:
		return Result{Content: "需要提供 query 或 around 之一。", IsError: true}, nil
	}
}

func (t *chatHistoryTool) search(ctx context.Context, query string) (Result, error) {
	rows, err := t.db.SearchConversationMessages(ctx, store.SearchConversationMessagesParams{
		ConversationID: t.convID,
		Query:          escapeLike(query),
		MaxRows:        historySearchLimit,
	})
	if err != nil {
		return Result{Content: "搜索失败: " + err.Error(), IsError: true}, nil
	}
	if len(rows) == 0 {
		return Result{Content: "没有找到包含「" + query + "」的消息。可以换一个更短或不同的关键词。"}, nil
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Ordinal < rows[j].Ordinal })
	var b strings.Builder
	fmt.Fprintf(&b, "找到 %d 条（按时间先后；用 around=序号 可查看上下文）：\n", len(rows))
	for _, row := range rows {
		b.WriteString(formatHistoryRow(row, snippetAround(row.Content, query, historySnippetRunes)))
	}
	return Result{Content: b.String()}, nil
}

func (t *chatHistoryTool) around(ctx context.Context, ordinal int64) (Result, error) {
	// Ordinals are global across conversations, so a window is read and the
	// nearest rows of this conversation are picked from it.
	rows, err := t.db.ListMessagesInOrdinalRange(ctx, store.ListMessagesInOrdinalRangeParams{
		ConversationID: t.convID,
		FromOrdinal:    ordinal - historyAroundWindow,
		ToOrdinal:      ordinal + historyAroundWindow,
		MaxRows:        historyAroundMaxRows,
	})
	if err != nil {
		return Result{Content: "读取失败: " + err.Error(), IsError: true}, nil
	}
	picked := pickAround(rows, ordinal, historyAroundEach)
	if len(picked) == 0 {
		return Result{Content: fmt.Sprintf("当前对话里没有序号 #%d 附近的消息。", ordinal)}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "#%d 附近的原文：\n", ordinal)
	for _, row := range picked {
		b.WriteString(formatHistoryRow(row, truncateHistory(row.Content, historyAroundRunes)))
	}
	return Result{Content: b.String()}, nil
}

// pickAround returns up to `each` rows on either side of the target ordinal,
// plus the target itself, in chronological order.
func pickAround(rows []store.Message, ordinal int64, each int) []store.Message {
	sort.Slice(rows, func(i, j int) bool { return rows[i].Ordinal < rows[j].Ordinal })
	split := sort.Search(len(rows), func(i int) bool { return rows[i].Ordinal >= ordinal })
	from := split - each
	if from < 0 {
		from = 0
	}
	to := split + each
	if split < len(rows) && rows[split].Ordinal == ordinal {
		to++
	}
	if to > len(rows) {
		to = len(rows)
	}
	return rows[from:to]
}

func formatHistoryRow(row store.Message, text string) string {
	speaker := "系统"
	switch row.SenderType {
	case "user":
		speaker = "用户"
	case "bot":
		speaker = "Bot"
	}
	return fmt.Sprintf("\n[#%d %s %s]\n%s\n", row.Ordinal, row.CreatedAt.Format(time.DateTime), speaker, text)
}

// snippetAround cuts a window of the content centred on the first match so a
// long message still shows why it matched.
func snippetAround(content, query string, limit int) string {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) <= limit {
		return string(runes)
	}
	lower := strings.ToLower(string(runes))
	start := 0
	if index := strings.Index(lower, strings.ToLower(query)); index >= 0 {
		start = utf8.RuneCountInString(lower[:index]) - limit/3
	}
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(runes) {
		end = len(runes)
		start = end - limit
	}
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

func truncateHistory(content string, limit int) string {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

// escapeLike neutralises LIKE wildcards so a query is matched literally.
func escapeLike(query string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(query)
}
