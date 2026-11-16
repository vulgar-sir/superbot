package agent

import (
	"encoding/json"
	"strings"

	"superbot/internal/llm"
	"superbot/internal/store"
)

// defaultAskTokens name the tool families that need a decision unless a user
// rule allows them. Browser/computer automation runs inside the bot's
// isolated computer and is part of normal tool use, so it is not listed.
var defaultAskTokens = []string{
	"shell", "python", "local_exec", "vault", "credential", "delete", "write",
	"terminal", "connector_add", "connector_remove",
}

// commandTools take a free-form command whose text the model controls.
var commandTools = map[string]bool{"shell": true, "python": true, "local_exec": true, "terminal": true}

var commandArgKeys = []string{"command", "cmd", "code", "script"}

// shellChaining lists the ways one command line can smuggle in a second
// command. An allow rule scoped to a command prefix never matches these.
var shellChaining = []string{";", "&&", "||", "|", "`", "$(", "\n", "\r", ">", "<", "&"}

func approvalRequired(autoReview bool, rules []store.PermissionRule, call llm.ToolCall) bool {
	if !autoReview {
		return true
	}
	lowerName := strings.ToLower(call.Name)
	text := lowerName + " " + strings.ToLower(string(call.Arguments))
	allowMatched := false
	for _, rule := range rules {
		pattern := strings.ToLower(strings.TrimSpace(rule.Pattern))
		if pattern == "" {
			continue
		}
		switch rule.Kind {
		case "always_ask":
			// Asking too often is safe, so this stays a loose substring match.
			if strings.Contains(text, pattern) {
				return true
			}
		case "allow":
			if allowRuleMatches(pattern, lowerName, call.Arguments) {
				allowMatched = true
			}
		}
	}
	if allowMatched {
		return false
	}
	for _, token := range defaultAskTokens {
		if strings.Contains(lowerName, token) {
			return true
		}
	}
	return false
}

// allowRuleMatches is deliberately strict. The model writes the arguments, so
// an allow rule must never be satisfiable by text placed somewhere inside
// them: it matches the tool name, or the start of a single plain command.
func allowRuleMatches(pattern, lowerName string, args json.RawMessage) bool {
	if strings.HasPrefix(lowerName, strings.TrimSuffix(pattern, "*")) && !strings.ContainsAny(pattern, " \t") {
		return true
	}
	tool, prefix, scoped := strings.Cut(pattern, " ")
	if scoped && tool == lowerName {
		return commandHasPrefix(args, strings.TrimSpace(prefix))
	}
	if commandTools[lowerName] {
		return commandHasPrefix(args, pattern)
	}
	return false
}

func commandHasPrefix(args json.RawMessage, prefix string) bool {
	if prefix == "" {
		return false
	}
	var fields map[string]any
	if err := json.Unmarshal(args, &fields); err != nil {
		return false
	}
	for _, key := range commandArgKeys {
		raw, ok := fields[key].(string)
		if !ok {
			continue
		}
		command := strings.ToLower(strings.TrimSpace(raw))
		for _, token := range shellChaining {
			if strings.Contains(command, token) {
				return false
			}
		}
		if command == prefix || strings.HasPrefix(command, prefix+" ") {
			return true
		}
	}
	return false
}
