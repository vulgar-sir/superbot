package agent

import "superbot/internal/llm"

// EstimateTokens is a conservative, dependency-free token estimate. Exact
// token counts for billing come from provider usage; this is only used for
// context trimming and pre-call budget reservations.
func EstimateTokens(s string) int {
	cjk := 0
	other := 0
	for _, r := range s {
		if r >= 0x2E80 { // CJK and full-width punctuation
			cjk++
		} else {
			other++
		}
	}
	return cjk + (other+3)/4
}

func EstimateMessagesTokens(messages []llm.Message) int {
	total := 0
	for _, msg := range messages {
		total += 4 // per-message overhead
		for _, part := range msg.Parts {
			switch part.Type {
			case llm.PartText, llm.PartReasoning:
				total += EstimateTokens(part.Text)
			case llm.PartToolCall:
				if part.ToolCall != nil {
					total += EstimateTokens(part.ToolCall.Name)
					total += EstimateTokens(string(part.ToolCall.Arguments))
				}
			case llm.PartToolResult:
				if part.ToolResult != nil {
					total += EstimateTokens(part.ToolResult.Content)
				}
			case llm.PartImage:
				// DeepSeek bills up to 384 tokens per image.
				total += 384
			}
		}
	}
	return total
}
