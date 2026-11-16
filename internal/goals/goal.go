// Package goals implements the group goal: a team works towards one stated
// objective in stages, the lead assigns tasks, and a stage only advances when
// the lead records evidence that its acceptance criteria were met.
//
// Everything here is pure state transformation so the rules can be tested
// without a database or a model.
package goals

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	StatusDrafting = "drafting"
	StatusActive   = "active"
	StatusPaused   = "paused"
	StatusComplete = "complete"
	StatusStopped  = "stopped"

	StagePending = "pending"
	StageActive  = "active"
	StagePassed  = "passed"

	TaskTodo  = "todo"
	TaskDoing = "doing"
	TaskDone  = "done"

	MaxStages        = 8
	MaxTasksPerStage = 12
	MaxConstraints   = 12
	MaxReworks       = 5
	MaxNudges        = 2
	maxEvents        = 60
	maxText          = 2000
	maxShortText     = 300
)

type Goal struct {
	Status      string   `json:"status"`
	Objective   string   `json:"objective"`
	Acceptance  string   `json:"acceptance"`
	Constraints []string `json:"constraints"`
	Stages      []Stage  `json:"stages"`
	// Current is the index of the stage being worked on; it equals
	// len(Stages) once every stage has passed and only `complete` is owed.
	Current  int     `json:"current"`
	Evidence string  `json:"evidence"`
	Nudges   int     `json:"nudges"`
	Events   []Event `json:"events"`
}

type Stage struct {
	Title      string `json:"title"`
	Acceptance string `json:"acceptance"`
	Status     string `json:"status"`
	Tasks      []Task `json:"tasks"`
	Reworks    int    `json:"reworks"`
	Evidence   string `json:"evidence"`
	Feedback   string `json:"feedback"`
}

type Task struct {
	BotID   string `json:"bot_id"`
	BotName string `json:"bot_name"`
	Task    string `json:"task"`
	Status  string `json:"status"`
	Note    string `json:"note"`
}

type Event struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Action string    `json:"action"`
	Detail string    `json:"detail"`
}

// Actor is whoever is asking for a change. The user and the team lead may do
// everything; other members may only report on their own tasks.
type Actor struct {
	BotID  string
	Name   string
	IsLead bool
	IsUser bool
}

func (a Actor) canManage() bool { return a.IsUser || a.IsLead }

type Member struct {
	ID   string
	Name string
}

type StageInput struct {
	Title      string `json:"title"`
	Acceptance string `json:"acceptance"`
}

// Action is one requested change, as sent by the group_goal tool or the UI.
type Action struct {
	Action      string       `json:"action"`
	Objective   string       `json:"objective"`
	Acceptance  string       `json:"acceptance"`
	Constraints []string     `json:"constraints"`
	Stages      []StageInput `json:"stages"`
	// Stage is a 1-based stage number; 0 means the current stage.
	Stage    int    `json:"stage"`
	Bot      string `json:"bot"`
	Task     string `json:"task"`
	Status   string `json:"status"`
	Note     string `json:"note"`
	Evidence string `json:"evidence"`
	Feedback string `json:"feedback"`
}

var ErrForbidden = errors.New("只有群主 Bot 或用户可以管理群目标；成员只能用 report 汇报自己的任务")

