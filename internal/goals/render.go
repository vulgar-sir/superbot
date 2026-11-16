package goals

import (
	"fmt"
	"strings"
)

// Render produces the block injected into a member's prompt. The lead sees
// the whole ledger because it steers the work; other members see the
// objective and only their own tasks, which keeps their prompts small.
func Render(goal *Goal, viewerBotID string, isLead bool) string {
	if goal == nil || goal.Status == StatusStopped {
		return ""
	}
	var b strings.Builder
	b.WriteString("<group_goal>\n")
	fmt.Fprintf(&b, "状态：%s\n目标：%s\n总体验收标准：%s\n", statusLabel(goal.Status), goal.Objective, goal.Acceptance)
	if len(goal.Constraints) > 0 {
		b.WriteString("约束：\n")
		for _, item := range goal.Constraints {
			fmt.Fprintf(&b, "- %s\n", item)
		}
	}
	if isLead {
		renderLedger(&b, goal)
		b.WriteString(leadGuidance(goal))
	} else {
		renderOwnTasks(&b, goal, viewerBotID)
	}
	b.WriteString("</group_goal>")
	return b.String()
}

func renderLedger(b *strings.Builder, goal *Goal) {
	b.WriteString("阶段：\n")
	for index, stage := range goal.Stages {
		marker := " "
		switch {
		case stage.Status == StagePassed:
			marker = "✓"
		case index == goal.Current && goal.Status != StatusDrafting:
			marker = "→"
		}
		fmt.Fprintf(b, "%s %d. %s — 验收：%s\n", marker, index+1, stage.Title, stage.Acceptance)
		if stage.Status == StagePassed {
			fmt.Fprintf(b, "    依据：%s\n", clip(stage.Evidence, 200))
			continue
		}
		if stage.Feedback != "" {
			fmt.Fprintf(b, "    上次返工反馈（第 %d 次）：%s\n", stage.Reworks, clip(stage.Feedback, 300))
		}
		for _, task := range stage.Tasks {
			fmt.Fprintf(b, "    - [%s] %s：%s", taskLabel(task.Status), task.BotName, clip(task.Task, 200))
			if task.Note != "" {
				fmt.Fprintf(b, "（汇报：%s）", clip(task.Note, 200))
			}
			b.WriteString("\n")
		}
	}
}

func renderOwnTasks(b *strings.Builder, goal *Goal, viewerBotID string) {
	if goal.Status != StatusActive || goal.Current >= len(goal.Stages) {
		return
	}
	stage := goal.Stages[goal.Current]
	fmt.Fprintf(b, "当前阶段：%d. %s — 验收：%s\n", goal.Current+1, stage.Title, stage.Acceptance)
	mine := 0
	for _, task := range stage.Tasks {
		if task.BotID != viewerBotID {
			continue
		}
		if mine == 0 {
			b.WriteString("分派给你的任务：\n")
		}
		mine++
		fmt.Fprintf(b, "- [%s] %s\n", taskLabel(task.Status), task.Task)
	}
	if mine == 0 {
		b.WriteString("当前阶段没有分派给你的任务；被 @ 时再行动。\n")
		return
	}
	if stage.Feedback != "" {
		fmt.Fprintf(b, "群主的返工反馈：%s\n", clip(stage.Feedback, 300))
	}
	b.WriteString("做完后调用 group_goal（action=report，status=done，note 写清产物和依据），再在群里简短汇报。\n")
}

func leadGuidance(goal *Goal) string {
	switch goal.Status {
	case StatusDrafting:
		return "你是群主。这是草稿：先和用户对齐目标、验收标准和阶段，得到用户明确同意后再 start。\n"
	case StatusPaused:
		return "目标已暂停。除非用户要求，不要推进；用户同意后用 resume。\n"
	case StatusComplete:
		return "目标已完成。\n"
	}
	if goal.Current >= len(goal.Stages) {
		return "你是群主。所有阶段已通过：对照总体验收标准核对后，用 group_goal（action=complete，带 evidence）完成目标并向用户汇报。\n"
	}
	return "你是群主。推进当前阶段：用 group_goal 的 assign 分派任务并在群里 @ 成员开工；" +
		"成员汇报后亲自核对产物，达标就 pass（必须写 evidence），不达标就 rework（必须写 feedback）。" +
		"需要用户拍板时直接问用户并停下，不要自行假设。\n"
}

// NudgeText is the message that wakes the lead when a round ended with the
// goal still open and nobody working on it.
func NudgeText(goal *Goal) string {
	if goal.Current >= len(goal.Stages) {
		return "[目标检查] 所有阶段都已通过，但目标还没有标记完成。请对照总体验收标准核对；达标就用 group_goal complete 并向用户汇报，否则说明还差什么。"
	}
	stage := goal.Stages[goal.Current]
	open := openTasks(&stage)
	switch {
	case len(stage.Tasks) == 0:
		return fmt.Sprintf("[目标检查] 当前阶段「%s」还没有分派任务。请拆分任务、用 group_goal assign 分派并 @ 成员开工；如果需要用户先做决定，直接问用户。", stage.Title)
	case len(open) > 0:
		return fmt.Sprintf("[目标检查] 当前阶段「%s」还有未完成的任务：%s。请 @ 相关成员跟进；如果已经卡住，向用户说明原因。", stage.Title, strings.Join(open, "、"))
	default:
		return fmt.Sprintf("[目标检查] 当前阶段「%s」的任务都已汇报完成。请核对产物是否达到验收标准：达标用 pass（写 evidence），不达标用 rework（写 feedback）。", stage.Title)
	}
}

// ShouldNudge reports whether the idle driver may wake the lead again. It
// stops after MaxNudges fruitless wake-ups; any goal change or user message
// re-arms it.
func ShouldNudge(goal *Goal) bool {
	return goal != nil && goal.Status == StatusActive && goal.Nudges < MaxNudges
}

func statusLabel(status string) string {
	switch status {
	case StatusDrafting:
		return "草稿（未开始）"
	case StatusActive:
		return "进行中"
	case StatusPaused:
		return "已暂停"
	case StatusComplete:
		return "已完成"
	case StatusStopped:
		return "已停止"
	}
	return status
}

func taskLabel(status string) string {
	switch status {
	case TaskDoing:
		return "进行中"
	case TaskDone:
		return "已完成"
	}
	return "待办"
}
