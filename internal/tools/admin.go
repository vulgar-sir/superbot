package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/chat"
	"superbot/internal/routines"
	"superbot/internal/store"
)

// Admin tools are exposed only to an account's primary Bot. They let the
// primary Bot manage the same objects a user can create from the UI.

type adminCreateAgentTool struct {
	db         *store.DB
	ownerID    uuid.UUID
	botID      uuid.UUID
	providerID *uuid.UUID
	model      string
}

func NewAdminCreateAgentTool(db *store.DB, ownerID, botID uuid.UUID, providerID *uuid.UUID, model string) Tool {
	return &adminCreateAgentTool{db: db, ownerID: ownerID, botID: botID, providerID: providerID, model: model}
}

func (t *adminCreateAgentTool) Name() string { return "create_agent" }
func (t *adminCreateAgentTool) Description() string {
	return "创建一个新的个人 Bot。仅账户主 Bot 可调用。"
}
func (t *adminCreateAgentTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"},"system_prompt":{"type":"string"},"avatar_shape":{"type":"string"},"avatar_color":{"type":"string"}},"required":["name"]}`)
}
func (t *adminCreateAgentTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Name         string `json:"name"`
		Title        string `json:"title"`
		Description  string `json:"description"`
		SystemPrompt string `json:"system_prompt"`
		AvatarShape  string `json:"avatar_shape"`
		AvatarColor  string `json:"avatar_color"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Result{Content: "name 不能为空", IsError: true}, nil
	}
	if input.AvatarShape == "" {
		input.AvatarShape = "blob"
	}
	if input.AvatarColor == "" {
		input.AvatarColor = "violet"
	}
	bot, err := t.db.CreateBot(ctx, store.CreateBotParams{
		OwnerID:         t.ownerID,
		Name:            name,
		Slug:            adminSlug(name),
		Description:     strings.TrimSpace(input.Description),
		SystemPrompt:    strings.TrimSpace(input.SystemPrompt),
		ProviderID:      t.providerID,
		Model:           t.model,
		Params:          json.RawMessage(`{}`),
		Capabilities:    json.RawMessage(`{}`),
		ToolPolicy:      json.RawMessage(`{}`),
		MemoryEnabled:   true,
		ComputerProfile: "browser",
		Title:           strings.TrimSpace(input.Title),
		AvatarShape:     input.AvatarShape,
		AvatarColor:     input.AvatarColor,
		Kind:            "personal",
	})
	if err != nil {
		return Result{Content: "创建 Bot 失败: " + err.Error(), IsError: true}, nil
	}
	if err := t.db.SetBotCreator(ctx, store.SetBotCreatorParams{
		ID: bot.ID, OwnerID: t.ownerID, CreatedByBotID: &t.botID,
	}); err != nil {
		return Result{Content: "创建 Bot 后写入创建者失败: " + err.Error(), IsError: true}, nil
	}
	if err := t.db.SetBotAccess(ctx, store.SetBotAccessParams{
		ID: bot.ID, OwnerID: t.ownerID, Visibility: "account", CreatedByUserID: nil,
	}); err != nil {
		return Result{Content: "创建 Bot 后设置可见性失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(map[string]any{
		"bot_id": bot.ID.String(), "name": bot.Name, "kind": bot.Kind,
	})
	return Result{Content: string(encoded)}, nil
}

type adminCreateGroupTool struct {
	db      *store.DB
	chat    *chat.Service
	ownerID uuid.UUID
}

func NewAdminCreateGroupTool(db *store.DB, chatService *chat.Service, ownerID uuid.UUID) Tool {
	return &adminCreateGroupTool{db: db, chat: chatService, ownerID: ownerID}
}

func (t *adminCreateGroupTool) Name() string { return "create_group" }
func (t *adminCreateGroupTool) Description() string {
	return "创建一个群聊（最多 6 个账户内 Bot），并生成群聊房间。bot_ids 可以是 Bot ID 或 Bot 名称。仅账户主 Bot 可调用。"
}
func (t *adminCreateGroupTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"bot_ids":{"type":"array","items":{"type":"string"}}},"required":["name","bot_ids"]}`)
}
func (t *adminCreateGroupTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		BotIDs      []string `json:"bot_ids"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Result{Content: "name 不能为空", IsError: true}, nil
	}
	if len(input.BotIDs) == 0 || len(input.BotIDs) > 6 {
		return Result{Content: "bot_ids 需要 1–6 个 Bot", IsError: true}, nil
	}
	accountBots, err := t.db.ListBots(ctx, t.ownerID)
	if err != nil {
		return Result{Content: "读取 Bot 列表失败: " + err.Error(), IsError: true}, nil
	}
	byID := make(map[uuid.UUID]store.Bot, len(accountBots))
	byName := make(map[string]store.Bot, len(accountBots))
	for _, item := range accountBots {
		byID[item.ID] = item
		byName[strings.ToLower(strings.TrimSpace(item.Name))] = item
	}
	botIDs := make([]uuid.UUID, 0, len(input.BotIDs))
	for _, rawID := range input.BotIDs {
		value := strings.TrimSpace(rawID)
		if id, err := uuid.Parse(value); err == nil {
			if _, ok := byID[id]; !ok {
				return Result{Content: "Bot 不存在或不属于当前账户: " + id.String(), IsError: true}, nil
			}
			botIDs = append(botIDs, id)
			continue
		}
		// Accept a Bot name so the model does not have to know UUIDs.
		item, ok := byName[strings.ToLower(value)]
		if !ok {
			return Result{Content: "找不到名为「" + value + "」的 Bot；可先用 list_agents 查看账户内 Bot", IsError: true}, nil
		}
		botIDs = append(botIDs, item.ID)
	}

	team, err := t.db.CreateTeam(ctx, store.CreateTeamParams{
		OwnerID:     t.ownerID,
		Name:        name,
		Description: strings.TrimSpace(input.Description),
		Mode:        "group",
		Config:      json.RawMessage(`{}`),
	})
	if err != nil {
		return Result{Content: "创建群聊失败: " + err.Error(), IsError: true}, nil
	}
	for position, botID := range botIDs {
		if err := t.db.AddTeamBot(ctx, store.AddTeamBotParams{
			TeamID: team.ID, BotID: botID, Position: int32(position), Role: "member",
		}); err != nil {
			_, _ = t.db.DeleteTeam(ctx, store.DeleteTeamParams{ID: team.ID, OwnerID: t.ownerID})
			return Result{Content: "添加群聊成员失败: " + err.Error(), IsError: true}, nil
		}
	}
	conv, err := t.chat.CreateTeamConversation(ctx, t.ownerID, team.ID, team.Name)
	if err != nil {
		_, _ = t.db.DeleteTeam(ctx, store.DeleteTeamParams{ID: team.ID, OwnerID: t.ownerID})
		return Result{Content: "创建群聊房间失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(map[string]any{
		"team_id": team.ID.String(), "name": team.Name, "conversation_id": conv.ID.String(),
	})
	return Result{Content: string(encoded)}, nil
}

type adminCreateRoutineTool struct {
	db      *store.DB
	routine *routines.Service
	ownerID uuid.UUID
	botID   uuid.UUID
}

func NewAdminCreateRoutineTool(db *store.DB, routineService *routines.Service, ownerID, botID uuid.UUID) Tool {
	return &adminCreateRoutineTool{db: db, routine: routineService, ownerID: ownerID, botID: botID}
}

func (t *adminCreateRoutineTool) Name() string { return "create_routine" }
func (t *adminCreateRoutineTool) Description() string {
	return "为一个账户内 Bot 创建例程；bot_id 可传 Bot ID 或名称。仅账户主 Bot 可调用。"
}
func (t *adminCreateRoutineTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"schedule":{"type":"string"},"prompt":{"type":"string"},"bot_id":{"type":"string"},"timezone":{"type":"string"},"timing":{"type":"string"}},"required":["name","schedule","prompt"]}`)
}
func (t *adminCreateRoutineTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Prompt   string `json:"prompt"`
		BotID    string `json:"bot_id"`
		Timezone string `json:"timezone"`
		Timing   string `json:"timing"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	target := t.botID
	if value := strings.TrimSpace(input.BotID); value != "" {
		if id, err := uuid.Parse(value); err == nil {
			target = id
		} else {
			// Accept a Bot name as well as an ID.
			bots, err := t.db.ListBots(ctx, t.ownerID)
			if err != nil {
				return Result{Content: "读取 Bot 列表失败: " + err.Error(), IsError: true}, nil
			}
			found := false
			for _, item := range bots {
				if strings.EqualFold(strings.TrimSpace(item.Name), value) {
					target = item.ID
					found = true
					break
				}
			}
			if !found {
				return Result{Content: "找不到名为「" + value + "」的 Bot；可先用 list_agents 查看账户内 Bot", IsError: true}, nil
			}
		}
	}
	timezone := strings.TrimSpace(input.Timezone)
	if timezone == "" {
		if raw, err := t.db.GetSetting(ctx, "timezone:"+t.ownerID.String()); err == nil {
			_ = json.Unmarshal(raw, &timezone)
		}
	}
	routine, err := t.routine.CreateBotRoutine(
		ctx, t.ownerID, target,
		input.Name, input.Schedule, timezone, input.Timing, input.Prompt, true,
	)
	if err != nil {
		return Result{Content: "创建例程失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(map[string]any{
		"routine_id": routine.ID.String(), "bot_id": routine.BotID.String(),
		"schedule": routine.Schedule, "next_run_at": routine.NextRunAt,
	})
	return Result{Content: string(encoded)}, nil
}

type adminSetPrimaryBotTool struct {
	db      *store.DB
	ownerID uuid.UUID
	botID   uuid.UUID
}

func NewAdminSetPrimaryBotTool(db *store.DB, ownerID, botID uuid.UUID) Tool {
	return &adminSetPrimaryBotTool{db: db, ownerID: ownerID, botID: botID}
}

func (t *adminSetPrimaryBotTool) Name() string { return "set_primary_bot" }
func (t *adminSetPrimaryBotTool) Description() string {
	return "把账户内一个个人 Bot 设为主 Bot。仅账户主 Bot 可调用。"
}
func (t *adminSetPrimaryBotTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"bot_id":{"type":"string"}},"required":["bot_id"]}`)
}
func (t *adminSetPrimaryBotTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		BotID string `json:"bot_id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(input.BotID))
	if err != nil {
		return Result{Content: "bot_id 格式不正确", IsError: true}, nil
	}
	bot, err := t.db.GetBot(ctx, store.GetBotParams{ID: id, OwnerID: t.ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Result{Content: "Bot 不存在或不属于当前账户", IsError: true}, nil
		}
		return Result{Content: "读取 Bot 失败: " + err.Error(), IsError: true}, nil
	}
	if bot.Kind == "team" {
		return Result{Content: "Team Bot 不能设为主 Bot", IsError: true}, nil
	}
	if err := t.db.SetAccountPrimaryBot(ctx, store.SetAccountPrimaryBotParams{
		ID: t.ownerID, PrimaryBotID: &id,
	}); err != nil {
		return Result{Content: "设置主 Bot 失败: " + err.Error(), IsError: true}, nil
	}
	if members, err := t.db.ListAccountMembers(ctx, t.ownerID); err == nil {
		for _, member := range members {
			if member.Role != "owner" {
				continue
			}
			_ = t.db.SetPrimaryBot(ctx, store.SetPrimaryBotParams{
				ID: member.ID, PrimaryBotID: &id,
			})
		}
	}
	encoded, _ := json.Marshal(map[string]any{
		"primary_bot_id": bot.ID.String(), "name": bot.Name,
	})
	return Result{Content: string(encoded)}, nil
}

func adminSlug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "agent"
	}
	if len(slug) > 48 {
		slug = strings.TrimRight(slug[:48], "-")
	}
	return slug + "-" + uuid.NewString()[:8]
}

type adminListAgentsTool struct {
	db      *store.DB
	ownerID uuid.UUID
}

func NewAdminListAgentsTool(db *store.DB, ownerID uuid.UUID) Tool {
	return &adminListAgentsTool{db: db, ownerID: ownerID}
}

func (t *adminListAgentsTool) Name() string { return "list_agents" }
func (t *adminListAgentsTool) Description() string {
	return "列出当前账户内的 Bot（id、名称、类型、模型），用于创建群聊或切换主 Bot。仅账户主 Bot 可调用。"
}
func (t *adminListAgentsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *adminListAgentsTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	bots, err := t.db.ListBots(ctx, t.ownerID)
	if err != nil {
		return Result{Content: "读取 Bot 列表失败: " + err.Error(), IsError: true}, nil
	}
	items := make([]map[string]any, 0, len(bots))
	for _, item := range bots {
		items = append(items, map[string]any{
			"id": item.ID.String(), "name": item.Name, "kind": item.Kind, "model": item.Model,
		})
	}
	encoded, _ := json.Marshal(map[string]any{"bots": items})
	return Result{Content: string(encoded)}, nil
}