// Apply validates an action against the goal and returns the updated copy.
// The input goal is never modified, so a rejected action leaves no trace.
func Apply(current *Goal, action Action, actor Actor, members []Member, now time.Time) (*Goal, string, error) {
	name := strings.ToLower(strings.TrimSpace(action.Action))
	if name == "status" {
		if current == nil {
			return nil, "当前群聊还没有设定目标。", nil
		}
		return current, "", nil
	}
	if name == "draft" {
		return applyDraft(current, action, actor, now)
	}
	if current == nil {
		return nil, "", errors.New("当前群聊还没有目标；先用 draft 起草")
	}
	goal := clone(current)
	if name == "report" {
		return applyReport(goal, action, actor, now)
	}
	if !actor.canManage() {
		return nil, "", ErrForbidden
	}

	switch name {
	case "start":
		if goal.Status != StatusDrafting {
			return nil, "", fmt.Errorf("只有草稿状态的目标可以开始，当前是 %s", goal.Status)
		}
		goal.Status = StatusActive
		goal.Current = 0
		goal.Stages[0].Status = StageActive
		return finish(goal, actor, name, goal.Stages[0].Title, now, "目标已开始，当前阶段："+goal.Stages[0].Title)

	case "revise":
		if goal.Status == StatusComplete || goal.Status == StatusStopped {
			return nil, "", errors.New("已结束的目标不能修改；用 draft 起草新目标")
		}
		if err := reviseGoal(goal, action); err != nil {
			return nil, "", err
		}
		return finish(goal, actor, name, "", now, "目标已更新。")

	case "assign":
		if err := requireRunning(goal); err != nil {
			return nil, "", err
		}
		stage, index, err := pickStage(goal, action.Stage)
		if err != nil {
			return nil, "", err
		}
		if stage.Status == StagePassed {
			return nil, "", errors.New("该阶段已通过验收，不能再分派任务")
		}
		member, err := findMember(members, action.Bot)
		if err != nil {
			return nil, "", err
		}
		text := clip(action.Task, maxText)
		if text == "" {
			return nil, "", errors.New("task 不能为空：写清楚要做什么、交付什么")
		}
		if len(stage.Tasks) >= MaxTasksPerStage {
			return nil, "", fmt.Errorf("每个阶段最多 %d 个任务", MaxTasksPerStage)
		}
		stage.Tasks = append(stage.Tasks, Task{BotID: member.ID, BotName: member.Name, Task: text, Status: TaskTodo})
		return finish(goal, actor, name, member.Name+": "+clip(text, 80), now,
			fmt.Sprintf("已在阶段 %d 给 %s 分派任务。记得在群里 @%s 让它开工。", index+1, member.Name, member.Name))

	case "pass":
		if err := requireRunning(goal); err != nil {
			return nil, "", err
		}
		if goal.Current >= len(goal.Stages) {
			return nil, "", errors.New("所有阶段都已通过；用 complete 完成目标")
		}
		stage := &goal.Stages[goal.Current]
		evidence := clip(action.Evidence, maxText)
		if evidence == "" {
			return nil, "", errors.New("pass 必须提供 evidence：说明依据什么判断该阶段达到了验收标准（产物路径、测试结果、成员汇报等）")
		}
		if open := openTasks(stage); len(open) > 0 {
			return nil, "", fmt.Errorf("阶段还有未完成的任务：%s。让成员完成并 report，或用 rework 退回", strings.Join(open, "、"))
		}
		stage.Status = StagePassed
		stage.Evidence = evidence
		goal.Current++
		message := "阶段「" + stage.Title + "」已通过。"
		if goal.Current < len(goal.Stages) {
			goal.Stages[goal.Current].Status = StageActive
			message += "进入下一阶段：" + goal.Stages[goal.Current].Title
		} else {
			message += "所有阶段已通过；核对总体验收标准后用 complete 完成目标。"
		}
		return finish(goal, actor, name, stage.Title, now, message)

	case "rework":
		if err := requireRunning(goal); err != nil {
			return nil, "", err
		}
		if goal.Current >= len(goal.Stages) {
			return nil, "", errors.New("没有进行中的阶段可以退回")
		}
		stage := &goal.Stages[goal.Current]
		feedback := clip(action.Feedback, maxText)
		if feedback == "" {
			return nil, "", errors.New("rework 必须提供 feedback：具体哪里没达到验收标准")
		}
		if stage.Reworks >= MaxReworks {
			return nil, "", fmt.Errorf("该阶段已退回 %d 次；请 pause 目标并向用户说明卡在哪里", MaxReworks)
		}
		stage.Reworks++
		stage.Feedback = feedback
		for i := range stage.Tasks {
			if stage.Tasks[i].Status == TaskDone {
				stage.Tasks[i].Status = TaskTodo
			}
		}
		return finish(goal, actor, name, stage.Title, now, "阶段「"+stage.Title+"」已退回返工。把反馈转达给相关成员。")

	case "complete":
		if goal.Status != StatusActive {
			return nil, "", fmt.Errorf("只有进行中的目标可以完成，当前是 %s", goal.Status)
		}
		if goal.Current < len(goal.Stages) {
			return nil, "", fmt.Errorf("阶段「%s」还没有通过验收", goal.Stages[goal.Current].Title)
		}
		evidence := clip(action.Evidence, maxText)
		if evidence == "" {
			return nil, "", errors.New("complete 必须提供 evidence：对照总体验收标准说明为什么算完成")
		}
		goal.Status = StatusComplete
		goal.Evidence = evidence
		return finish(goal, actor, name, "", now, "目标已完成。向用户汇报结果和依据。")

	case "pause":
		if goal.Status != StatusActive {
			return nil, "", fmt.Errorf("只有进行中的目标可以暂停，当前是 %s", goal.Status)
		}
		goal.Status = StatusPaused
		return finish(goal, actor, name, clip(action.Note, 200), now, "目标已暂停。")

	case "resume":
		if goal.Status != StatusPaused {
			return nil, "", fmt.Errorf("只有暂停的目标可以恢复，当前是 %s", goal.Status)
		}
		goal.Status = StatusActive
		return finish(goal, actor, name, "", now, "目标已恢复。")

	case "stop":
		if goal.Status == StatusComplete || goal.Status == StatusStopped {
			return nil, "", fmt.Errorf("目标已经是 %s", goal.Status)
		}
		goal.Status = StatusStopped
		return finish(goal, actor, name, clip(action.Note, 200), now, "目标已停止。")
	}
	return nil, "", fmt.Errorf("未知的 action %q；可用：draft, start, revise, assign, report, pass, rework, complete, pause, resume, stop, status", action.Action)
}

