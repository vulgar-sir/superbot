package memory

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"superbot/internal/store"
)

const (
	// duplicateSimilarity is the cosine similarity above which a new memory is
	// treated as a restatement of an existing one in the same scope.
	duplicateSimilarity = 0.92
	maxContentRunes     = 4000
	scopeScanLimit      = 500

	SourceManual = "manual"
	SourceTool   = "tool"
	SourceReview = "review"
	SourceImport = "import"
	SourceTidy   = "tidy"
)

var (
	ErrNotFound = errors.New("memory not found")
	// ErrPinned protects memories the user pinned from being forgotten by a bot.
	ErrPinned = errors.New("memory is pinned; only the user can remove it")
)

// Scope addresses one memory namespace: a bot, a team or a user.
type Scope struct {
	Type string
	ID   uuid.UUID
}

type SaveInput struct {
	OwnerID   uuid.UUID
	ScopeType string
	ScopeID   uuid.UUID
	Content   string
	Metadata  json.RawMessage
	// Source is one of the Source* constants; empty means manual.
	Source string
	Pinned bool
	// SourceConversationID and SourceOrdinal record where a reviewed memory
	// came from; SourceOrdinal 0 means unknown.
	SourceConversationID *uuid.UUID
	SourceOrdinal        int64
}

type SaveResult struct {
	Memory store.Memory
	// Duplicate is true when an existing memory was refreshed instead of a
	// new row being inserted.
	Duplicate bool
}

const trailingPunctuation = ".。．!！?？,，;；:：、~～…"

// NormalizeContent is the comparison key for duplicate detection: trimmed,
// whitespace collapsed, ASCII lowercased, trailing punctuation removed.
func NormalizeContent(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	space := false
	for _, r := range strings.TrimSpace(text) {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return strings.TrimRightFunc(b.String(), func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(trailingPunctuation, r)
	})
}

// findNormalizedDuplicate returns the first row whose normalized text equals
// that of content. Rows are expected newest first, so the newest copy wins.
func findNormalizedDuplicate(content string, rows []store.Memory) (store.Memory, bool) {
	key := NormalizeContent(content)
	if key == "" {
		return store.Memory{}, false
	}
	for _, row := range rows {
		if NormalizeContent(row.Content) == key {
			return row, true
		}
	}
	return store.Memory{}, false
}

func validSource(source string) bool {
	switch source {
	case SourceManual, SourceTool, SourceReview, SourceImport, SourceTidy:
		return true
	}
	return false
}

func validateContent(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", errors.New("content is required")
	}
	if len([]rune(content)) > maxContentRunes {
		return "", errors.New("memory content is too long (max 4000 chars)")
	}
	if LooksLikeSecret(content) {
		return "", ErrSecretContent
	}
	return content, nil
}

// SaveWith stores a memory unless the scope already holds the same fact, in
// which case the existing row is refreshed. Secrets are refused.
func (s *Service) SaveWith(ctx context.Context, in SaveInput) (SaveResult, error) {
	content, err := validateContent(in.Content)
	if err != nil {
		return SaveResult{}, err
	}
	if in.Source == "" {
		in.Source = SourceManual
	}
	if !validSource(in.Source) {
		return SaveResult{}, errors.New("invalid memory source")
	}
	if len(in.Metadata) == 0 {
		in.Metadata = json.RawMessage(`{}`)
	}
	// An embedding outage must not lose the memory: the row is still written
	// and the error is reported, exactly as before dedupe existed.
	vector, embedErr := s.EmbedText(ctx, content)

	if existing, found := s.findDuplicate(ctx, in, content, vector); found {
		// A reviewed restatement only confirms the fact is still current; it
		// must not overwrite wording the user or the bot chose deliberately.
		next := content
		if in.Source == SourceReview {
			next = existing.Content
		}
		item, err := s.db.RefreshMemory(ctx, store.RefreshMemoryParams{
			ID: existing.ID, OwnerID: in.OwnerID, Content: next, Pinned: in.Pinned,
		})
		if err != nil {
			return SaveResult{}, err
		}
		item.Embedding = nil
		if next != existing.Content || existing.EmbeddingModel != s.ModelName() {
			if embedErr != nil {
				return SaveResult{Memory: item, Duplicate: true}, embedErr
			}
			if next != content {
				if vector, err = s.EmbedText(ctx, next); err != nil {
					return SaveResult{Memory: item, Duplicate: true}, err
				}
			}
			if err := s.db.SetMemoryEmbedding(ctx, store.SetMemoryEmbeddingParams{
				ID: item.ID, OwnerID: in.OwnerID, Embedding: vector, EmbeddingModel: s.ModelName(),
			}); err != nil {
				return SaveResult{Memory: item, Duplicate: true}, err
			}
		}
		return SaveResult{Memory: item, Duplicate: true}, nil
	}

	item, err := s.db.CreateMemory(ctx, store.CreateMemoryParams{
		OwnerID: in.OwnerID, ScopeType: in.ScopeType, ScopeID: in.ScopeID,
		Content: content, Metadata: in.Metadata, Source: in.Source, Pinned: in.Pinned,
		SourceConversationID: in.SourceConversationID,
		SourceOrdinal:        pgtype.Int8{Int64: in.SourceOrdinal, Valid: in.SourceOrdinal > 0},
	})
	if err != nil {
		return SaveResult{}, err
	}
	item.Embedding = nil
	if embedErr != nil {
		return SaveResult{Memory: item}, embedErr
	}
	if err := s.db.SetMemoryEmbedding(ctx, store.SetMemoryEmbeddingParams{
		ID: item.ID, OwnerID: in.OwnerID, Embedding: vector, EmbeddingModel: s.ModelName(),
	}); err != nil {
		return SaveResult{Memory: item}, err
	}
	return SaveResult{Memory: item}, nil
}

