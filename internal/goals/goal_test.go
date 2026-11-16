package goals

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var (
	now     = time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	lead    = Actor{BotID: "lead", Name: "Chief", IsLead: true}
	writer  = Actor{BotID: "w", Name: "Writer"}
	coder   = Actor{BotID: "c", Name: "Coder"}
	user    = Actor{IsUser: true}
	members = []Member{{ID: "lead", Name: "Chief"}, {ID: "w", Name: "Writer"}, {ID: "c", Name: "Coder"}}
)

func mustApply(t *testing.T, goal *Goal, action Action, actor Actor) *Goal {
	t.Helper()
	next, _, err := Apply(goal, action, actor, members, now)
	if err != nil {
		t.Fatalf("%s: %v", action.Action, err)
	}
	return next
}

func mustFail(t *testing.T, goal *Goal, action Action, actor Actor, want string) {
	t.Helper()
	_, _, err := Apply(goal, action, actor, members, now)
	if err == nil {
		t.Fatalf("%s should have been rejected", action.Action)
	}
	if want != "" && !strings.Contains(err.Error(), want) {
		t.Fatalf("%s: error %q does not mention %q", action.Action, err, want)
	}
}

func draft() Action {
	return Action{
		Action: "draft", Objective: "上线新版落地页", Acceptance: "页面可访问且通过验收清单",
		Constraints: []string{"不改后端接口", "  "},
		Stages: []StageInput{
			{Title: "文案", Acceptance: "三屏文案定稿"},
			{Title: "实现", Acceptance: "构建通过并部署到预览"},
		},
	}
}

func TestFullLifecycle(t *testing.T) {
	goal := mustApply(t, nil, draft(), lead)
	if goal.Status != StatusDrafting || len(goal.Stages) != 2 || len(goal.Constraints) != 1 {
		t.Fatalf("draft wrong: %+v", goal)
	}
	mustFail(t, goal, Action{Action: "assign", Bot: "Writer", Task: "x"}, lead, "不在进行中")

	goal = mustApply(t, goal, Action{Action: "start"}, lead)
	if goal.Status != StatusActive || goal.Stages[0].Status != StageActive {
		t.Fatalf("start wrong: %+v", goal)
	}
	goal = mustApply(t, goal, Action{Action: "assign", Bot: "@writer", Task: "写三屏文案"}, lead)
	if goal.Stages[0].Tasks[0].BotID != "w" {
		t.Fatal("member lookup should be case-insensitive and accept a leading @")
	}
	mustFail(t, goal, Action{Action: "assign", Bot: "Nobody", Task: "x"}, lead, "Writer")

	mustFail(t, goal, Action{Action: "pass", Evidence: "看过了"}, lead, "未完成的任务")
	mustFail(t, goal, Action{Action: "report"}, coder, "没有分派给你")
	goal = mustApply(t, goal, Action{Action: "report", Status: "done", Note: "文案在 shared/copy.md"}, writer)
	mustFail(t, goal, Action{Action: "pass"}, lead, "evidence")

	goal = mustApply(t, goal, Action{Action: "pass", Evidence: "读了 shared/copy.md，三屏齐全"}, lead)
	if goal.Current != 1 || goal.Stages[0].Status != StagePassed || goal.Stages[1].Status != StageActive {
		t.Fatalf("pass did not advance: %+v", goal)
	}
	mustFail(t, goal, Action{Action: "complete", Evidence: "x"}, lead, "还没有通过验收")

	goal = mustApply(t, goal, Action{Action: "assign", Bot: "Coder", Task: "实现页面"}, lead)
	goal = mustApply(t, goal, Action{Action: "report", Note: "已部署"}, coder)
	goal = mustApply(t, goal, Action{Action: "rework", Feedback: "移动端排版错位"}, lead)
	if goal.Stages[1].Reworks != 1 || goal.Stages[1].Tasks[0].Status != TaskTodo {
		t.Fatalf("rework should reopen finished tasks: %+v", goal.Stages[1])
	}
	mustFail(t, goal, Action{Action: "rework"}, lead, "feedback")
	goal = mustApply(t, goal, Action{Action: "report", Note: "已修复"}, coder)
	goal = mustApply(t, goal, Action{Action: "pass", Evidence: "预览地址移动端正常"}, lead)
	if goal.Current != 2 {
		t.Fatalf("current = %d", goal.Current)
	}
	mustFail(t, goal, Action{Action: "pass", Evidence: "x"}, lead, "complete")
	mustFail(t, goal, Action{Action: "complete"}, lead, "evidence")
	goal = mustApply(t, goal, Action{Action: "complete", Evidence: "清单全部通过"}, lead)
	if goal.Status != StatusComplete {
		t.Fatalf("status = %s", goal.Status)
	}
	mustFail(t, goal, Action{Action: "revise", Objective: "改一下"}, lead, "已结束")

	// A finished goal can be replaced by a new draft.
	goal = mustApply(t, goal, draft(), user)
	if goal.Status != StatusDrafting || len(goal.Events) != 1 {
		t.Fatalf("new draft should start clean: %+v", goal)
	}
}

