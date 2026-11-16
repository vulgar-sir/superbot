package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"superbot/internal/secrets"
	"superbot/internal/store"
)

type Result struct {
	Title     string
	URL       string
	Snippet   string
	SiteName  string
	Published string
}

type Service struct {
	db     *store.DB
	box    *secrets.Box
	logger *slog.Logger
	client *http.Client
}

func NewService(db *store.DB, box *secrets.Box, logger *slog.Logger) *Service {
	return &Service{
		db:     db,
		box:    box,
		logger: logger,
		client: &http.Client{Timeout: 25 * time.Second},
	}
}

func (s *Service) Search(ctx context.Context, ownerID uuid.UUID, query string, count int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if count <= 0 {
		count = 5
	}
	if count > 10 {
		count = 10
	}
	providers, err := s.db.ListProvidersByType(ctx, store.ListProvidersByTypeParams{
		OwnerID: ownerID,
		Type:    "search",
	})
	if err != nil {
		return nil, fmt.Errorf("list search providers: %w", err)
	}
	var provider *store.Provider
	for i := range providers {
		if providers[i].Enabled {
			provider = &providers[i]
			break
		}
	}
	if provider == nil {
		return nil, errors.New("未配置搜索 Provider。请在 设置 → 新增 Provider 中添加 type=search 的 Bocha 或 Tavily")
	}
	apiKey := ""
	if len(provider.ApiKeyEnc) > 0 {
		apiKey, err = s.box.DecryptString(provider.ApiKeyEnc)
		if err != nil {
			return nil, fmt.Errorf("decrypt search provider key: %w", err)
		}
	}
	switch provider.Kind {
	case "bocha":
		return s.searchBocha(ctx, *provider, apiKey, query, count)
	case "tavily":
		return s.searchTavily(ctx, *provider, apiKey, query, count)
	default:
		return nil, fmt.Errorf("unsupported search provider kind %q", provider.Kind)
	}
}

func (s *Service) searchBocha(ctx context.Context, provider store.Provider, apiKey, query string, count int) ([]Result, error) {
	if apiKey == "" {
		return nil, errors.New("Bocha search provider has no api key")
	}
	endpoint := strings.TrimSpace(provider.BaseUrl)
	if endpoint == "" {
		endpoint = "https://api.bochaai.com/v1/web-search"
	} else if !strings.Contains(endpoint, "/web-search") {
		endpoint = strings.TrimRight(endpoint, "/") + "/v1/web-search"
	}
	payload := map[string]any{
		"query":   query,
		"count":   count,
		"summary": true,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bocha request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("bocha returned HTTP %d: %s", resp.StatusCode, truncate(string(raw), 500))
	}
	var decoded struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			WebPages struct {
				Value []struct {
					Name          string `json:"name"`
					URL           string `json:"url"`
					Snippet       string `json:"snippet"`
					Summary       string `json:"summary"`
					SiteName      string `json:"siteName"`
					DatePublished string `json:"datePublished"`
				} `json:"value"`
			} `json:"webPages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode bocha response: %w", err)
	}
	if decoded.Code != 0 && decoded.Code != 200 {
		return nil, fmt.Errorf("bocha error %d: %s", decoded.Code, decoded.Msg)
	}
	out := make([]Result, 0, len(decoded.Data.WebPages.Value))
	for _, item := range decoded.Data.WebPages.Value {
		snippet := item.Snippet
		if item.Summary != "" {
			snippet = item.Summary
		}
		out = append(out, Result{
			Title:     item.Name,
			URL:       item.URL,
			Snippet:   truncate(snippet, 800),
			SiteName:  item.SiteName,
			Published: item.DatePublished,
		})
	}
	return out, nil
}

func (s *Service) searchTavily(ctx context.Context, provider store.Provider, apiKey, query string, count int) ([]Result, error) {
	if apiKey == "" {
		return nil, errors.New("Tavily search provider has no api key")
	}
	endpoint := strings.TrimSpace(provider.BaseUrl)
	if endpoint == "" {
		endpoint = "https://api.tavily.com/search"
	} else if !strings.HasSuffix(strings.TrimRight(endpoint, "/"), "/search") {
		endpoint = strings.TrimRight(endpoint, "/") + "/search"
	}
	payload := map[string]any{
		"api_key":        apiKey,
		"query":          query,
		"max_results":    count,
		"include_answer": false,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tavily returned HTTP %d: %s", resp.StatusCode, truncate(string(raw), 500))
	}
	var decoded struct {
		Results []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode tavily response: %w", err)
	}
	out := make([]Result, 0, len(decoded.Results))
	for _, item := range decoded.Results {
		out = append(out, Result{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: truncate(item.Content, 800),
		})
	}
	return out, nil
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