func applyDraft(current *Goal, action Action, actor Actor, now time.Time) (*Goal, string, error) {
	if !actor.canManage() {
		return nil, "", ErrForbidden
	}
	if current != nil && (current.Status == StatusActive || current.Status == StatusPaused) {
		return nil, "", errors.New("已有进行中的目标；用 revise 修改它，或先 stop 再起草新的")
	}
	goal := &Goal{Status: StatusDrafting}
	goal.Objective = clip(action.Objective, maxText)
	goal.Acceptance = clip(action.Acceptance, maxText)
	if goal.Objective == "" {
		return nil, "", errors.New("objective 不能为空：一句话说明要达成什么")
	}
	if goal.Acceptance == "" {
		return nil, "", errors.New("acceptance 不能为空：怎样才算完成，必须能被检查")
	}
	goal.Constraints = clipList(action.Constraints, MaxConstraints)
	stages, err := buildStages(action.Stages)
	if err != nil {
		return nil, "", err
	}
	goal.Stages = stages
	return finish(goal, actor, "draft", clip(goal.Objective, 80), now,
		"目标草稿已保存。把目标、验收标准和阶段讲给用户确认；用户同意后再用 start 开始。")
}

func applyReport(goal *Goal, action Action, actor Actor, now time.Time) (*Goal, string, error) {
	if err := requireRunning(goal); err != nil {
		return nil, "", err
	}
	if goal.Current >= len(goal.Stages) {
		return nil, "", errors.New("没有进行中的阶段")
	}
	status := strings.ToLower(strings.TrimSpace(action.Status))
	if status == "" {
		status = TaskDone
	}
	if status != TaskDoing && status != TaskDone {
		return nil, "", errors.New("status 只能是 doing 或 done")
	}
	stage := &goal.Stages[goal.Current]
	needle := strings.TrimSpace(action.Task)
	updated := 0
	for i := range stage.Tasks {
		task := &stage.Tasks[i]
		if task.BotID != actor.BotID || task.Status == TaskDone {
			continue
		}
		if needle != "" && !strings.Contains(task.Task, needle) {
			continue
		}
		task.Status = status
		task.Note = clip(action.Note, maxText)
		updated++
		if needle == "" {
			break
		}
	}
	if updated == 0 {
		return nil, "", errors.New("当前阶段没有分派给你的未完成任务")
	}
	message := "已记录。"
	if status == TaskDone && len(openTasks(stage)) == 0 {
		message = "已记录；当前阶段的任务都已完成，等待群主验收。"
	}
	return finish(goal, actor, "report", status, now, message)
}

