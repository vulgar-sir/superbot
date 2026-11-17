package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeRequestedRules(t *testing.T) {
	rules, err := NormalizeRequestedRules(json.RawMessage(`{"summary":"x","rules":[
		{"pattern":"  Local_Exec   git  status ","reason":"只读查看仓库状态"},
		{"pattern":"local_exec git status","reason":"重复"},
		{"pattern":"connector_add","reason":"常用"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 || rules[0].Pattern != "local_exec git status" || rules[1].Pattern != "connector_add" {
		t.Fatalf("normalized rules = %+v", rules)
	}
	for name, raw := range map[string]string{
		"empty":          `{"rules":[]}`,
		"no reason":      `{"rules":[{"pattern":"local_exec","reason":" "}]}`,
		"wildcard":       `{"rules":[{"pattern":"*","reason":"all"}]}`,
		"too short":      `{"rules":[{"pattern":"lo","reason":"x"}]}`,
		"multi-line":     `{"rules":[{"pattern":"local_exec ls\nrm -rf /","reason":"x"}]}`,
		"self exemption": `{"rules":[{"pattern":"request_permission_rules","reason":"x"}]}`,
		"self by prefix": `{"rules":[{"pattern":"request_","reason":"x"}]}`,
		"not json":       `rules`,
	} {
		if _, err := NormalizeRequestedRules(json.RawMessage(raw)); err == nil {
			t.Fatalf("%s: request should be rejected", name)
		}
	}
	many := make([]string, 0, maxRequestedRules+1)
	for i := 0; i <= maxRequestedRules; i++ {
		many = append(many, `{"pattern":"tool_`+strings.Repeat("a", i+1)+`","reason":"x"}`)
	}
	if _, err := NormalizeRequestedRules(json.RawMessage(`{"rules":[` + strings.Join(many, ",") + `]}`)); err == nil {
		t.Fatal("more than the maximum number of rules should be rejected")
	}
}