func TestMembersCannotManage(t *testing.T) {
	goal := mustApply(t, mustApply(t, nil, draft(), lead), Action{Action: "start"}, lead)
	for _, action := range []string{"start", "revise", "assign", "pass", "rework", "complete", "pause", "resume", "stop"} {
		_, _, err := Apply(goal, Action{Action: action, Evidence: "x", Feedback: "x", Bot: "Coder", Task: "x"}, writer, members, now)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s by a member: got %v, want ErrForbidden", action, err)
		}
	}
	if _, _, err := Apply(nil, draft(), writer, members, now); !errors.Is(err, ErrForbidden) {
		t.Fatalf("draft by a member: %v", err)
	}
	// The user may do what the lead may do.
	mustApply(t, goal, Action{Action: "pause"}, user)
}

func TestRejectedActionLeavesGoalUntouched(t *testing.T) {
	goal := mustApply(t, mustApply(t, nil, draft(), lead), Action{Action: "start"}, lead)
	goal = mustApply(t, goal, Action{Action: "assign", Bot: "Writer", Task: "写文案"}, lead)
	before := len(goal.Events)
	mustFail(t, goal, Action{Action: "pass", Evidence: "x"}, lead, "")
	if len(goal.Events) != before || goal.Stages[0].Status != StageActive || goal.Stages[0].Evidence != "" {
		t.Fatal("a rejected action mutated the goal")
	}
	next := mustApply(t, goal, Action{Action: "report"}, writer)
	if goal.Stages[0].Tasks[0].Status != TaskTodo || next.Stages[0].Tasks[0].Status != TaskDone {
		t.Fatal("Apply must copy, not mutate its input")
	}
}

func TestReviseKeepsPassedStages(t *testing.T) {
	goal := mustApply(t, mustApply(t, nil, draft(), lead), Action{Action: "start"}, lead)
	goal = mustApply(t, goal, Action{Action: "pass", Evidence: "定稿"}, lead)
	goal = mustApply(t, goal, Action{Action: "revise", Stages: []StageInput{
		{Title: "实现", Acceptance: "构建通过"}, {Title: "验收", Acceptance: "用户确认"},
	}}, lead)
	if len(goal.Stages) != 3 || goal.Stages[0].Status != StagePassed || goal.Stages[0].Evidence != "定稿" {
		t.Fatalf("passed stage was rewritten: %+v", goal.Stages)
	}
	if goal.Current != 1 || goal.Stages[1].Status != StageActive {
		t.Fatalf("revised stage should be active: %+v", goal)
	}
}

func TestLimits(t *testing.T) {
	tooMany := draft()
	for i := 0; i < MaxStages; i++ {
		tooMany.Stages = append(tooMany.Stages, StageInput{Title: "s", Acceptance: "a"})
	}
	mustFail(t, nil, tooMany, lead, "不能超过")
	missing := draft()
	missing.Stages[1].Acceptance = ""
	mustFail(t, nil, missing, lead, "第 2 个阶段")
	noStages := draft()
	noStages.Stages = nil
	mustFail(t, nil, noStages, lead, "至少需要一个阶段")

	goal := mustApply(t, mustApply(t, nil, draft(), lead), Action{Action: "start"}, lead)
	for i := 0; i < MaxReworks; i++ {
		goal = mustApply(t, goal, Action{Action: "rework", Feedback: "不行"}, lead)
	}
	mustFail(t, goal, Action{Action: "rework", Feedback: "还是不行"}, lead, "pause")

	mustFail(t, goal, draft(), lead, "已有进行中的目标")
	mustFail(t, goal, Action{Action: "dance"}, lead, "未知的 action")
	if _, message, err := Apply(nil, Action{Action: "status"}, writer, members, now); err != nil || message == "" {
		t.Fatalf("status without a goal should explain, got %q %v", message, err)
	}
}

