package agent

import (
	"testing"

	"superbot/internal/llm"
	"superbot/internal/store"
)

func TestApprovalRequired(t *testing.T) {
	call := llm.ToolCall{Name: "web_search", Arguments: []byte(`{"query":"x"}`)}
	if !approvalRequired(false, nil, call) {
		t.Fatal("auto-review off must require approval for every tool")
	}
	if approvalRequired(true, nil, call) {
		t.Fatal("read-only search should not require approval by default")
	}
	browser := llm.ToolCall{Name: "browser_open", Arguments: []byte(`{"url":"https://example.com"}`)}
	if approvalRequired(true, nil, browser) {
		t.Fatal("browser automation should not require approval by default")
	}
	shell := llm.ToolCall{Name: "shell", Arguments: []byte(`{"command":"ls"}`)}
	if !approvalRequired(true, nil, shell) {
		t.Fatal("shell must require approval by default")
	}
	rules := []store.PermissionRule{{Kind: "allow", Pattern: "shell"}}
	if approvalRequired(true, rules, shell) {
		t.Fatal("allow rule should permit shell")
	}
	rules = []store.PermissionRule{{Kind: "always_ask", Pattern: "ls"}}
	if !approvalRequired(true, rules, shell) {
		t.Fatal("always_ask rule must win over default")
	}
}

func TestAllowRuleCannotBeSatisfiedFromArguments(t *testing.T) {
	rules := []store.PermissionRule{{Kind: "allow", Pattern: "git status"}}
	plain := llm.ToolCall{Name: "shell", Arguments: []byte(`{"command":"git status --short"}`)}
	if approvalRequired(true, rules, plain) {
		t.Fatal("a plain command matching the allowed prefix should run without approval")
	}
	for name, args := range map[string]string{
		"comment smuggling": `{"command":"rm -rf x # git status"}`,
		"chained":           `{"command":"git status && rm -rf x"}`,
		"piped":             `{"command":"git status | sh"}`,
		"substitution":      `{"command":"git status $(rm -rf x)"}`,
		"other field":       `{"command":"rm -rf x","note":"git status"}`,
		"prefix of word":    `{"command":"git statusx"}`,
	} {
		call := llm.ToolCall{Name: "shell", Arguments: []byte(args)}
		if !approvalRequired(true, rules, call) {
			t.Fatalf("%s: allow rule was satisfied by model-controlled arguments", name)
		}
	}
	scoped := []store.PermissionRule{{Kind: "allow", Pattern: "shell ls"}}
	if approvalRequired(true, scoped, llm.ToolCall{Name: "shell", Arguments: []byte(`{"command":"ls -la"}`)}) {
		t.Fatal("tool-scoped command rule should match")
	}
	if !approvalRequired(true, scoped, llm.ToolCall{Name: "python", Arguments: []byte(`{"code":"ls"}`)}) {
		t.Fatal("tool-scoped rule must not leak to another tool")
	}
	family := []store.PermissionRule{{Kind: "allow", Pattern: "workspace_write"}}
	if approvalRequired(true, family, llm.ToolCall{Name: "workspace_write_file", Arguments: []byte(`{"path":"a"}`)}) {
		t.Fatal("tool-name prefix rule should match the tool family")
	}
	if !approvalRequired(true, family, llm.ToolCall{Name: "shell", Arguments: []byte(`{"command":"echo workspace_write"}`)}) {
		t.Fatal("tool-name rule must not match text inside another tool's arguments")
	}
}
