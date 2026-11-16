package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

type subagentSpawnTool struct {
	db           *store.DB
	ownerID      uuid.UUID
	parentBotID  uuid.UUID
	parentConv   uuid.UUID
	parentCycle  uuid.UUID
	parentTaskID *uuid.UUID
	depth        int32
}

func NewSubagentSpawnTool(db *store.DB, ownerID, parentBotID, parentConv, parentCycle uuid.UUID, parentTaskID *uuid.UUID, depth int32) Tool {
	return &subagentSpawnTool{
		db: db, ownerID: ownerID, parentBotID: parentBotID, parentConv: parentConv,
		parentCycle: parentCycle, parentTaskID: parentTaskID, depth: depth,
	}
}

func (t *subagentSpawnTool) Name() string { return "spawn_subagent" }
func (t *subagentSpawnTool) Description() string {
	return "派生一个隔离子代理执行独立任务。返回 task_id；用 check_subagent 查看进度和摘要。"
}
func (t *subagentSpawnTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"objective":{"type":"string"}},"required":["objective"]}`)
}

func (t *subagentSpawnTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Objective string `json:"objective"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	objective := strings.TrimSpace(input.Objective)
	if objective == "" {
		return Result{Content: "objective 不能为空", IsError: true}, nil
	}
	// depth is the nesting level of the caller: 0 for a normal bot, 1 for a
	// subagent. A subagent may spawn one more level, and no further.
	if t.depth >= 2 {
		return Result{Content: "子代理嵌套最多 2 层", IsError: true}, nil
	}
	active, err := t.db.CountActiveSubagentsForOwner(ctx, t.ownerID)
	if err != nil {
		return Result{Content: "读取子代理状态失败: " + err.Error(), IsError: true}, nil
	}
	if active >= 8 {
		return Result{Content: "当前账户已有 8 个运行中的子代理，请先等待或停止。", IsError: true}, nil
	}
	tx, err := t.db.Pool.Begin(ctx)
	if err != nil {
		return Result{Content: "创建子代理失败: " + err.Error(), IsError: true}, nil
	}
	defer tx.Rollback(ctx)
	q := t.db.WithTx(tx)

	botID := t.parentBotID
	conv, err := q.CreateConversation(ctx, store.CreateConversationParams{
		DirectBotID: &botID,
		Kind:        "subagent",
		Title:       "子代理：" + truncateRunes(objective, 80),
		CreatedBy:   t.ownerID,
	})
	if err != nil {
		return Result{Content: "创建子代理会话失败: " + err.Error(), IsError: true}, nil
	}
	meta, _ := json.Marshal(map[string]any{"subagent": true, "parent_conversation_id": t.parentConv.String()})
	msg, err := q.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: conv.ID,
		SenderType:     "user",
		Content:        objective,
		Metadata:       meta,
	})
	if err != nil {
		return Result{Content: "创建子代理任务失败: " + err.Error(), IsError: true}, nil
	}
	cycle, err := q.CreateCycle(ctx, store.CreateCycleParams{
		ConversationID:   conv.ID,
		TriggerMessageID: msg.ID,
		MaxRounds:        1,
	})
	if err != nil {
		return Result{Content: "创建子代理执行周期失败: " + err.Error(), IsError: true}, nil
	}
	if err := q.SetMessageCycle(ctx, store.SetMessageCycleParams{ID: msg.ID, CycleID: &cycle.ID}); err != nil {
		return Result{Content: "关联子代理周期失败: " + err.Error(), IsError: true}, nil
	}
	task, err := q.CreateSubagentTask(ctx, store.CreateSubagentTaskParams{
		OwnerID:              t.ownerID,
		ParentConversationID: t.parentConv,
		ParentCycleID:        t.parentCycle,
		ParentBotID:          t.parentBotID,
		ChildConversationID:  conv.ID,
		ChildCycleID:         &cycle.ID,
		Objective:            objective,
		ParentTaskID:         t.parentTaskID,
		Depth:                t.depth,
	})
	if err != nil {
		return Result{Content: "记录子代理任务失败: " + err.Error(), IsError: true}, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{Content: "提交子代理任务失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(map[string]any{"task_id": task.ID.String(), "status": task.Status})
	return Result{Content: string(encoded)}, nil
}

type subagentCheckTool struct {
	db      *store.DB
	ownerID uuid.UUID
}

func NewSubagentCheckTool(db *store.DB, ownerID uuid.UUID) Tool {
	return &subagentCheckTool{db: db, ownerID: ownerID}
}
func (t *subagentCheckTool) Name() string { return "check_subagent" }
func (t *subagentCheckTool) Description() string {
	return "查看子代理任务状态和结果摘要。"
}
func (t *subagentCheckTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"}},"required":["task_id"]}`)
}
func (t *subagentCheckTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(input.TaskID))
	if err != nil {
		return Result{Content: "task_id 格式不正确", IsError: true}, nil
	}
	task, err := t.db.GetSubagentTaskForOwner(ctx, store.GetSubagentTaskForOwnerParams{ID: id, OwnerID: t.ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Result{Content: "子代理任务不存在", IsError: true}, nil
		}
		return Result{Content: "读取子代理失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(map[string]any{
		"task_id": task.ID.String(), "status": task.Status,
		"result": task.ResultSummary, "error": task.Error,
	})
	return Result{Content: string(encoded)}, nil
}

