package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/fetch"
	"superbot/internal/search"
)

type FetchTool struct {
	fetcher *fetch.Fetcher
}

func NewFetchTool(fetcher *fetch.Fetcher) *FetchTool {
	return &FetchTool{fetcher: fetcher}
}

func (t *FetchTool) Name() string { return "fetch_url" }
func (t *FetchTool) Description() string {
	return "Fetch a public web page and return its main article as Markdown. Private, loopback and cloud-metadata addresses are blocked (SSRF protection)."
}
func (t *FetchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"},"max_chars":{"type":"integer","description":"default 12000, max 40000"}},"required":["url"]}`)
}

type fetchArgs struct {
	URL      string `json:"url"`
	MaxChars int    `json:"max_chars"`
}

func (t *FetchTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args fetchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	result, err := t.fetcher.Fetch(ctx, args.URL, args.MaxChars)
	if err != nil {
		return Result{}, err
	}
	var b strings.Builder
	if result.Title != "" {
		fmt.Fprintf(&b, "# %s\n", result.Title)
	}
	fmt.Fprintf(&b, "URL: %s\n", result.URL)
	if result.SiteName != "" {
		fmt.Fprintf(&b, "Site: %s\n", result.SiteName)
	}
	if result.Byline != "" {
		fmt.Fprintf(&b, "Author: %s\n", result.Byline)
	}
	b.WriteString("\n---\n\n")
	b.WriteString(result.Content)
	return Result{Content: b.String()}, nil
}

type SearchTool struct {
	service *search.Service
	ownerID uuid.UUID
}

func NewSearchTool(service *search.Service, ownerID uuid.UUID) *SearchTool {
	return &SearchTool{service: service, ownerID: ownerID}
}

func (t *SearchTool) Name() string { return "web_search" }
func (t *SearchTool) Description() string {
	return "Search the web through the configured search provider (Bocha or Tavily) and return titles, URLs and snippets. Use fetch_url to read a promising result."
}
func (t *SearchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"count":{"type":"integer","description":"default 5, max 10"}},"required":["query"]}`)
}

type webSearchArgs struct {
	Query string `json:"query"`
	Count int    `json:"count"`
}

func (t *SearchTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args webSearchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	results, err := t.service.Search(ctx, t.ownerID, args.Query, args.Count)
	if err != nil {
		return Result{}, err
	}
	if len(results) == 0 {
		return Result{Content: "no results"}, nil
	}
	var b strings.Builder
	for i, item := range results {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item.Title)
		fmt.Fprintf(&b, "   %s\n", item.URL)
		if item.SiteName != "" || item.Published != "" {
			fmt.Fprintf(&b, "   %s %s\n", item.SiteName, item.Published)
		}
		if item.Snippet != "" {
			fmt.Fprintf(&b, "   %s\n", item.Snippet)
		}
		b.WriteString("\n")
	}
	return Result{Content: strings.TrimSpace(b.String())}, nil
}
