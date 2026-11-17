package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/officetext"
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
	db        *store.DB
	ownerID   uuid.UUID
	workspace *os.Root
}

// NewReadLibraryTool reads Library items. Files (a Word template a user
// uploaded, a spreadsheet) are also copied into the bot's workspace so its
// computer can open them with the right library; workspace may be nil.
func NewReadLibraryTool(db *store.DB, ownerID uuid.UUID, workspace *os.Root) Tool {
	return &readLibraryTool{db: db, ownerID: ownerID, workspace: workspace}
}

func (t *readLibraryTool) Name() string { return "read_library" }

func (t *readLibraryTool) Description() string {
	return "读取资料库中某条资料的完整内容。Word/PPT/Excel 会返回提取出的正文；" +
		"资料是文件时还会把原文件复制到你电脑的 /workspace/uploads/，需要保留原格式（例如按模板填写）时在电脑里用 python-docx / openpyxl / python-pptx 处理它。"
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
	content := item.Content
	var notes []string
	if item.FilePath != "" {
		name := libraryFileName(item)
		raw, readErr := os.ReadFile(item.FilePath)
		switch {
		case readErr != nil:
			notes = append(notes, "（原文件已不在服务器上，只能读取保存的文字内容。）")
		default:
			if strings.TrimSpace(content) == "" && officetext.Supported(name) {
				if text, truncated, err := officetext.Extract(name, raw, maxLibraryContentBytes/4); err == nil {
					content = text
					if truncated {
						content += "\n…（正文较长，已截断；完整内容请在电脑里打开原文件）"
					}
				}
			}
			if inWorkspace := t.workspaceRelative(item.FilePath); inWorkspace != "" {
				notes = append(notes, "原文件就在你的电脑里：/workspace/"+inWorkspace)
			} else if t.workspace != nil {
				if rel, err := PlaceInWorkspace(t.workspace, name, raw); err == nil {
					notes = append(notes, "原文件已复制到你的电脑：/workspace/"+rel+"（工作区路径 "+rel+"）")
				}
			}
			if strings.TrimSpace(content) == "" {
				notes = append(notes, "（这是二进制文件，没有可直接显示的文字；请在电脑里用合适的工具打开。）")
			}
		}
	}
	text := item.Title + "\n\n" + content
	if len(notes) > 0 {
		text += "\n\n" + strings.Join(notes, "\n")
	}
	return Result{Content: text}, nil
}

// libraryFileName is the name the item's file should have when copied out:
// the original upload name when known, else the title, keeping the file's
// own extension so tools recognise its format.
func libraryFileName(item store.LibraryItem) string {
	var meta struct {
		OriginalName string `json:"original_name"`
	}
	_ = json.Unmarshal(item.Metadata, &meta)
	name := strings.TrimSpace(meta.OriginalName)
	if name == "" {
		name = strings.TrimSpace(item.Title)
	}
	if ext := filepath.Ext(item.FilePath); ext != "" && !strings.EqualFold(filepath.Ext(name), ext) {
		name += ext
	}
	return name
}

// workspaceRelative returns the path of hostPath inside the workspace, or ""
// when it lies elsewhere.
func (t *readLibraryTool) workspaceRelative(hostPath string) string {
	if t.workspace == nil {
		return ""
	}
	rel, err := filepath.Rel(t.workspace.Name(), hostPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return ""
	}
	return filepath.ToSlash(rel)
}
