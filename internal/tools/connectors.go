package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"superbot/internal/connectors"
)

type githubAPITool struct {
	connectors *connectors.Service
	ownerID    uuid.UUID
	client     *http.Client
}

func NewGitHubAPITool(service *connectors.Service, ownerID uuid.UUID) Tool {
	return &githubAPITool{connectors: service, ownerID: ownerID, client: &http.Client{Timeout: 30 * time.Second}}
}

func (t *githubAPITool) Name() string { return "github_api" }
func (t *githubAPITool) Description() string {
	return "调用用户已连接的 GitHub API。适合读仓库、Issue、PR、创建评论等；path 以 / 开头，method 为 GET/POST/PATCH/DELETE。"
}
func (t *githubAPITool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"method":{"type":"string","enum":["GET","POST","PATCH","DELETE"]},"path":{"type":"string"},"body":{"type":"object"}},"required":["method","path"]}`)
}
func (t *githubAPITool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	input.Method = strings.ToUpper(strings.TrimSpace(input.Method))
	if input.Method == "" {
		input.Method = "GET"
	}
	if !strings.HasPrefix(input.Path, "/") {
		return Result{Content: "path must start with /", IsError: true}, nil
	}
	connector, err := t.connectors.ByProvider(ctx, t.ownerID, "github")
	if err != nil {
		return Result{Content: "尚未连接 GitHub。请先在扩展页连接。", IsError: true}, nil
	}
	token, err := t.connectors.AccessToken(connector)
	if err != nil {
		return Result{Content: "GitHub token 不可用: " + err.Error(), IsError: true}, nil
	}
	var body io.Reader
	if len(input.Body) > 0 && string(input.Body) != "null" {
		body = bytes.NewReader(input.Body)
	}
	req, err := http.NewRequestWithContext(ctx, input.Method, "https://api.github.com"+input.Path, body)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return Result{Content: "GitHub 请求失败: " + err.Error(), IsError: true}, nil
	}
	defer resp.Body.Close()
	content, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	text := strings.TrimSpace(string(content))
	if text == "" {
		text = "(empty)"
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{Content: fmt.Sprintf("GitHub HTTP %d: %s", resp.StatusCode, text), IsError: true}, nil
	}
	return Result{Content: text}, nil
}

type slackPostTool struct {
	connectors *connectors.Service
	ownerID    uuid.UUID
	client     *http.Client
}

func NewSlackPostTool(service *connectors.Service, ownerID uuid.UUID) Tool {
	return &slackPostTool{connectors: service, ownerID: ownerID, client: &http.Client{Timeout: 30 * time.Second}}
}
func (t *slackPostTool) Name() string { return "slack_post_message" }
func (t *slackPostTool) Description() string {
	return "向用户已连接的 Slack 频道发送消息。"
}
func (t *slackPostTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"channel":{"type":"string"},"text":{"type":"string"}},"required":["channel","text"]}`)
}
func (t *slackPostTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Channel string `json:"channel"`
		Text    string `json:"text"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	connector, err := t.connectors.ByProvider(ctx, t.ownerID, "slack")
	if err != nil {
		return Result{Content: "尚未连接 Slack。", IsError: true}, nil
	}
	token, err := t.connectors.AccessToken(connector)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	payload, _ := json.Marshal(map[string]string{"channel": input.Channel, "text": input.Text})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/chat.postMessage", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := t.client.Do(req)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return Result{Content: string(body), IsError: resp.StatusCode >= 300}, nil
}

type notionSearchTool struct {
	connectors *connectors.Service
	ownerID    uuid.UUID
	client     *http.Client
}

func NewNotionSearchTool(service *connectors.Service, ownerID uuid.UUID) Tool {
	return &notionSearchTool{connectors: service, ownerID: ownerID, client: &http.Client{Timeout: 30 * time.Second}}
}
func (t *notionSearchTool) Name() string { return "notion_search" }
func (t *notionSearchTool) Description() string {
	return "搜索用户已连接的 Notion 工作区。"
}
func (t *notionSearchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)
}
func (t *notionSearchTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	connector, err := t.connectors.ByProvider(ctx, t.ownerID, "notion")
	if err != nil {
		return Result{Content: "尚未连接 Notion。", IsError: true}, nil
	}
	token, err := t.connectors.AccessToken(connector)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	payload, _ := json.Marshal(map[string]any{"query": input.Query, "page_size": 20})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.notion.com/v1/search", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", "2022-06-28")
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	return Result{Content: string(body), IsError: resp.StatusCode >= 300}, nil
}
