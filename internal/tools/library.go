package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/store"
)

const maxLibraryContentBytes = 512 * 1024

type saveToLibraryTool struct {
	db             *store.DB
	ownerID        uuid.UUID
	botID          uuid.UUID
	conversationID uuid.UUID
}

func NewSaveToLibraryTool(db *store.DB, ownerID, botID, conversationID uuid.UUID) Tool {
	return &saveToLibraryTool{db: db, ownerID: ownerID, botID: botID, conversationID: conversationID}
}

func (t *saveToLibraryTool) Name() string { return "save_to_library" }

func (t *saveToLibraryTool) Description() string {
	return "把一份成果保存到当前 Bot 的资料库，用户可以在 Bot 详情 → 资料库里查看。适合保存报告、页面、文档、清单等。"
}

func (t *saveToLibraryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"title": {"type": "string", "description": "资料标题"},
			"description": {"type": "string", "description": "一句话说明"},
			"kind": {"type": "string", "enum": ["page", "document", "file", "media"], "description": "资料类型，默认 page"},
			"content": {"type": "string", "description": "Markdown 或纯文本内容"}
		},
		"required": ["title", "content"]
	}`)
}

func (t *saveToLibraryTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Kind        string `json:"kind"`
		Content     string `json:"content"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Content = strings.TrimSpace(input.Content)
	if input.Title == "" || input.Content == "" {
		return Result{Content: "title and content are required", IsError: true}, nil
	}
	if len(input.Content) > maxLibraryContentBytes {
		return Result{Content: "content is too large (max 512KB)", IsError: true}, nil
	}
	switch input.Kind {
	case "", "page", "document", "file", "media":
	default:
		return Result{Content: "invalid kind", IsError: true}, nil
	}
	if input.Kind == "" {
		input.Kind = "page"
	}
	item, err := t.db.CreateLibraryItem(ctx, store.CreateLibraryItemParams{
		OwnerID:        t.ownerID,
		BotID:          t.botID,
		Kind:           input.Kind,
		Title:          input.Title,
		Description:    strings.TrimSpace(input.Description),
		Content:        input.Content,
		MimeType:       "text/markdown",
		FilePath:       "",
		ConversationID: &t.conversationID,
		Metadata:       json.RawMessage(`{}`),
	})
	if err != nil {
		return Result{Content: "save failed: " + err.Error(), IsError: true}, nil
	}
	return Result{Content: fmt.Sprintf("已保存到资料库：%s（id=%s）", item.Title, item.ID)}, nil
}

type listLibraryTool struct {
	db      *store.DB
	ownerID uuid.UUID
	botID   uuid.UUID
}

func NewListLibraryTool(db *store.DB, ownerID, botID uuid.UUID) Tool {
	return &listLibraryTool{db: db, ownerID: ownerID, botID: botID}
}

func (t *listLibraryTool) Name() string { return "list_library" }

func (t *listLibraryTool) Description() string {
	return "列出当前 Bot 资料库中的资料，返回 id、标题、类型和时间。"
}

func (t *listLibraryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","description":"最多返回多少条，默认 20"}}}`)
}

func (t *listLibraryTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	var input struct {
		Limit int32 `json:"limit"`
	}
	_ = json.Unmarshal(args, &input)
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 20
	}
	items, err := t.db.ListLibraryItemsForBot(ctx, store.ListLibraryItemsForBotParams{
		OwnerID: t.ownerID, BotID: t.botID, Limit: input.Limit,
	})
	if err != nil {
		return Result{Content: "list failed: " + err.Error(), IsError: true}, nil
	}
	if len(items) == 0 {
		return Result{Content: "资料库还是空的。"}, nil
	}
	var sb strings.Builder
	for _, item := range items {
		fmt.Fprintf(&sb, "- %s | %s | %s | %s\n", item.ID, item.Kind, item.Title, item.CreatedAt.Format("2006-01-02 15:04"))
	}
	return Result{Content: sb.String()}, nil
}

type readLibraryTool struct {
	db      *store.DB
	ownerID uuid.UUID
}

func NewReadLibraryTool(db *store.DB, ownerID uuid.UUID) Tool {
	return &readLibraryTool{db: db, ownerID: ownerID}
}

func (t *readLibraryTool) Name() string { return "read_library" }

func (t *readLibraryTool) Description() string {
	return "读取资料库中某条资料的完整内容。"
}

func (t *readLibraryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"item_id":{"type":"string","description":"资料 id"}},"required":["item_id"]}`)
}

func (t *readLibraryTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	var input struct {
		ItemID string `json:"item_id"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	id, err := uuid.Parse(input.ItemID)
	if err != nil {
		return Result{Content: "invalid item_id", IsError: true}, nil
	}
	item, err := t.db.GetLibraryItem(ctx, store.GetLibraryItemParams{ID: id, OwnerID: t.ownerID})
	if err != nil {
		return Result{Content: "library item not found", IsError: true}, nil
	}
	return Result{Content: item.Title + "\n\n" + item.Content}, nil
}
