package agent

import (
	"testing"

	"github.com/google/uuid"

	"superbot/internal/store"
)

func TestParseRouteDecision(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		action  string
		botID   string
		wantErr bool
	}{
		{name: "plain", raw: `{"action":"speak","next_bot_id":"abc"}`, action: "speak", botID: "abc"},
		{name: "fenced", raw: "```json\n{\"action\":\"finish\",\"reason\":\"done\"}\n```", action: "finish"},
		{name: "implicit", raw: `{"next_bot_id":"xyz"}`, action: "speak", botID: "xyz"},
		{name: "bad action", raw: `{"action":"dance"}`, wantErr: true},
		{name: "no json", raw: `not json`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := parseRouteDecision(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", decision)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if decision.Action != tc.action {
				t.Fatalf("action = %q, want %q", decision.Action, tc.action)
			}
			if decision.NextBotID != tc.botID {
				t.Fatalf("next_bot_id = %q, want %q", decision.NextBotID, tc.botID)
			}
		})
	}
}

func TestApplyMentionOverride(t *testing.T) {
	architect := store.Bot{ID: uuid.New(), Name: "架构师"}
	reviewer := store.Bot{ID: uuid.New(), Name: "审查者"}
	members := []store.Bot{architect, reviewer}
	decision := routeDecision{Action: "speak", NextBotID: architect.ID.String()}
	recent := []store.Message{{Content: "请 @审查者 检查一下边界条件"}}
	got := applyMentionOverride(decision, recent, members)
	if got.NextBotID != reviewer.ID.String() {
		t.Fatalf("next_bot_id = %q, want reviewer %q", got.NextBotID, reviewer.ID)
	}
}

func TestNextMemberWraps(t *testing.T) {
	first := store.Bot{ID: uuid.New(), Name: "A"}
	second := store.Bot{ID: uuid.New(), Name: "B"}
	members := []store.Bot{first, second}
	if got := nextMember(members, first.ID); got == nil || got.ID != second.ID {
		t.Fatalf("next after first = %+v, want second", got)
	}
	if got := nextMember(members, second.ID); got == nil || got.ID != first.ID {
		t.Fatalf("next after last = %+v, want first", got)
	}
}

func TestParseTeamConfig(t *testing.T) {
	cfg := parseTeamConfig([]byte(`{"max_rounds":4,"coordinator_model":"deepseek-flash","primary_bot_id":"bot-1"}`))
	if cfg.MaxRounds != 4 || cfg.CoordinatorModel != "deepseek-flash" || cfg.PrimaryBotID != "bot-1" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestMentionedBots(t *testing.T) {
	lead := store.Bot{ID: uuid.New(), Name: "一队-主管"}
	strategy := store.Bot{ID: uuid.New(), Name: "一队-策略"}
	reviewer := store.Bot{ID: uuid.New(), Name: "一队-审查"}
	members := []store.Bot{lead, strategy, reviewer}

	got := mentionedBots("请 @一队-策略 和 @一队-审查 一起看下", members, uuid.Nil)
	if len(got) != 2 || got[0].ID != strategy.ID || got[1].ID != reviewer.ID {
		t.Fatalf("mentionedBots() = %+v, want strategy then reviewer", got)
	}
	if got := mentionedBots("请 @一队-策略 自己检查", members, strategy.ID); len(got) != 0 {
		t.Fatalf("mentionedBots() should exclude the speaker, got %+v", got)
	}
	if got := mentionedBots("没有点名", members, uuid.Nil); len(got) != 0 {
		t.Fatalf("mentionedBots() = %+v, want none", got)
	}
}

func TestHasMentionBoundaries(t *testing.T) {
	cases := []struct {
		name    string
		content string
		bot     string
		want    bool
	}{
		{name: "plain", content: "请 @Alice 看一下", bot: "Alice", want: true},
		{name: "prefix must not match", content: "请 @Alice2 看一下", bot: "Alice", want: false},
		{name: "latin name then CJK", content: "@Alice检查一下", bot: "Alice", want: true},
		{name: "email must not match", content: "mail me at a@Alice.com", bot: "Alice", want: false},
		{name: "cjk name", content: "请 @审查者 检查", bot: "审查者", want: true},
		{name: "no at", content: "请 Alice 看一下", bot: "Alice", want: false},
		{name: "punctuation", content: "(@Alice)", bot: "Alice", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasMention(tc.content, tc.bot); got != tc.want {
				t.Fatalf("hasMention(%q, %q) = %v, want %v", tc.content, tc.bot, got, tc.want)
			}
		})
	}
}

func TestMentionedBotsDoesNotMatchPrefix(t *testing.T) {
	alice := store.Bot{ID: uuid.New(), Name: "Alice"}
	alice2 := store.Bot{ID: uuid.New(), Name: "Alice2"}
	members := []store.Bot{alice, alice2}
	got := mentionedBots("@Alice2 请回答", members, uuid.Nil)
	if len(got) != 1 || got[0].ID != alice2.ID {
		t.Fatalf("mentionedBots() = %+v, want only Alice2", got)
	}
}