// findDuplicate looks for an active memory in the same scope that says the
// same thing. Lookup failures count as "no duplicate": a missed dedupe is
// cleaned up later by Tidy, a refused save would lose the fact.
func (s *Service) findDuplicate(ctx context.Context, in SaveInput, content, vector string) (store.Memory, bool) {
	if vector != "" {
		rows, err := s.db.SearchMemoriesScored(ctx, store.SearchMemoriesScoredParams{
			OwnerID: in.OwnerID, ScopeType: in.ScopeType, ScopeID: in.ScopeID,
			Embedding: vector, EmbeddingModel: s.ModelName(),
			MinSimilarity: duplicateSimilarity, MaxResults: 1,
		})
		if err == nil && len(rows) > 0 {
			return rows[0].Memory, true
		}
	}
	// Rows embedded in another vector space (or not embedded yet) are only
	// reachable by text.
	rows, err := s.db.ListMemories(ctx, store.ListMemoriesParams{
		OwnerID: in.OwnerID, ScopeType: in.ScopeType, ScopeID: in.ScopeID, Limit: scopeScanLimit,
	})
	if err != nil {
		return store.Memory{}, false
	}
	return findNormalizedDuplicate(content, rows)
}

// GetInScope loads a memory (archived or not) and checks it belongs to scope.
func (s *Service) GetInScope(ctx context.Context, ownerID, memoryID uuid.UUID, scope Scope) (store.Memory, error) {
	item, err := s.db.GetMemory(ctx, store.GetMemoryParams{ID: memoryID, OwnerID: ownerID})
	if err != nil || item.ScopeType != scope.Type || item.ScopeID != scope.ID {
		return store.Memory{}, ErrNotFound
	}
	item.Embedding = nil
	return item, nil
}

// SetPinned marks a memory as protected from Tidy and memory_forget.
func (s *Service) SetPinned(ctx context.Context, ownerID, memoryID uuid.UUID, pinned bool) (store.Memory, error) {
	item, err := s.db.SetMemoryPinned(ctx, store.SetMemoryPinnedParams{ID: memoryID, OwnerID: ownerID, Pinned: pinned})
	if err != nil {
		return store.Memory{}, ErrNotFound
	}
	item.Embedding = nil
	return item, nil
}

// Restore un-archives a memory that Tidy or memory_forget put away.
func (s *Service) Restore(ctx context.Context, ownerID, memoryID uuid.UUID) (store.Memory, error) {
	item, err := s.db.RestoreMemory(ctx, store.RestoreMemoryParams{ID: memoryID, OwnerID: ownerID})
	if err != nil {
		return store.Memory{}, ErrNotFound
	}
	item.Embedding = nil
	return item, nil
}

// Forget soft-archives one memory, addressed by id or by its text, provided
// it lives in one of the caller's scopes. Pinned memories are left alone.
func (s *Service) Forget(ctx context.Context, ownerID uuid.UUID, scopes []Scope, memoryID uuid.UUID, content string) (store.Memory, error) {
	var target store.Memory
	found := false
	if memoryID != uuid.Nil {
		item, err := s.db.GetMemory(ctx, store.GetMemoryParams{ID: memoryID, OwnerID: ownerID})
		if err == nil && item.ArchivedAt == nil {
			for _, scope := range scopes {
				if item.ScopeType == scope.Type && item.ScopeID == scope.ID {
					target, found = item, true
					break
				}
			}
		}
	} else if strings.TrimSpace(content) != "" {
		for _, scope := range scopes {
			rows, err := s.db.ListMemories(ctx, store.ListMemoriesParams{
				OwnerID: ownerID, ScopeType: scope.Type, ScopeID: scope.ID, Limit: scopeScanLimit,
			})
			if err != nil {
				return store.Memory{}, err
			}
			if target, found = findNormalizedDuplicate(content, rows); found {
				break
			}
		}
	}
	if !found {
		return store.Memory{}, ErrNotFound
	}
	if target.Pinned {
		return store.Memory{}, ErrPinned
	}
	item, err := s.db.ArchiveMemory(ctx, store.ArchiveMemoryParams{ID: target.ID, OwnerID: ownerID})
	if err != nil {
		return store.Memory{}, ErrNotFound
	}
	item.Embedding = nil
	return item, nil
}
