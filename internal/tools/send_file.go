package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/events"
	libindex "superbot/internal/library"
	"superbot/internal/store"
)

const (
	maxSentFileBytes  = 25 << 20
	maxSentTotalBytes = 50 << 20
	maxSentFiles      = 10
)

// fileTypes covers the formats bots produce most, so they get their proper
// type even where the system MIME table lacks them.
var fileTypes = map[string]string{
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".doc":  "application/msword",
	".xls":  "application/vnd.ms-excel",
	".ppt":  "application/vnd.ms-powerpoint",
	".pdf":  "application/pdf",
	".md":   "text/markdown; charset=utf-8",
	".txt":  "text/plain; charset=utf-8",
	".csv":  "text/csv; charset=utf-8",
	".zip":  "application/zip",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".mp4":  "video/mp4",
}

func fileType(name string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(name))
	if known := fileTypes[ext]; known != "" {
		return known
	}
	if byExt := mime.TypeByExtension(ext); byExt != "" {
		return byExt
	}
	return http.DetectContentType(data)
}

type sendFileTool struct {
	db        *store.DB
	bus       *events.Bus
	ownerID   uuid.UUID
	convID    uuid.UUID
	botID     uuid.UUID
	cycleID   uuid.UUID
	workspace *os.Root
}

// NewSendFileTool lets a bot hand files to the user: a message with the
// files attached appears in the conversation, ready to open or download.
// Files come from the bot's computer (workspace may be nil) or its Library.
func NewSendFileTool(db *store.DB, bus *events.Bus, ownerID, convID, botID, cycleID uuid.UUID, workspace *os.Root) Tool {
	return &sendFileTool{db: db, bus: bus, ownerID: ownerID, convID: convID, botID: botID, cycleID: cycleID, workspace: workspace}
}

func (t *sendFileTool) Name() string { return "send_file" }

func (t *sendFileTool) Description() string {
	return "把文件发给用户：在当前对话里发出一条带附件的消息，用户点一下就能打开或下载。" +
		"paths 填你电脑里的文件（/workspace/... 或工作区相对路径）；library_item_ids 填资料库里的资料（没有原文件的文档以 .md 发送）。" +
		"做好 Word/Excel/PPT/PDF/图片等成品、或用户要文件时，用它交付，不要只告诉用户文件路径——用户看不到你的电脑。" +
		"一次最多 10 个文件，单个不超过 25MB；目录请先打包成 zip。caption 是随文件发的一句说明，可省略。"
}

func (t *sendFileTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"paths": {"type": "array", "items": {"type": "string"}, "description": "电脑里的文件路径，例如 /workspace/output/开题报告.docx"},
			"library_item_ids": {"type": "array", "items": {"type": "string"}, "description": "资料库资料 id"},
			"caption": {"type": "string", "description": "随文件发送的一句说明，可省略"}
		}
	}`)
}

type outgoingFile struct {
	name     string
	mimeType string
	data     []byte
}

func (t *sendFileTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Path           string   `json:"path"`
		Paths          []string `json:"paths"`
		LibraryItemIDs []string `json:"library_item_ids"`
		Caption        string   `json:"caption"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	paths := input.Paths
	if strings.TrimSpace(input.Path) != "" {
		paths = append([]string{input.Path}, paths...)
	}
	if len(paths)+len(input.LibraryItemIDs) == 0 {
		return Result{Content: "请至少提供一个 paths 或 library_item_ids", IsError: true}, nil
	}
	if len(paths)+len(input.LibraryItemIDs) > maxSentFiles {
		return Result{Content: fmt.Sprintf("一次最多发送 %d 个文件；文件多时请先打包成 zip", maxSentFiles), IsError: true}, nil
	}

	files := make([]outgoingFile, 0, len(paths)+len(input.LibraryItemIDs))
	total := 0
	for _, raw := range paths {
		file, err := t.fromWorkspace(raw)
		if err != nil {
			return Result{Content: err.Error(), IsError: true}, nil
		}
		total += len(file.data)
		files = append(files, file)
	}
	for _, raw := range input.LibraryItemIDs {
		file, err := t.fromLibrary(ctx, raw)
		if err != nil {
			return Result{Content: err.Error(), IsError: true}, nil
		}
		total += len(file.data)
		files = append(files, file)
	}
	if total > maxSentTotalBytes {
		return Result{Content: "这些文件合计超过 50MB，请分几次发送或先压缩", IsError: true}, nil
	}

	message, sent, err := t.post(ctx, strings.TrimSpace(input.Caption), files)
	if err != nil {
		return Result{Content: "发送文件失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(map[string]any{
		"message_id": message.ID.String(),
		"sent":       sent,
		"note":       "文件已作为附件发到对话里，用户点击即可下载；回复里不用再贴路径或链接。",
	})
	return Result{Content: string(encoded)}, nil
}

