package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

type taskCreateTool struct {
	db      *store.DB
	ownerID uuid.UUID
	botID   uuid.UUID
}

func NewTaskCreateTool(db *store.DB, ownerID, botID uuid.UUID) Tool {
	return &taskCreateTool{db: db, ownerID: ownerID, botID: botID}
}
func (t *taskCreateTool) Name() string { return "create_task" }
func (t *taskCreateTool) Description() string {
	return "创建并可选地分配给一个 Bot 的任务。"
}
func (t *taskCreateTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"description":{"type":"string"},"assignee_bot_id":{"type":"string"},"priority":{"type":"integer"}},"required":["title"]}`)
}
func (t *taskCreateTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Assignee    string `json:"assignee_bot_id"`
		Priority    int32  `json:"priority"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return Result{Content: "title 不能为空", IsError: true}, nil
	}
	var assignee *uuid.UUID
	if strings.TrimSpace(input.Assignee) != "" {
		id, err := uuid.Parse(strings.TrimSpace(input.Assignee))
		if err != nil {
			return Result{Content: "assignee_bot_id 格式不正确", IsError: true}, nil
		}
		assignee = &id
	}
	task, err := t.db.CreateTask(ctx, store.CreateTaskParams{
		OwnerID: t.ownerID, Title: title, Description: strings.TrimSpace(input.Description),
		Status: "todo", Priority: input.Priority, AssigneeBotID: assignee,
		CreatedByUserID: t.ownerID,
	})
	if err != nil {
		return Result{Content: "创建任务失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(map[string]any{"task_id": task.ID.String(), "status": task.Status})
	return Result{Content: string(encoded)}, nil
}

type taskListTool struct {
	db      *store.DB
	ownerID uuid.UUID
}

func NewTaskListTool(db *store.DB, ownerID uuid.UUID) Tool {
	return &taskListTool{db: db, ownerID: ownerID}
}
func (t *taskListTool) Name() string        { return "list_tasks" }
func (t *taskListTool) Description() string { return "列出任务，可按状态过滤。" }
func (t *taskListTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"status":{"type":"string"}}}`)
}
func (t *taskListTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(raw, &input)
	var status *string
	if strings.TrimSpace(input.Status) != "" {
		value := strings.TrimSpace(input.Status)
		status = &value
	}
	tasks, err := t.db.ListTasks(ctx, store.ListTasksParams{OwnerID: t.ownerID, Status: status, Limit: 100})
	if err != nil {
		return Result{Content: "读取任务失败: " + err.Error(), IsError: true}, nil
	}
	out := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, map[string]any{
			"id": task.ID.String(), "title": task.Title, "status": task.Status,
			"priority": task.Priority, "assignee_bot_id": task.AssigneeBotID,
		})
	}
	encoded, _ := json.Marshal(out)
	return Result{Content: string(encoded)}, nil
}

type taskUpdateTool struct {
	db      *store.DB
	ownerID uuid.UUID
}

func NewTaskUpdateTool(db *store.DB, ownerID uuid.UUID) Tool {
	return &taskUpdateTool{db: db, ownerID: ownerID}
}
func (t *taskUpdateTool) Name() string        { return "update_task" }
func (t *taskUpdateTool) Description() string { return "更新任务状态或标题。" }
func (t *taskUpdateTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"},"status":{"type":"string"},"title":{"type":"string"}},"required":["task_id"]}`)
}
func (t *taskUpdateTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(input.TaskID))
	if err != nil {
		return Result{Content: "task_id 格式不正确", IsError: true}, nil
	}
	existing, err := t.db.GetTaskForOwner(ctx, store.GetTaskForOwnerParams{ID: id, OwnerID: t.ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Result{Content: "任务不存在", IsError: true}, nil
		}
		return Result{Content: "读取任务失败: " + err.Error(), IsError: true}, nil
	}
	title := existing.Title
	if strings.TrimSpace(input.Title) != "" {
		title = strings.TrimSpace(input.Title)
	}
	status := existing.Status
	if strings.TrimSpace(input.Status) != "" {
		status = strings.TrimSpace(input.Status)
	}
	task, err := t.db.UpdateTask(ctx, store.UpdateTaskParams{
		ID: id, OwnerID: t.ownerID, Title: title, Description: existing.Description,
		Status: status, Priority: existing.Priority, AssigneeBotID: existing.AssigneeBotID, DueAt: existing.DueAt,
	})
	if err != nil {
		return Result{Content: "更新任务失败: " + err.Error(), IsError: true}, nil
	}
	return Result{Content: "任务已更新: " + task.Title + " [" + task.Status + "]"}, nil
}

