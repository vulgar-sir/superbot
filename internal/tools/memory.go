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

// MemoryContext is who a turn's memory tools act for.
type MemoryContext struct {
	OwnerID uuid.UUID
	BotID   uuid.UUID
	TeamID  *uuid.UUID
	// UserID is the person the bot is talking with: the user of a direct
	// conversation, or in a group the sender of the message being answered.
	// Nil when no person is known (routines, old conversations). User scope
	// only ever reaches this person's own memories.
	UserID *uuid.UUID
	// Judge decides, on save, whether similar memories are restated or
	// outdated by the new one. Nil keeps every similar memory.
	Judge memory.ConflictJudge
}

// scopes are the memory scopes the tools may read and change, in recall order.
func (c MemoryContext) scopes() []memory.Scope {
	scopes := []memory.Scope{{Type: "bot", ID: c.BotID}}
	if c.UserID != nil {
		scopes = append(scopes, memory.Scope{Type: "user", ID: *c.UserID})
	}
	if c.TeamID != nil {
		scopes = append(scopes, memory.Scope{Type: "team", ID: *c.TeamID})
	}
	return scopes
}

// scope resolves a requested scope name; empty picks the default: the
// person's own scope when there is a person, else the bot's.
func (c MemoryContext) scope(requested string) (memory.Scope, error) {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "":
		if c.UserID != nil {
			return memory.Scope{Type: "user", ID: *c.UserID}, nil
		}
		return memory.Scope{Type: "bot", ID: c.BotID}, nil
	case "bot":
		return memory.Scope{Type: "bot", ID: c.BotID}, nil
	case "user":
		if c.UserID == nil {
			return memory.Scope{}, errors.New("scope=user is not available here: no person is known for this conversation; use scope=bot")
		}
		return memory.Scope{Type: "user", ID: *c.UserID}, nil
	case "team":
		if c.TeamID == nil {
			return memory.Scope{}, errors.New("scope=team is only available in a group conversation")
		}
		return memory.Scope{Type: "team", ID: *c.TeamID}, nil
	}
	return memory.Scope{}, fmt.Errorf("unknown scope %q (use user, bot or team)", requested)
}

type MemorySaveTool struct {
	memory *memory.Service
	ctx    MemoryContext
}

func NewMemorySaveTool(service *memory.Service, mc MemoryContext) *MemorySaveTool {
	return &MemorySaveTool{memory: service, ctx: mc}
}

func (t *MemorySaveTool) Name() string { return "memory_save" }
func (t *MemorySaveTool) Description() string {
	return "Save a durable fact or preference to long-term memory. Scopes: " +
		"user = facts about the person you are talking with (preferences, background, family); private to that person and recalled for them with every bot (the default when a person is known). " +
		"bot = knowledge for your own work that is not about a particular person; shared with everyone who uses you. " +
		"team = agreements every member of this group should know (group conversations only). " +
		"If the new fact updates an older memory, just save it: outdated memories are retired automatically. " +
		"Set pinned=true only for facts the user asked to always keep. Never save passwords, API keys or tokens: they are refused."
}
func (t *MemorySaveTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"content":{"type":"string"},"scope":{"type":"string","enum":["user","bot","team"]},"pinned":{"type":"boolean","description":"Protect this memory from automatic tidy and memory_forget."}},"required":["content"]}`)
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
	if memory.LooksLikeSecret(content) {
		return Result{Content: memory.ErrSecretContent.Error(), IsError: true}, nil
	}
	scope, err := t.ctx.scope(args.Scope)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	saved, err := t.memory.SaveWith(ctx, memory.SaveInput{
		OwnerID: t.ctx.OwnerID, ScopeType: scope.Type, ScopeID: scope.ID,
		Content: content, Source: memory.SourceTool, Pinned: args.Pinned,
		Judge: t.ctx.Judge,
	})
	if errors.Is(err, memory.ErrSecretContent) {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("save memory: %w", err)
	}
	return Result{Content: saveReport(scope.Type, saved)}, nil
}

func saveMessage(scopeType string, duplicate bool) string {
	if duplicate {
		return "已存在，已更新 (" + scopeType + ")"
	}
	return "memory saved (" + scopeType + ")"
}

// saveReport tells the model exactly what happened, including a save that
// is stored but not yet searchable by meaning.
func saveReport(scopeType string, saved memory.SaveResult) string {
	lines := []string{saveMessage(scopeType, saved.Duplicate)}
	if saved.Pending {
		lines = append(lines, "The memory is stored, but the embedding service is unavailable right now: until it is indexed automatically, only keyword search finds it. Do not save it again.")
	}
	if len(saved.Superseded) > 0 {
		lines = append(lines, "Retired as outdated (archived; the user can restore them):")
		for _, item := range saved.Superseded {
			lines = append(lines, memoryLine(item))
		}
	}
	if len(saved.PinnedConflicts) > 0 {
		lines = append(lines, "These pinned memories look outdated by the new one but only the user can retire them; mention it to the user:")
		for _, item := range saved.PinnedConflicts {
			lines = append(lines, memoryLine(item))
		}
	}
	return strings.Join(lines, "\n")
}

type MemorySearchTool struct {
	memory *memory.Service
	ctx    MemoryContext
}

func NewMemorySearchTool(service *memory.Service, mc MemoryContext) *MemorySearchTool {
	return &MemorySearchTool{memory: service, ctx: mc}
}

func (t *MemorySearchTool) Name() string { return "memory_search" }
func (t *MemorySearchTool) Description() string {
	return "Search durable memories for relevant facts and preferences: your bot memory, this group's team memory, and the memories of the person you are talking with. Each line ends with the memory id, which memory_forget accepts."
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
	items := t.memory.Recall(ctx, t.ctx.OwnerID, t.ctx.BotID, t.ctx.TeamID, t.ctx.UserID, query, 8)
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
	memory *memory.Service
	ctx    MemoryContext
}

func NewMemoryForgetTool(service *memory.Service, mc MemoryContext) *MemoryForgetTool {
	return &MemoryForgetTool{memory: service, ctx: mc}
}

func (t *MemoryForgetTool) Name() string { return "memory_forget" }
func (t *MemoryForgetTool) Description() string {
	return "Forget a memory that is wrong or outdated: your bot memory, this group's team memory, or a memory of the person you are talking with. Pass its id (from memory_search) or its exact content. The memory is archived, not deleted, and the user can restore it. Pinned memories cannot be forgotten."
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
	item, err := t.memory.Forget(ctx, t.ctx.OwnerID, t.ctx.scopes(), memoryID, args.Content)
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
