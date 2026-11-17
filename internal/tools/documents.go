package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/store"
)

// DocumentIndexer files a new account document into a bot's Library.
type DocumentIndexer interface {
	IndexDocument(ctx context.Context, ownerID, botID uuid.UUID, document store.Document) (bool, error)
}

type documentCreateTool struct {
	db      *store.DB
	ownerID uuid.UUID
	botID   uuid.UUID
	library DocumentIndexer
}

// NewDocumentCreateTool creates account documents. The document also lands
// in the creating bot's Library, which is where people look for what a bot
// wrote; library may be nil in tests.
func NewDocumentCreateTool(db *store.DB, ownerID, botID uuid.UUID, library DocumentIndexer) Tool {
	return &documentCreateTool{db: db, ownerID: ownerID, botID: botID, library: library}
}
func (t *documentCreateTool) Name() string { return "create_document" }
func (t *documentCreateTool) Description() string {
	return "创建账户文档或 Canvas（Markdown），返回 document_id。文档会出现在账户「文档」列表，并收进你的资料库。" +
		"只有调用成功并拿到 document_id 后，才能告诉用户文档已写好。"
}
func (t *documentCreateTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"content":{"type":"string"},"kind":{"type":"string","enum":["document","canvas"]}},"required":["title"]}`)
}
func (t *documentCreateTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Kind    string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return Result{Content: "title 不能为空", IsError: true}, nil
	}
	kind := input.Kind
	if kind != "canvas" {
		kind = "document"
	}
	doc, err := t.db.CreateDocument(ctx, store.CreateDocumentParams{
		OwnerID: t.ownerID, Title: title, Content: input.Content, Kind: kind,
	})
	if err != nil {
		return Result{Content: "创建文档失败: " + err.Error(), IsError: true}, nil
	}
	inLibrary := false
	if t.library != nil && t.botID != uuid.Nil {
		if _, err := t.library.IndexDocument(ctx, t.ownerID, t.botID, doc); err == nil {
			inLibrary = true
		}
	}
	encoded, _ := json.Marshal(map[string]any{
		"document_id": doc.ID.String(),
		"kind":        doc.Kind,
		"title":       doc.Title,
		"in_library":  inLibrary,
	})
	return Result{Content: string(encoded)}, nil
}

type documentReadTool struct {
	db      *store.DB
	ownerID uuid.UUID
}

func NewDocumentReadTool(db *store.DB, ownerID uuid.UUID) Tool {
	return &documentReadTool{db: db, ownerID: ownerID}
}
func (t *documentReadTool) Name() string { return "read_document" }
func (t *documentReadTool) Description() string {
	return "读取文档/Canvas 内容；不传 document_id 时列出最近文档。"
}
func (t *documentReadTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"document_id":{"type":"string"}}}`)
}
func (t *documentReadTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		DocumentID string `json:"document_id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	if strings.TrimSpace(input.DocumentID) == "" {
		docs, err := t.db.ListDocuments(ctx, t.ownerID)
		if err != nil {
			return Result{Content: "读取文档列表失败: " + err.Error(), IsError: true}, nil
		}
		encoded, _ := json.Marshal(docs)
		return Result{Content: string(encoded)}, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(input.DocumentID))
	if err != nil {
		return Result{Content: "document_id 格式不正确", IsError: true}, nil
	}
	doc, err := t.db.GetDocument(ctx, store.GetDocumentParams{ID: id, OwnerID: t.ownerID})
	if err != nil {
		return Result{Content: "文档不存在", IsError: true}, nil
	}
	encoded, _ := json.Marshal(doc)
	return Result{Content: string(encoded)}, nil
}
