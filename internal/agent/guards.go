package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"superbot/internal/llm"
	"superbot/internal/store"
)

// Guards against two ways a turn goes wrong without any error being raised:
// the model claims an artifact it never created, and a tool call whose
// arguments were cut off by the output limit gets run as if it were whole.

var uuidPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)

// fabricatedIDs returns the ids a reply cites that appear nowhere in what
// the model was given or got back: not in the prompt, the history, or any
// tool call or tool result. Such an id was made up, typically for a
// document or file the reply says exists but no tool ever created.
func fabricatedIDs(reply string, messages []llm.Message) []string {
	cited := uuidPattern.FindAllString(reply, -1)
	if len(cited) == 0 {
		return nil
	}
	var known strings.Builder
	for _, message := range messages {
		for _, part := range message.Parts {
			known.WriteString(part.Text)
			if part.ToolCall != nil {
				known.Write(part.ToolCall.Arguments)
			}
			if part.ToolResult != nil {
				known.WriteString(part.ToolResult.Content)
			}
			known.WriteByte('\n')
		}
	}
	haystack := strings.ToLower(known.String())
	seen := make(map[string]bool, len(cited))
	var out []string
	for _, id := range cited {
		id = strings.ToLower(id)
		if seen[id] {
			continue
		}
		seen[id] = true
		if !strings.Contains(haystack, id) {
			out = append(out, id)
		}
	}
	return out
}

func fabricatedIDsNotice(ids []string) string {
	return "【系统核查，用户看不到这段】你刚才的回复引用了这些 ID：" + strings.Join(ids, "、") +
		"。本次对话里没有任何工具结果产生过它们，说明它们并不存在。" +
		"如果你说的是要交付的文档或文件，现在就真正调用工具把它创建出来，并使用工具返回的真实 ID；" +
		"如果不需要，就改正回复，删掉这些 ID，不要声称做了没做的事。改好后直接输出完整的最终回复。"
}

// truncatedToolCall reports whether a call from a turn that stopped at the
// output limit lost its arguments: they are missing or not valid JSON.
// Running it would act on half an instruction (an empty script, a file cut
// off midway), so it is reported back to the model instead.
func truncatedToolCall(call llm.ToolCall, finish string) bool {
	if finish != llm.FinishLength {
		return false
	}
	args := strings.TrimSpace(string(call.Arguments))
	return args == "" || args == "{}" || !json.Valid([]byte(args))
}

const truncatedToolCallNotice = "这次工具调用的参数因为输出长度上限被截断，没有执行。" +
	"不要在一次调用的参数里放很长的内容：把长文件分几段写，第一段用 workspace_write，后面的用 workspace_write 加 append=true 追加，" +
	"然后再运行它。"

// stepLimitSummary makes one last model call when a turn used up its tool
// steps, asking for a progress report instead of ending on a placeholder.
// Tool definitions stay in the request (some providers reject a history
// with tool calls but no tools); any tool call in the answer is ignored.
// It returns "" when the call fails or yields no text.
func (e *Engine) stepLimitSummary(
	ctx context.Context,
	provider llm.Provider,
	conv store.Conversation,
	cycle store.Cycle,
	run store.Run,
	bot store.Bot,
	modelRow *store.Model,
	messages []llm.Message,
	toolDefs []llm.ToolDef,
	params botParams,
	maxTokens int,
	outcome *runOutcome,
) string {
	if ctx.Err() != nil {
		return ""
	}
	req := llm.Request{
		Model:       bot.Model,
		Messages:    append(append([]llm.Message(nil), messages...), llm.TextMessage(llm.RoleUser, stepLimitSummaryPrompt)),
		Tools:       toolDefs,
		Temperature: params.Temperature,
		MaxTokens:   maxTokens,
	}
	reserved, err := e.reserve(ctx, conv.ID, cycle.ID, modelRow, req)
	if err != nil {
		return ""
	}
	turn, err := e.streamTurn(ctx, provider, req, conv, cycle, run, bot)
	if err != nil {
		e.release(ctx, conv.ID, cycle.ID, reserved)
		e.logger.Warn("step-limit summary failed", "run_id", run.ID, "error", err)
		return ""
	}
	usage := turn.Usage
	if usage == nil {
		usage = &llm.Usage{PromptTokens: EstimateMessagesTokens(req.Messages), CompletionTokens: EstimateTokens(turn.Text)}
		outcome.Estimated = true
	}
	cost := computeCost(modelRow, usage)
	e.settle(ctx, conv.ID, cycle.ID, reserved, cost)
	outcome.UsageIn += int64(usage.PromptTokens)
	outcome.UsageOut += int64(usage.CompletionTokens)
	outcome.CachedIn += int64(usage.CachedTokens)
	outcome.CostMicros += cost
	return strings.TrimSpace(turn.Text)
}

const stepLimitSummaryPrompt = "【系统】这一轮可用的工具步数已经用完，现在不要再调用任何工具。" +
	"请直接给用户写一段简短的进度说明：已经完成了什么（给出产出文件的路径或文档名）、还差哪些步骤、下一步准备怎么做。" +
	"不要声称完成了还没做完的部分。最后告诉用户回复「继续」就能接着做。"
