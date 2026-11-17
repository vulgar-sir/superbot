package agent

import (
	"testing"

	"superbot/internal/llm"
	"superbot/internal/store"
)

func TestApprovalRequired(t *testing.T) {
	call := llm.ToolCall{Name: "web_search", Arguments: []byte(`{"query":"x"}`)}
	if need, delegable := approvalNeed(false, nil, call); !need || delegable {
		t.Fatal("auto-review off must require the user's approval for every tool")
	}
	if approvalRequired(true, nil, call) {
		t.Fatal("read-only search should not require approval by default")
	}
	browser := llm.ToolCall{Name: "browser_open", Arguments: []byte(`{"url":"https://example.com"}`)}
	if approvalRequired(true, nil, browser) {
		t.Fatal("browser automation should not require approval by default")
	}
	// Work inside the bot's own isolated computer is ordinary tool use.
	for _, name := range []string{"shell", "python", "workspace_write", "drive_write", "team_write"} {
		inside := llm.ToolCall{Name: name, Arguments: []byte(`{"command":"rm -rf build","code":"print(1)","path":"a"}`)}
		if approvalRequired(true, nil, inside) {
			t.Fatalf("%s runs in the bot's own computer and must not ask by default", name)
		}
	}
	// What reaches outside it does.
	for _, name := range []string{"local_exec", "vault_fill_credentials", "connector_add", "connector_remove"} {
		risky := llm.ToolCall{Name: name, Arguments: []byte(`{"command":"ls"}`)}
		if need, delegable := approvalNeed(true, nil, risky); !need || !delegable {
			t.Fatalf("%s must ask by default, and the primary bot may decide it", name)
		}
	}
	local := llm.ToolCall{Name: "local_exec", Arguments: []byte(`{"command":"ls"}`)}
	rules := []store.PermissionRule{{Kind: "allow", Pattern: "local_exec"}}
	if approvalRequired(true, rules, local) {
		t.Fatal("allow rule should permit local_exec")
	}
	shell := llm.ToolCall{Name: "shell", Arguments: []byte(`{"command":"ls"}`)}
	rules = []store.PermissionRule{{Kind: "always_ask", Pattern: "ls"}}
	if need, delegable := approvalNeed(true, rules, shell); !need || delegable {
		t.Fatal("an always_ask rule must win over the default and stay with the user")
	}
}

func TestAllowRuleCannotBeSatisfiedFromArguments(t *testing.T) {
	rules := []store.PermissionRule{{Kind: "allow", Pattern: "git status"}}
	plain := llm.ToolCall{Name: "local_exec", Arguments: []byte(`{"command":"git status --short"}`)}
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
		call := llm.ToolCall{Name: "local_exec", Arguments: []byte(args)}
		if !approvalRequired(true, rules, call) {
			t.Fatalf("%s: allow rule was satisfied by model-controlled arguments", name)
		}
	}
	scoped := []store.PermissionRule{{Kind: "allow", Pattern: "local_exec ls"}}
	if approvalRequired(true, scoped, llm.ToolCall{Name: "local_exec", Arguments: []byte(`{"command":"ls -la"}`)}) {
		t.Fatal("tool-scoped command rule should match")
	}
	other := []store.PermissionRule{{Kind: "allow", Pattern: "shell ls"}}
	if !approvalRequired(true, other, llm.ToolCall{Name: "local_exec", Arguments: []byte(`{"command":"ls"}`)}) {
		t.Fatal("tool-scoped rule must not leak to another tool")
	}
	family := []store.PermissionRule{{Kind: "allow", Pattern: "connector_"}}
	if approvalRequired(true, family, llm.ToolCall{Name: "connector_add", Arguments: []byte(`{"name":"a"}`)}) {
		t.Fatal("tool-name prefix rule should match the tool family")
	}
	if !approvalRequired(true, family, llm.ToolCall{Name: "local_exec", Arguments: []byte(`{"command":"echo connector_"}`)}) {
		t.Fatal("tool-name rule must not match text inside another tool's arguments")
	}
}

func TestParseApprovalVerdict(t *testing.T) {
	cases := map[string]string{
		`{"decision":"allow","reason":"在做用户要的报表"}`:                     "allow",
		"好的。\n```json\n{\"decision\":\"DENY\",\"reason\":\"删库\"}\n```": "deny",
		`{"decision":"ask_user","reason":"拿不准"}`:                       "ask_user",
		`我觉得可以`:        "",
		`{"decision":`: "",
	}
	for text, want := range cases {
		if got := parseApprovalVerdict(text).Decision; got != want {
			t.Fatalf("%q: decision %q, want %q", text, got, want)
		}
	}
	if reason := parseApprovalVerdict(`{"decision":"allow"}`).Reason; reason == "" {
		t.Fatal("a verdict without a reason still needs one to show the user")
	}
}

func TestPermissionRuleRequestsAlwaysGoToTheUser(t *testing.T) {
	call := llm.ToolCall{Name: "request_permission_rules", Arguments: []byte(`{"rules":[{"pattern":"local_exec","reason":"x"}]}`)}
	everything := []store.PermissionRule{
		{Kind: "allow", Pattern: "request_permission_rules"},
		{Kind: "allow", Pattern: "request"},
	}
	for _, autoReview := range []bool{true, false} {
		if need, delegable := approvalNeed(autoReview, everything, call); !need || delegable {
			t.Fatalf("autoReview=%v: a request for standing exemptions must always wait for the user", autoReview)
		}
	}
}
