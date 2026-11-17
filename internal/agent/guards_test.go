package agent

import (
	"encoding/json"
	"testing"

	"superbot/internal/llm"
)

func TestFabricatedIDsFlagsOnlyIDsNothingProduced(t *testing.T) {
	real := "61aa806e-2e9d-4731-bbc4-e0fc7b044a56"
	fake := "b9f2ea4c-3d10-4e6b-9a9e-4b2f0e71c8d5"
	pasted := "37BBA4A5-0487-4185-A089-1F31B392D1FF"
	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, "你是助理"),
		llm.TextMessage(llm.RoleUser, "看看文档 "+pasted),
		llm.AssistantMessage("", "", []llm.ToolCall{{ID: "c1", Name: "create_document", Arguments: json.RawMessage(`{"title":"x"}`)}}),
		llm.ToolResultMessage("c1", "create_document", `{"document_id":"`+real+`"}`, false),
	}
	reply := "已写好（文档 ID " + real + "），另一份见 " + fake + "，你给的 " + pasted + " 我也看了。再提一次 " + fake
	got := fabricatedIDs(reply, messages)
	if len(got) != 1 || got[0] != fake {
		t.Fatalf("fabricated = %v, want only %s", got, fake)
	}
	if ids := fabricatedIDs("没有任何 ID 的回复", messages); ids != nil {
		t.Fatalf("unexpected ids %v", ids)
	}
}

func TestTruncatedToolCall(t *testing.T) {
	call := func(args string) llm.ToolCall {
		return llm.ToolCall{Name: "python", Arguments: json.RawMessage(args)}
	}
	if !truncatedToolCall(call(`{}`), llm.FinishLength) {
		t.Fatal("empty arguments at the output limit must count as truncated")
	}
	if !truncatedToolCall(call(`{"code": "import docx\nd = docx.Doc`), llm.FinishLength) {
		t.Fatal("invalid JSON at the output limit must count as truncated")
	}
	if truncatedToolCall(call(`{"code":"print(1)"}`), llm.FinishLength) {
		t.Fatal("complete arguments must run")
	}
	if truncatedToolCall(call(`{}`), "tool_calls") {
		t.Fatal("a call that did not hit the output limit is never treated as truncated")
	}
}
