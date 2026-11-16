package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/connectors"
	"superbot/internal/mailbox"
)

type sendEmailTool struct {
	connectors *connectors.Service
	mailbox    *mailbox.Service
	ownerID    uuid.UUID
}

func NewSendEmailTool(connectorsService *connectors.Service, mailboxService *mailbox.Service, ownerID uuid.UUID) Tool {
	return &sendEmailTool{connectors: connectorsService, mailbox: mailboxService, ownerID: ownerID}
}

func (t *sendEmailTool) Name() string { return "send_email" }
func (t *sendEmailTool) Description() string {
	return "通过用户已连接的邮箱发送邮件。需要先在扩展页连接 Email（SMTP）。"
}
func (t *sendEmailTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"to":{"type":"string"},"subject":{"type":"string"},"body":{"type":"string"},"cc":{"type":"string"}},"required":["to","subject","body"]}`)
}

func (t *sendEmailTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		To      string `json:"to"`
		CC      string `json:"cc"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	connector, err := t.connectors.ByProvider(ctx, t.ownerID, "email")
	if err != nil {
		return Result{Content: "尚未连接邮箱。请先在扩展页连接 Email（SMTP）。", IsError: true}, nil
	}
	password, err := t.connectors.AccessToken(connector)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	cfg, err := mailbox.ParseSMTPConfig(connector.Config)
	if err != nil {
		return Result{Content: "邮箱连接器配置无效: " + err.Error(), IsError: true}, nil
	}
	to := splitAddresses(input.To)
	cc := splitAddresses(input.CC)
	if len(to) == 0 {
		return Result{Content: "收件人为空", IsError: true}, nil
	}
	if err := t.mailbox.Send(ctx, cfg, password, mailbox.Outgoing{
		To: to, CC: cc, Subject: input.Subject, Body: input.Body,
	}); err != nil {
		return Result{Content: "发送邮件失败: " + err.Error(), IsError: true}, nil
	}
	return Result{Content: "邮件已发送给 " + strings.Join(to, ", ")}, nil
}

func splitAddresses(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); strings.Contains(value, "@") {
			out = append(out, value)
		}
	}
	return out
}
