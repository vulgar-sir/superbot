package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/memory"
	"superbot/internal/store"
)

type MemorySaveTool struct {
	memory  *memory.Service
	ownerID uuid.UUID
	botID   uuid.UUID
	teamID  *uuid.UUID
}

func NewMemorySaveTool(service *memory.Service, ownerID, botID uuid.UUID, teamID *uuid.UUID) *MemorySaveTool {
	return &MemorySaveTool{memory: service, ownerID: ownerID, botID: botID, teamID: teamID}
}

func (t *MemorySaveTool) Name() string { return "memory_save" }
func (t *MemorySaveTool) Description() string {
	return "Save a durable fact or preference to long-term memory. Use scope=team for knowledge that all teammates should see, otherwise scope=bot (default). Set pinned=true only for facts the user asked to always keep. Never save passwords, API keys or tokens: they are refused."
}
func (t *MemorySaveTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"content":{"type":"string"},"scope":{"type":"string","enum":["bot","team"]},"pinned":{"type":"boolean","description":"Protect this memory from automatic tidy and memory_forget."}},"required":["content"]}`)
}

type memorySaveArgs struct {
	Content string `json:"content"`
	Scope   string `json:"scope"`
	Pinned  bool   `json:"pinned"`
}

func (t *MemorySaveTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args memorySaveArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	content := strings.TrimSpace(args.Content)
	if content == "" {
		return Result{}, fmt.Errorf("content is required")
	}
	if len([]rune(content)) > 4000 {
		return Result{}, fmt.Errorf("memory content is too long (max 4000 chars)")
	}
	scopeType := "bot"
	scopeID := t.botID
	if strings.EqualFold(args.Scope, "team") && t.teamID != nil {
		scopeType = "team"
		scopeID = *t.teamID
	}
	if memory.LooksLikeSecret(content) {
		return Result{Content: memory.ErrSecretContent.Error(), IsError: true}, nil
	}
	saved, err := t.memory.SaveWith(ctx, memory.SaveInput{
		OwnerID: t.ownerID, ScopeType: scopeType, ScopeID: scopeID,
		Content: content, Source: memory.SourceTool, Pinned: args.Pinned,
	})
	if err != nil {
		return Result{}, fmt.Errorf("save memory: %w", err)
	}
	return Result{Content: saveMessage(scopeType, saved.Duplicate)}, nil
}

func saveMessage(scopeType string, duplicate bool) string {
	if duplicate {
		return "已存在，已更新 (" + scopeType + ")"
	}
	return "memory saved (" + scopeType + ")"
}

type MemorySearchTool struct {
	memory  *memory.Service
	ownerID uuid.UUID
	botID   uuid.UUID
	teamID  *uuid.UUID
}

func NewMemorySearchTool(service *memory.Service, ownerID, botID uuid.UUID, teamID *uuid.UUID) *MemorySearchTool {
	return &MemorySearchTool{memory: service, ownerID: ownerID, botID: botID, teamID: teamID}
}

func (t *MemorySearchTool) Name() string { return "memory_search" }
func (t *MemorySearchTool) Description() string {
	return "Search durable memories for relevant facts and preferences. Searches both the bot-scoped and team-scoped memory. Each line ends with the memory id, which memory_forget accepts."
}
func (t *MemorySearchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)
}

func (t *MemorySearchTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return Result{}, fmt.Errorf("query is required")
	}
	items := t.memory.Recall(ctx, t.ownerID, t.botID, t.teamID, nil, query, 8)
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, memoryLine(item))
	}
	if len(lines) == 0 {
		return Result{Content: "no relevant memories"}, nil
	}
	if len(lines) > 8 {
		lines = lines[:8]
	}
	return Result{Content: strings.Join(lines, "\n")}, nil
}

func memoryLine(item store.Memory) string {
	pinned := ""
	if item.Pinned {
		pinned = " [pinned]"
	}
	return fmt.Sprintf("- [%s]%s %s (id: %s)", item.ScopeType, pinned, strings.TrimSpace(item.Content), item.ID)
}

type MemoryForgetTool struct {
	memory  *memory.Service
	ownerID uuid.UUID
	botID   uuid.UUID
	teamID  *uuid.UUID
}

func NewMemoryForgetTool(service *memory.Service, ownerID, botID uuid.UUID, teamID *uuid.UUID) *MemoryForgetTool {
	return &MemoryForgetTool{memory: service, ownerID: ownerID, botID: botID, teamID: teamID}
}

func (t *MemoryForgetTool) Name() string { return "memory_forget" }
func (t *MemoryForgetTool) Description() string {
	return "Forget a memory that is wrong or outdated. Pass its id (from memory_search) or its exact content. The memory is archived, not deleted, and the user can restore it. Pinned memories cannot be forgotten."
}
func (t *MemoryForgetTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Memory id as shown by memory_search."},"content":{"type":"string","description":"Exact text of the memory, used when no id is given."}}}`)
}

func (t *MemoryForgetTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	memoryID := uuid.Nil
	if id := strings.TrimSpace(args.ID); id != "" {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return Result{}, fmt.Errorf("id is not a valid memory id")
		}
		memoryID = parsed
	} else if strings.TrimSpace(args.Content) == "" {
		return Result{}, fmt.Errorf("id or content is required")
	}
	scopes := []memory.Scope{{Type: "bot", ID: t.botID}}
	if t.teamID != nil {
		scopes = append(scopes, memory.Scope{Type: "team", ID: *t.teamID})
	}
	item, err := t.memory.Forget(ctx, t.ownerID, scopes, memoryID, args.Content)
	switch {
	case errors.Is(err, memory.ErrNotFound):
		return Result{Content: "no matching memory found", IsError: true}, nil
	case errors.Is(err, memory.ErrPinned):
		return Result{Content: "this memory is pinned by the user and cannot be forgotten", IsError: true}, nil
	case err != nil:
		return Result{}, fmt.Errorf("forget memory: %w", err)
	}
	return Result{Content: "memory forgotten (" + item.ScopeType + "): " + strings.TrimSpace(item.Content)}, nil
}