func TestNudgeBudgetAndReset(t *testing.T) {
	goal := mustApply(t, mustApply(t, nil, draft(), lead), Action{Action: "start"}, lead)
	if !ShouldNudge(goal) {
		t.Fatal("an active goal with no work should be nudged")
	}
	goal.Nudges = MaxNudges
	if ShouldNudge(goal) {
		t.Fatal("nudging must stop after the budget is used")
	}
	goal = mustApply(t, goal, Action{Action: "assign", Bot: "Coder", Task: "干活"}, lead)
	if goal.Nudges != 0 || !ShouldNudge(goal) {
		t.Fatal("progress must re-arm the driver")
	}
	paused := mustApply(t, goal, Action{Action: "pause"}, user)
	if ShouldNudge(paused) || ShouldNudge(nil) {
		t.Fatal("paused or missing goals are never nudged")
	}
}

func TestRenderScopesByRole(t *testing.T) {
	goal := mustApply(t, mustApply(t, nil, draft(), lead), Action{Action: "start"}, lead)
	goal = mustApply(t, goal, Action{Action: "assign", Bot: "Writer", Task: "写三屏文案"}, lead)
	goal = mustApply(t, goal, Action{Action: "assign", Bot: "Coder", Task: "搭页面骨架"}, lead)

	forLead := Render(goal, "lead", true)
	if !strings.Contains(forLead, "写三屏文案") || !strings.Contains(forLead, "搭页面骨架") || !strings.Contains(forLead, "你是群主") {
		t.Fatalf("lead must see the whole ledger:\n%s", forLead)
	}
	forWriter := Render(goal, "w", false)
	if !strings.Contains(forWriter, "写三屏文案") || strings.Contains(forWriter, "搭页面骨架") {
		t.Fatalf("a member must see only its own tasks:\n%s", forWriter)
	}
	if !strings.Contains(forWriter, "上线新版落地页") {
		t.Fatal("members still need the objective")
	}
	if idle := Render(goal, "lead", false); !strings.Contains(idle, "没有分派给你的任务") {
		t.Fatalf("member without tasks should be told to wait:\n%s", idle)
	}
	stopped := mustApply(t, goal, Action{Action: "stop"}, user)
	if Render(stopped, "lead", true) != "" || Render(nil, "lead", true) != "" {
		t.Fatal("stopped or missing goals render nothing")
	}
}

func TestNudgeTextFollowsStageState(t *testing.T) {
	goal := mustApply(t, mustApply(t, nil, draft(), lead), Action{Action: "start"}, lead)
	if text := NudgeText(goal); !strings.Contains(text, "还没有分派任务") {
		t.Fatalf("unexpected nudge: %s", text)
	}
	goal = mustApply(t, goal, Action{Action: "assign", Bot: "Writer", Task: "写文案"}, lead)
	if text := NudgeText(goal); !strings.Contains(text, "Writer") {
		t.Fatalf("nudge should name who is still working: %s", text)
	}
	goal = mustApply(t, goal, Action{Action: "report"}, writer)
	if text := NudgeText(goal); !strings.Contains(text, "核对产物") {
		t.Fatalf("nudge should ask for acceptance: %s", text)
	}
	goal = mustApply(t, goal, Action{Action: "pass", Evidence: "ok"}, lead)
	goal = mustApply(t, goal, Action{Action: "pass", Evidence: "ok"}, lead)
	if text := NudgeText(goal); !strings.Contains(text, "complete") {
		t.Fatalf("nudge should ask to complete: %s", text)
	}
}
