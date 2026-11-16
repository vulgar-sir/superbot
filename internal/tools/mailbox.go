package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"superbot/internal/connectors"
	"superbot/internal/mailbox"
)

type readEmailTool struct {
	connectors *connectors.Service
	mailbox    *mailbox.Service
	ownerID    uuid.UUID
}

func NewReadEmailTool(connectorsService *connectors.Service, mailboxService *mailbox.Service, ownerID uuid.UUID) Tool {
	return &readEmailTool{connectors: connectorsService, mailbox: mailboxService, ownerID: ownerID}
}

func (t *readEmailTool) Name() string { return "read_email" }
func (t *readEmailTool) Description() string {
	return "读取已连接邮箱的最近收件（IMAP）。需要先在扩展页连接 Email（IMAP/SMTP）并配置 host/username。"
}
func (t *readEmailTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer"}}}`)
}
func (t *readEmailTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Limit int `json:"limit"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	connector, err := t.connectors.ByProvider(ctx, t.ownerID, "email")
	if err != nil {
		return Result{Content: "尚未连接邮箱。请先在扩展页连接 Email（IMAP/SMTP）。", IsError: true}, nil
	}
	password, err := t.connectors.AccessToken(connector)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	cfg, err := mailbox.ParseConfig(connector.Config)
	if err != nil {
		return Result{Content: "邮箱配置无效: " + err.Error(), IsError: true}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	messages, err := t.mailbox.Recent(ctx, cfg, password, input.Limit)
	if err != nil {
		return Result{Content: "读取收件箱失败: " + err.Error(), IsError: true}, nil
	}
	encoded, err := json.Marshal(map[string]any{
		"messages": messages,
		"threads":  mailbox.GroupThreads(messages),
	})
	if err != nil {
		return Result{Content: "编码收件箱失败: " + err.Error(), IsError: true}, nil
	}
	return Result{Content: string(encoded)}, nil
}