type projectShareTool struct {
	db      *store.DB
	ownerID uuid.UUID
}

func NewProjectShareTool(db *store.DB, ownerID uuid.UUID) Tool {
	return &projectShareTool{db: db, ownerID: ownerID}
}
func (t *projectShareTool) Name() string { return "share_project" }
func (t *projectShareTool) Description() string {
	return "把项目共享给当前账户的成员，角色可为 viewer/editor/runner。"
}
func (t *projectShareTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"project_id":{"type":"string"},"email":{"type":"string"},"role":{"type":"string"}},"required":["project_id","email"]}`)
}
func (t *projectShareTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		ProjectID string `json:"project_id"`
		Email     string `json:"email"`
		Role      string `json:"role"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	projectID, err := uuid.Parse(strings.TrimSpace(input.ProjectID))
	if err != nil {
		return Result{Content: "project_id 格式不正确", IsError: true}, nil
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if email == "" {
		return Result{Content: "email 不能为空", IsError: true}, nil
	}
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "viewer"
	}
	if role != "viewer" && role != "editor" && role != "runner" {
		return Result{Content: "role 必须是 viewer、editor 或 runner", IsError: true}, nil
	}
	if _, err := t.db.GetProjectForOwner(ctx, store.GetProjectForOwnerParams{ID: projectID, OwnerID: t.ownerID}); err != nil {
		return Result{Content: "项目不存在", IsError: true}, nil
	}
	user, err := t.db.GetUserByEmail(ctx, email)
	if err != nil {
		return Result{Content: "该邮箱没有对应用户", IsError: true}, nil
	}
	if _, err := t.db.GetAccountMembership(ctx, store.GetAccountMembershipParams{AccountID: t.ownerID, UserID: user.ID}); err != nil {
		return Result{Content: "该用户不是当前账户成员，请先邀请", IsError: true}, nil
	}
	if err := t.db.UpsertResourceACL(ctx, store.UpsertResourceACLParams{
		AccountID: t.ownerID, ResourceType: "project", ResourceID: projectID,
		UserID: &user.ID, Role: role,
	}); err != nil {
		return Result{Content: "共享项目失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(map[string]any{
		"project_id": projectID.String(), "email": email, "role": role, "shared": true,
	})
	return Result{Content: string(encoded)}, nil
}

type projectJoinTool struct {
	db      *store.DB
	ownerID uuid.UUID
}

func NewProjectJoinTool(db *store.DB, ownerID uuid.UUID) Tool {
	return &projectJoinTool{db: db, ownerID: ownerID}
}
func (t *projectJoinTool) Name() string { return "join_project" }
func (t *projectJoinTool) Description() string {
	return "让一个账户成员加入项目；没有既有权限时授予 viewer，已有更高权限保持不变。"
}
func (t *projectJoinTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"project_id":{"type":"string"},"email":{"type":"string"}},"required":["project_id","email"]}`)
}
func (t *projectJoinTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		ProjectID string `json:"project_id"`
		Email     string `json:"email"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	projectID, err := uuid.Parse(strings.TrimSpace(input.ProjectID))
	if err != nil {
		return Result{Content: "project_id 格式不正确", IsError: true}, nil
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if email == "" {
		return Result{Content: "email 不能为空", IsError: true}, nil
	}
	if _, err := t.db.GetProjectForOwner(ctx, store.GetProjectForOwnerParams{ID: projectID, OwnerID: t.ownerID}); err != nil {
		return Result{Content: "项目不存在", IsError: true}, nil
	}
	user, err := t.db.GetUserByEmail(ctx, email)
	if err != nil {
		return Result{Content: "该邮箱没有对应用户", IsError: true}, nil
	}
	if _, err := t.db.GetAccountMembership(ctx, store.GetAccountMembershipParams{AccountID: t.ownerID, UserID: user.ID}); err != nil {
		return Result{Content: "该用户不是当前账户成员，请先邀请", IsError: true}, nil
	}
	role := "viewer"
	if existing, err := t.db.GetResourceACLRole(ctx, store.GetResourceACLRoleParams{
		ResourceType: "project", ResourceID: projectID, UserID: &user.ID,
	}); err == nil && existing != "" {
		role = existing
	} else if err := t.db.UpsertResourceACL(ctx, store.UpsertResourceACLParams{
		AccountID: t.ownerID, ResourceType: "project", ResourceID: projectID,
		UserID: &user.ID, Role: role,
	}); err != nil {
		return Result{Content: "加入项目失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(map[string]any{
		"project_id": projectID.String(), "email": email, "role": role, "joined": true,
	})
	return Result{Content: string(encoded)}, nil
}