func (t *sendFileTool) fromWorkspace(raw string) (outgoingFile, error) {
	if t.workspace == nil {
		return outgoingFile{}, fmt.Errorf("当前没有可用的电脑工作区，无法发送 %s", raw)
	}
	name := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	for _, prefix := range []string{"/workspace/", "workspace/"} {
		name = strings.TrimPrefix(name, prefix)
	}
	rel, err := cleanRel(name)
	if err != nil || rel == "." {
		return outgoingFile{}, fmt.Errorf("路径 %q 不在你的电脑工作区（/workspace）里", raw)
	}
	info, err := t.workspace.Stat(rel)
	if err != nil {
		return outgoingFile{}, fmt.Errorf("找不到文件 %s；先用 workspace_list 确认路径", raw)
	}
	if info.IsDir() {
		return outgoingFile{}, fmt.Errorf("%s 是目录；请先打包成 zip 再发送", raw)
	}
	if info.Size() == 0 {
		return outgoingFile{}, fmt.Errorf("%s 是空文件", raw)
	}
	if info.Size() > maxSentFileBytes {
		return outgoingFile{}, fmt.Errorf("%s 超过 25MB，请压缩或拆分后再发送", raw)
	}
	data, err := t.workspace.ReadFile(rel)
	if err != nil {
		return outgoingFile{}, fmt.Errorf("读取 %s 失败: %v", raw, err)
	}
	fileName := SafeUploadName(path.Base(rel))
	return outgoingFile{name: fileName, mimeType: fileType(fileName, data), data: data}, nil
}

func (t *sendFileTool) fromLibrary(ctx context.Context, raw string) (outgoingFile, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return outgoingFile{}, fmt.Errorf("资料 id %q 无效", raw)
	}
	item, err := t.db.GetLibraryItem(ctx, store.GetLibraryItemParams{ID: id, OwnerID: t.ownerID})
	if err != nil {
		return outgoingFile{}, fmt.Errorf("资料库里没有 id 为 %s 的资料", raw)
	}
	fileName := SafeUploadName(libindex.FileName(item))
	var data []byte
	if strings.TrimSpace(item.FilePath) != "" {
		info, statErr := os.Stat(item.FilePath)
		if statErr != nil {
			return outgoingFile{}, fmt.Errorf("「%s」的原文件已不在服务器上", item.Title)
		}
		if info.Size() > maxSentFileBytes {
			return outgoingFile{}, fmt.Errorf("「%s」超过 25MB，无法作为附件发送", item.Title)
		}
		if data, err = os.ReadFile(item.FilePath); err != nil {
			return outgoingFile{}, fmt.Errorf("读取「%s」失败: %v", item.Title, err)
		}
	} else {
		data = []byte(item.Content)
	}
	if len(data) == 0 {
		return outgoingFile{}, fmt.Errorf("「%s」没有可发送的内容", item.Title)
	}
	return outgoingFile{name: fileName, mimeType: fileType(fileName, data), data: data}, nil
}

type sentFile struct {
	ID        uuid.UUID `json:"id"`
	Filename  string    `json:"filename"`
	MimeType  string    `json:"mime_type"`
	SizeBytes int64     `json:"size_bytes"`
	URL       string    `json:"url"`
}

// post stores the message and its attachments in one transaction, so the
// user never sees a message whose files are missing.
func (t *sendFileTool) post(ctx context.Context, caption string, files []outgoingFile) (store.Message, []sentFile, error) {
	tx, err := t.db.Pool.Begin(ctx)
	if err != nil {
		return store.Message{}, nil, err
	}
	defer tx.Rollback(ctx)
	q := t.db.WithTx(tx)
	message, err := q.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: t.convID,
		CycleID:        &t.cycleID,
		SenderType:     "bot",
		SenderBotID:    &t.botID,
		Content:        caption,
		Metadata:       json.RawMessage(`{"kind":"files"}`),
	})
	if err != nil {
		return store.Message{}, nil, err
	}
	sent := make([]sentFile, 0, len(files))
	for _, file := range files {
		attachment, err := q.CreateAttachment(ctx, store.CreateAttachmentParams{
			OwnerID:   t.ownerID,
			Filename:  file.name,
			MimeType:  file.mimeType,
			SizeBytes: int64(len(file.data)),
			Content:   file.data,
		})
		if err != nil {
			return store.Message{}, nil, err
		}
		if err := q.AttachAttachmentToMessage(ctx, store.AttachAttachmentToMessageParams{
			ID: attachment.ID, OwnerID: t.ownerID, MessageID: &message.ID,
		}); err != nil {
			return store.Message{}, nil, err
		}
		sent = append(sent, sentFile{
			ID: attachment.ID, Filename: attachment.Filename, MimeType: attachment.MimeType,
			SizeBytes: attachment.SizeBytes, URL: "/api/attachments/" + attachment.ID.String(),
		})
	}
	// The live event carries the attachments too, so the files show up
	// without a reload.
	payload := map[string]any{}
	if encoded, err := json.Marshal(message); err == nil {
		_ = json.Unmarshal(encoded, &payload)
	}
	payload["attachments"] = sent
	if _, err := events.Append(ctx, q, t.convID, &t.cycleID, nil, events.TypeMessageCreated, map[string]any{"message": payload}); err != nil {
		return store.Message{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.Message{}, nil, err
	}
	// The message is persisted; a missed notification is recovered by the
	// next SSE poll tick.
	_ = t.bus.Notify(ctx, t.convID)
	return message, sent, nil
}