func reviseGoal(goal *Goal, action Action) error {
	if text := clip(action.Objective, maxText); text != "" {
		goal.Objective = text
	}
	if text := clip(action.Acceptance, maxText); text != "" {
		goal.Acceptance = text
	}
	if action.Constraints != nil {
		goal.Constraints = clipList(action.Constraints, MaxConstraints)
	}
	if len(action.Stages) == 0 {
		return nil
	}
	stages, err := buildStages(action.Stages)
	if err != nil {
		return err
	}
	// Stages that already passed are history: a revision replaces only what
	// is still ahead, so recorded evidence cannot be rewritten away.
	passed := 0
	for passed < len(goal.Stages) && goal.Stages[passed].Status == StagePassed {
		passed++
	}
	if passed+len(stages) > MaxStages {
		return fmt.Errorf("阶段总数不能超过 %d（已通过 %d 个）", MaxStages, passed)
	}
	goal.Stages = append(goal.Stages[:passed:passed], stages...)
	goal.Current = passed
	if goal.Status != StatusDrafting {
		goal.Stages[passed].Status = StageActive
	}
	return nil
}

func buildStages(inputs []StageInput) ([]Stage, error) {
	if len(inputs) == 0 {
		return nil, errors.New("至少需要一个阶段（stages），每个阶段要有 title 和 acceptance")
	}
	if len(inputs) > MaxStages {
		return nil, fmt.Errorf("阶段不能超过 %d 个", MaxStages)
	}
	stages := make([]Stage, 0, len(inputs))
	for index, input := range inputs {
		title := clip(input.Title, maxShortText)
		acceptance := clip(input.Acceptance, maxText)
		if title == "" || acceptance == "" {
			return nil, fmt.Errorf("第 %d 个阶段缺少 title 或 acceptance", index+1)
		}
		stages = append(stages, Stage{Title: title, Acceptance: acceptance, Status: StagePending, Tasks: []Task{}})
	}
	return stages, nil
}

func requireRunning(goal *Goal) error {
	if goal.Status != StatusActive {
		return fmt.Errorf("目标当前是 %s，不在进行中", goal.Status)
	}
	return nil
}

func pickStage(goal *Goal, number int) (*Stage, int, error) {
	index := goal.Current
	if number > 0 {
		index = number - 1
	}
	if index < 0 || index >= len(goal.Stages) {
		return nil, 0, fmt.Errorf("没有第 %d 个阶段", index+1)
	}
	return &goal.Stages[index], index, nil
}

func findMember(members []Member, ref string) (Member, error) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "@")
	if ref == "" {
		return Member{}, errors.New("bot 不能为空：写成员名称")
	}
	for _, member := range members {
		if member.ID == ref || strings.EqualFold(member.Name, ref) {
			return member, nil
		}
	}
	names := make([]string, 0, len(members))
	for _, member := range members {
		names = append(names, member.Name)
	}
	return Member{}, fmt.Errorf("群里没有成员 %q；成员有：%s", ref, strings.Join(names, "、"))
}

func openTasks(stage *Stage) []string {
	var open []string
	for _, task := range stage.Tasks {
		if task.Status != TaskDone {
			open = append(open, task.BotName+"（"+clip(task.Task, 30)+"）")
		}
	}
	return open
}

func finish(goal *Goal, actor Actor, action, detail string, now time.Time, message string) (*Goal, string, error) {
	who := actor.Name
	if actor.IsUser {
		who = "用户"
	}
	goal.Events = append(goal.Events, Event{At: now.UTC(), Actor: who, Action: action, Detail: detail})
	if len(goal.Events) > maxEvents {
		goal.Events = goal.Events[len(goal.Events)-maxEvents:]
	}
	// Any real progress re-arms the idle driver.
	goal.Nudges = 0
	return goal, message, nil
}

func clone(goal *Goal) *Goal {
	out := *goal
	out.Constraints = append([]string{}, goal.Constraints...)
	out.Events = append([]Event{}, goal.Events...)
	out.Stages = make([]Stage, len(goal.Stages))
	for i, stage := range goal.Stages {
		out.Stages[i] = stage
		out.Stages[i].Tasks = append([]Task{}, stage.Tasks...)
	}
	return &out
}

func clip(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return string(runes)
}

func clipList(items []string, limit int) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := clip(item, maxShortText); text != "" && len(out) < limit {
			out = append(out, text)
		}
	}
	return out
}
