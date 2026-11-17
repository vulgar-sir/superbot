package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/store"
)

// PermissionRulesToolName is the primary bot's way to ask for standing
// exemptions. Every call needs the user's own approval: no rule, setting or
// other bot can approve it.
const PermissionRulesToolName = "request_permission_rules"

const maxRequestedRules = 20

// A rule is a tool name, a tool-name prefix ending in "_", or a tool name
// followed by a command prefix ("local_exec git status").
var rulePatternShape = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}( [^\n]{1,120})?$`)

type permissionRulesTool struct {
	db      *store.DB
	ownerID uuid.UUID
}

// NewPermissionRulesTool lets the account's primary bot add a batch of
// allow rules once the user approves the request.
func NewPermissionRulesTool(db *store.DB, ownerID uuid.UUID) Tool {
	return &permissionRulesTool{db: db, ownerID: ownerID}
}

func (t *permissionRulesTool) Name() string { return PermissionRulesToolName }

func (t *permissionRulesTool) Description() string {
	return "一次申请一批免审批规则（仅主 Bot 可用）。每次调用都会停下来等用户本人批准，批准后规则对账号下所有 Bot 生效，用户可随时在「设置 → 权限」删除。" +
		"pattern 写工具名（如 local_exec）放行这个工具，或写「工具名 命令前缀」只放行某一类命令（如 local_exec git status，带 ; | && 等拼接的命令不会被放行）。" +
		"只申请你反复被审批打断、且风险可控的操作，每条写清理由；不要申请删除数据、读写密码这类操作的整体放行。"
}

func (t *permissionRulesTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"summary": {"type": "string", "description": "一句话说明为什么要这批规则"},
			"rules": {
				"type": "array",
				"maxItems": 20,
				"items": {
					"type": "object",
					"properties": {
						"pattern": {"type": "string", "description": "工具名，或「工具名 命令前缀」"},
						"reason": {"type": "string", "description": "为什么这类操作可以免审批"}
					},
					"required": ["pattern", "reason"]
				}
			}
		},
		"required": ["summary", "rules"]
	}`)
}

type requestedRule struct {
	Pattern string `json:"pattern"`
	Reason  string `json:"reason"`
}

// NormalizeRequestedRules checks a request before it is shown to the user
// and again before it is applied.
func NormalizeRequestedRules(raw json.RawMessage) ([]requestedRule, error) {
	var input struct {
		Rules []requestedRule `json:"rules"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, fmt.Errorf("参数格式不正确: %w", err)
	}
	if len(input.Rules) == 0 {
		return nil, fmt.Errorf("至少要申请一条规则")
	}
	if len(input.Rules) > maxRequestedRules {
		return nil, fmt.Errorf("一次最多申请 %d 条规则", maxRequestedRules)
	}
	seen := make(map[string]bool, len(input.Rules))
	out := make([]requestedRule, 0, len(input.Rules))
	for _, rule := range input.Rules {
		if strings.ContainsAny(rule.Pattern, "\n\r\t;&|`$<>") {
			// An allow rule never matches a chained command anyway; a
			// pattern carrying one is a mistake or an attempt to hide one.
			return nil, fmt.Errorf("规则 %q 含有换行或 ; & | ` $ < > 等拼接符号", rule.Pattern)
		}
		pattern := strings.Join(strings.Fields(strings.ToLower(rule.Pattern)), " ")
		reason := strings.TrimSpace(rule.Reason)
		if !rulePatternShape.MatchString(pattern) {
			return nil, fmt.Errorf("规则 %q 不是「工具名」或「工具名 命令前缀」的形式", rule.Pattern)
		}
		tool, _, _ := strings.Cut(pattern, " ")
		if strings.HasPrefix(PermissionRulesToolName, strings.TrimSuffix(tool, "_")) {
			return nil, fmt.Errorf("不能申请放行 %s 本身", PermissionRulesToolName)
		}
		if reason == "" {
			return nil, fmt.Errorf("规则 %q 缺少理由", pattern)
		}
		if seen[pattern] {
			continue
		}
		seen[pattern] = true
		out = append(out, requestedRule{Pattern: pattern, Reason: reason})
	}
	return out, nil
}

func (t *permissionRulesTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	rules, err := NormalizeRequestedRules(raw)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	existing, err := t.db.ListPermissionRules(ctx, t.ownerID)
	if err != nil {
		return Result{Content: "读取现有规则失败: " + err.Error(), IsError: true}, nil
	}
	have := make(map[string]bool, len(existing))
	for _, rule := range existing {
		if rule.Kind == "allow" {
			have[strings.ToLower(strings.TrimSpace(rule.Pattern))] = true
		}
	}
	var added, skipped []string
	for _, rule := range rules {
		if have[rule.Pattern] {
			skipped = append(skipped, rule.Pattern)
			continue
		}
		if _, err := t.db.CreatePermissionRule(ctx, store.CreatePermissionRuleParams{
			OwnerID: t.ownerID, Kind: "allow", Pattern: rule.Pattern,
		}); err != nil {
			return Result{Content: fmt.Sprintf("添加规则 %q 失败: %v（已添加：%s）", rule.Pattern, err, strings.Join(added, "、")), IsError: true}, nil
		}
		have[rule.Pattern] = true
		added = append(added, rule.Pattern)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "用户已批准，新增 %d 条免审批规则", len(added))
	if len(added) > 0 {
		b.WriteString("：" + strings.Join(added, "、"))
	}
	if len(skipped) > 0 {
		b.WriteString("。已存在、未重复添加：" + strings.Join(skipped, "、"))
	}
	b.WriteString("。这些规则对账号下所有 Bot 生效，用户可在「设置 → 权限」删除。")
	return Result{Content: b.String()}, nil
}
