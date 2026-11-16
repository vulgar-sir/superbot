package tools

import (
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

type githubReviewTool struct {
	connectors *connectors.Service
	ownerID    uuid.UUID
	client     *http.Client
}

func NewGitHubReviewTool(service *connectors.Service, ownerID uuid.UUID) Tool {
	return &githubReviewTool{
		connectors: service,
		ownerID:    ownerID,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *githubReviewTool) Name() string { return "review_pull_request" }
func (t *githubReviewTool) Description() string {
	return "读取一个 GitHub Pull Request 的标题、描述、变更文件与 patch，供你审查。审查完成后可用 github_api POST /repos/{owner}/{repo}/pulls/{number}/reviews 提交 COMMENT/APPROVE/REQUEST_CHANGES。"
}
func (t *githubReviewTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string"},"repo":{"type":"string"},"number":{"type":"integer"}},"required":["owner","repo","number"]}`)
}

func (t *githubReviewTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Owner  string `json:"owner"`
		Repo   string `json:"repo"`
		Number int    `json:"number"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	input.Owner = strings.TrimSpace(input.Owner)
	input.Repo = strings.TrimSpace(input.Repo)
	if input.Owner == "" || input.Repo == "" || input.Number <= 0 {
		return Result{Content: "owner, repo and number are required", IsError: true}, nil
	}
	connector, err := t.connectors.ByProvider(ctx, t.ownerID, "github")
	if err != nil {
		return Result{Content: "尚未连接 GitHub。请先在扩展页连接。", IsError: true}, nil
	}
	token, err := t.connectors.AccessToken(connector)
	if err != nil {
		return Result{Content: "GitHub token 不可用: " + err.Error(), IsError: true}, nil
	}

	var pr struct {
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
		Merged bool   `json:"merged"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		Head struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Additions int `json:"additions"`
		Deletions int `json:"deletions"`
		Changed   int `json:"changed_files"`
	}
	if err := t.githubJSON(ctx, token, fmt.Sprintf("/repos/%s/%s/pulls/%d", input.Owner, input.Repo, input.Number), &pr); err != nil {
		return Result{Content: "读取 PR 失败: " + err.Error(), IsError: true}, nil
	}

	var files []struct {
		Filename  string `json:"filename"`
		Status    string `json:"status"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
		Patch     string `json:"patch"`
	}
	if err := t.githubJSON(ctx, token, fmt.Sprintf("/repos/%s/%s/pulls/%d/files?per_page=100", input.Owner, input.Repo, input.Number), &files); err != nil {
		return Result{Content: "读取 PR 文件失败: " + err.Error(), IsError: true}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "PR #%d: %s\nstate=%s merged=%t author=@%s base=%s head=%s +%d -%d files=%d\n\n",
		input.Number, pr.Title, pr.State, pr.Merged, pr.User.Login, pr.Base.Ref, pr.Head.Ref,
		pr.Additions, pr.Deletions, pr.Changed)
	if body := strings.TrimSpace(pr.Body); body != "" {
		b.WriteString("描述：\n")
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	b.WriteString("变更文件：\n")
	budget := 120 * 1024
	for _, file := range files {
		header := fmt.Sprintf("\n--- %s (%s, +%d -%d)\n", file.Filename, file.Status, file.Additions, file.Deletions)
		if b.Len()+len(header) >= budget {
			b.WriteString("\n…变更内容过长，已截断。\n")
			break
		}
		b.WriteString(header)
		patch := file.Patch
		if len(patch) > budget-b.Len() {
			patch = patch[:max(0, budget-b.Len())] + "\n…patch 已截断"
		}
		b.WriteString(patch)
		b.WriteString("\n")
	}
	return Result{Content: b.String()}, nil
}

func (t *githubReviewTool) githubJSON(ctx context.Context, token, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	content, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GitHub HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(content)))
	}
	return json.Unmarshal(content, target)
}
