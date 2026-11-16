package tools

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"superbot/internal/goals"
)

// groupGoalTool is how bots read and move the group's goal. The rules live in
// the goals package; this only adapts them to a tool call.
type groupGoalTool struct {
	service *goals.Service
	convID  uuid.UUID
	ownerID uuid.UUID
	actor   goals.Actor
	members []goals.Member
	changed func(ctx context.Context, goal *goals.Goal)
}

// NewGroupGoalTool builds the tool for one member of a team conversation.
// changed is called after every stored change so the UI can refresh.
func NewGroupGoalTool(service *goals.Service, convID, ownerID uuid.UUID, actor goals.Actor, members []goals.Member, changed func(ctx context.Context, goal *goals.Goal)) Tool {
	return &groupGoalTool{service: service, convID: convID, ownerID: ownerID, actor: actor, members: members, changed: changed}
}

func (t *groupGoalTool) Name() string { return "group_goal" }

func (t *groupGoalTool) Description() string {
	if t.actor.IsLead {
		return "管理这个群聊的目标（你是群主）。大任务先 draft 起草目标、验收标准和阶段，向用户确认后 start；" +
			"每个阶段用 assign 分派任务，成员完成后亲自核对，达标用 pass（必须写 evidence 说明依据），" +
			"不达标用 rework（必须写 feedback）；全部阶段通过后用 complete（必须写 evidence）。" +
			"也可以 revise / pause / resume / stop / status。小事不需要设目标。"
	}
	return "查看群聊目标，或汇报分派给你的任务。action=status 查看；action=report 汇报（status 为 doing 或 done，note 写清产物路径和依据）。" +
		"目标的起草、分派和验收由群主负责。"
}

func (t *groupGoalTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {"type": "string", "enum": ["status", "draft", "start", "revise", "assign", "report", "pass", "rework", "complete", "pause", "resume", "stop"]},
			"objective": {"type": "string", "description": "draft/revise：要达成什么"},
			"acceptance": {"type": "string", "description": "draft/revise：总体验收标准，必须能被检查"},
			"constraints": {"type": "array", "items": {"type": "string"}, "description": "draft/revise：用户提出的限制"},
			"stages": {
				"type": "array",
				"description": "draft/revise：按顺序的阶段",
				"items": {
					"type": "object",
					"properties": {"title": {"type": "string"}, "acceptance": {"type": "string"}},
					"required": ["title", "acceptance"]
				}
			},
			"stage": {"type": "integer", "description": "assign：阶段序号（从 1 开始），不填表示当前阶段"},
			"bot": {"type": "string", "description": "assign：成员名称"},
			"task": {"type": "string", "description": "assign：任务内容；report：可选，用来指明汇报的是哪个任务"},
			"status": {"type": "string", "enum": ["doing", "done"], "description": "report：任务状态"},
			"note": {"type": "string", "description": "report：产物和依据；pause/stop：原因"},
			"evidence": {"type": "string", "description": "pass/complete：依据什么判断达到了验收标准"},
			"feedback": {"type": "string", "description": "rework：具体哪里没达标"}
		},
		"required": ["action"]
	}`)
}

func (t *groupGoalTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var action goals.Action
	if err := json.Unmarshal(raw, &action); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	goal, message, err := t.service.Apply(ctx, t.convID, t.ownerID, action, t.actor, t.members)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	if goal == nil {
		return Result{Content: message}, nil
	}
	if action.Action != "status" && t.changed != nil {
		t.changed(ctx, goal)
	}
	ledger := goals.Render(goal, t.actor.BotID, t.actor.IsLead)
	if message == "" {
		return Result{Content: ledger}, nil
	}
	return Result{Content: message + "\n\n" + ledger}, nil
}
