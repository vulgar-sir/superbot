package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/connectors"
)

var connectorProviders = []string{
	"github", "slack", "notion", "onepassword", "email", "custom",
	"linear", "figma", "vercel", "sentry", "postgres", "atlassian",
	"google_docs", "google_drive", "canva", "onedrive", "msteams",
}

type connectorListTool struct {
	service *connectors.Service
	ownerID uuid.UUID
}

func NewConnectorListTool(service *connectors.Service, ownerID uuid.UUID) Tool {
	return &connectorListTool{service: service, ownerID: ownerID}
}

func (t *connectorListTool) Name() string { return "connector_list" }
func (t *connectorListTool) Description() string {
	return "列出用户已安装的连接器（Provider、名称、状态）。不会返回任何密钥。"
}
func (t *connectorListTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *connectorListTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	items, err := t.service.List(ctx, t.ownerID)
	if err != nil {
		return Result{Content: "读取连接器失败: " + err.Error(), IsError: true}, nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":       item.ID.String(),
			"provider": item.Provider,
			"name":     item.Name,
			"status":   item.Status,
		})
	}
	encoded, _ := json.Marshal(out)
	return Result{Content: string(encoded)}, nil
}

type connectorAddTool struct {
	service *connectors.Service
	ownerID uuid.UUID
}

func NewConnectorAddTool(service *connectors.Service, ownerID uuid.UUID) Tool {
	return &connectorAddTool{service: service, ownerID: ownerID}
}

func (t *connectorAddTool) Name() string { return "connector_add" }
func (t *connectorAddTool) Description() string {
	return "在用户明确提供凭据时，为用户添加连接器。支持 github/slack/notion/onepassword/email/linear/figma/vercel/sentry/postgres/atlassian/google_docs/google_drive/canva/onedrive/msteams/custom。凭据会加密保存；绝不要在回复中回显 token。"
}
func (t *connectorAddTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"provider":{"type":"string"},"name":{"type":"string"},"token":{"type":"string"},"config":{"type":"object"}},"required":["provider","token"]}`)
}
func (t *connectorAddTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Provider string          `json:"provider"`
		Name     string          `json:"name"`
		Token    string          `json:"token"`
		Config   json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	known := false
	for _, item := range connectorProviders {
		if item == provider {
			known = true
			break
		}
	}
	if !known {
		return Result{Content: "不支持的 Provider: " + provider, IsError: true}, nil
	}
	if strings.TrimSpace(input.Token) == "" {
		return Result{Content: "token 不能为空", IsError: true}, nil
	}
	if len(input.Config) == 0 {
		input.Config = json.RawMessage(`{}`)
	}
	connector, err := t.service.CreateToken(ctx, t.ownerID, connectors.Input{
		Provider: provider,
		Name:     strings.TrimSpace(input.Name),
		Token:    strings.TrimSpace(input.Token),
		Config:   input.Config,
	})
	if err != nil {
		return Result{Content: "添加连接器失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(map[string]any{
		"id":       connector.ID.String(),
		"provider": connector.Provider,
		"name":     connector.Name,
		"status":   connector.Status,
	})
	return Result{Content: "连接器已添加（凭据已加密保存，不会回显）。\n" + string(encoded)}, nil
}

type connectorTestTool struct {
	service *connectors.Service
	ownerID uuid.UUID
}

func NewConnectorTestTool(service *connectors.Service, ownerID uuid.UUID) Tool {
	return &connectorTestTool{service: service, ownerID: ownerID}
}

func (t *connectorTestTool) Name() string { return "connector_test" }
func (t *connectorTestTool) Description() string {
	return "测试已安装连接器是否可用。可以用 provider 或连接器 id 指定。"
}
func (t *connectorTestTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"provider":{"type":"string"},"id":{"type":"string"}}}`)
}
func (t *connectorTestTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Provider string `json:"provider"`
		ID       string `json:"id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	var connectorID uuid.UUID
	if strings.TrimSpace(input.ID) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(input.ID))
		if err != nil {
			return Result{Content: "id 格式不正确", IsError: true}, nil
		}
		connectorID = parsed
	} else if strings.TrimSpace(input.Provider) != "" {
		connector, err := t.service.ByProvider(ctx, t.ownerID, strings.ToLower(strings.TrimSpace(input.Provider)))
		if err != nil {
			return Result{Content: "尚未连接该 Provider", IsError: true}, nil
		}
		connectorID = connector.ID
	} else {
		return Result{Content: "需要 provider 或 id", IsError: true}, nil
	}
	identity, err := t.service.Test(ctx, t.ownerID, connectorID)
	if err != nil {
		return Result{Content: "连接测试失败: " + err.Error(), IsError: true}, nil
	}
	return Result{Content: fmt.Sprintf("连接正常: %s", identity)}, nil
}

type connectorRemoveTool struct {
	service *connectors.Service
	ownerID uuid.UUID
}

func NewConnectorRemoveTool(service *connectors.Service, ownerID uuid.UUID) Tool {
	return &connectorRemoveTool{service: service, ownerID: ownerID}
}

func (t *connectorRemoveTool) Name() string { return "connector_remove" }
func (t *connectorRemoveTool) Description() string {
	return "删除用户的一个连接器。可以用 provider 或连接器 id 指定；删除前应向用户确认。"
}
func (t *connectorRemoveTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"provider":{"type":"string"},"id":{"type":"string"}}}`)
}
func (t *connectorRemoveTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Provider string `json:"provider"`
		ID       string `json:"id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	var connectorID uuid.UUID
	if strings.TrimSpace(input.ID) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(input.ID))
		if err != nil {
			return Result{Content: "id 格式不正确", IsError: true}, nil
		}
		connectorID = parsed
	} else if strings.TrimSpace(input.Provider) != "" {
		connector, err := t.service.ByProvider(ctx, t.ownerID, strings.ToLower(strings.TrimSpace(input.Provider)))
		if err != nil {
			return Result{Content: "尚未连接该 Provider", IsError: true}, nil
		}
		connectorID = connector.ID
	} else {
		return Result{Content: "需要 provider 或 id", IsError: true}, nil
	}
	if err := t.service.Delete(ctx, t.ownerID, connectorID); err != nil {
		return Result{Content: "删除失败: " + err.Error(), IsError: true}, nil
	}
	return Result{Content: "连接器已删除。"}, nil
}
