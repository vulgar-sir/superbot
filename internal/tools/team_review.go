package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/store"
)

type teamReviewTool struct {
	db      *store.DB
	ownerID uuid.UUID
	botID   uuid.UUID
	botKind string
}

func NewTeamReviewTool(db *store.DB, ownerID, botID uuid.UUID, botKind string) Tool {
	return &teamReviewTool{db: db, ownerID: ownerID, botID: botID, botKind: botKind}
}

func (t *teamReviewTool) Name() string { return "submit_for_review" }
func (t *teamReviewTool) Description() string {
	return "Team Bot 把 Skill、记忆或发布内容提交给团队管理员审查。仅 Team Bot 可用；批准后才会生效（Skill 会按 Auto-Publish 规则附加）。"
}
func (t *teamReviewTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string","enum":["skill","memory","publish"]},"title":{"type":"string"},"description":{"type":"string"},"content":{"type":"string"},"resource_id":{"type":"string"}},"required":["kind","title"]}`)
}
func (t *teamReviewTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	if t.botKind != "team" {
		return Result{Content: "只有 Team Bot 可以提交团队审查", IsError: true}, nil
	}
	var input struct {
		Kind        string `json:"kind"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Content     string `json:"content"`
		ResourceID  string `json:"resource_id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	kind := strings.TrimSpace(input.Kind)
	switch kind {
	case "skill", "memory", "publish":
	default:
		return Result{Content: "kind 必须是 skill、memory 或 publish", IsError: true}, nil
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return Result{Content: "title 不能为空", IsError: true}, nil
	}
	var resourceID *uuid.UUID
	if kind == "skill" {
		parsed, err := uuid.Parse(strings.TrimSpace(input.ResourceID))
		if err != nil {
			return Result{Content: "skill 审查需要合法的 resource_id", IsError: true}, nil
		}
		resourceID = &parsed
	}
	review, err := t.db.CreateTeamReview(ctx, store.CreateTeamReviewParams{
		OwnerID:          t.ownerID,
		TeamBotID:        t.botID,
		Kind:             kind,
		ResourceID:       resourceID,
		Title:            title,
		Description:      strings.TrimSpace(input.Description),
		Content:          strings.TrimSpace(input.Content),
		SubmittedByBotID: &t.botID,
	})
	if err != nil {
		return Result{Content: "提交审查失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(map[string]any{"review_id": review.ID.String(), "status": review.Status})
	return Result{Content: string(encoded)}, nil
}