type subagentStopTool struct {
	db      *store.DB
	ownerID uuid.UUID
}

func NewSubagentStopTool(db *store.DB, ownerID uuid.UUID) Tool {
	return &subagentStopTool{db: db, ownerID: ownerID}
}
func (t *subagentStopTool) Name() string        { return "stop_subagent" }
func (t *subagentStopTool) Description() string { return "停止一个运行中的子代理任务。" }
func (t *subagentStopTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"}},"required":["task_id"]}`)
}
func (t *subagentStopTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(input.TaskID))
	if err != nil {
		return Result{Content: "task_id 格式不正确", IsError: true}, nil
	}
	task, err := t.db.GetSubagentTaskForOwner(ctx, store.GetSubagentTaskForOwnerParams{ID: id, OwnerID: t.ownerID})
	if err != nil {
		return Result{Content: "子代理任务不存在", IsError: true}, nil
	}
	if task.ChildConversationID != uuid.Nil {
		if approvals, err := t.db.ListPendingApprovalsForConversation(ctx, task.ChildConversationID); err == nil {
			for _, approval := range approvals {
				_ = t.db.ExpireToolApproval(ctx, approval.ID)
				if approval.CheckpointID != nil {
					_ = t.db.DiscardCheckpoint(ctx, *approval.CheckpointID)
					_ = t.db.CancelCycleForCheckpoint(ctx, *approval.CheckpointID)
				}
			}
		}
		_, _ = t.db.RequestCycleCancel(ctx, task.ChildConversationID)
	}
	n, err := t.db.CancelSubagentTask(ctx, store.CancelSubagentTaskParams{ID: id, OwnerID: t.ownerID})
	if err != nil || n == 0 {
		return Result{Content: fmt.Sprintf("无法停止任务（当前状态 %s）", task.Status), IsError: true}, nil
	}
	return Result{Content: "子代理任务已停止。"}, nil
}

type subagentMessageTool struct {
	db      *store.DB
	ownerID uuid.UUID
}

func NewSubagentMessageTool(db *store.DB, ownerID uuid.UUID) Tool {
	return &subagentMessageTool{db: db, ownerID: ownerID}
}
func (t *subagentMessageTool) Name() string { return "message_subagent" }
func (t *subagentMessageTool) Description() string {
	return "向正在等待审批的子代理发送补充指令。指令会写入 checkpoint，子代理恢复后生效；子代理未在等待审批时返回错误。"
}
func (t *subagentMessageTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"},"message":{"type":"string"}},"required":["task_id","message"]}`)
}
func (t *subagentMessageTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		TaskID  string `json:"task_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(input.TaskID))
	if err != nil {
		return Result{Content: "task_id 格式不正确", IsError: true}, nil
	}
	content := strings.TrimSpace(input.Message)
	if content == "" {
		return Result{Content: "message 不能为空", IsError: true}, nil
	}
	task, err := t.db.GetSubagentTaskForOwner(ctx, store.GetSubagentTaskForOwnerParams{ID: id, OwnerID: t.ownerID})
	if err != nil {
		return Result{Content: "子代理任务不存在", IsError: true}, nil
	}
	if task.Status != "running" {
		return Result{Content: fmt.Sprintf("子代理当前状态为 %s，无法发送指令", task.Status), IsError: true}, nil
	}
	meta, _ := json.Marshal(map[string]any{"subagent_task_id": task.ID.String(), "parent_steer": true})
	if _, err := t.db.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: task.ChildConversationID,
		SenderType:     "user",
		Content:        content,
		Metadata:       meta,
	}); err != nil {
		return Result{Content: "记录子代理指令失败: " + err.Error(), IsError: true}, nil
	}
	return Result{Content: `{"task_id":"` + task.ID.String() + `","delivered":true,"note":"将在子代理下一步或审批恢复后生效"}`}, nil
}
