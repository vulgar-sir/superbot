package memory

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"superbot/internal/store"
)

const (
	maxContentRunes = 4000
	scopeScanLimit  = 500

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
	// Supersedes are memories of the same scope the caller already knows the
	// new one replaces (the user picked them, or the reviewer said so). They
	// are archived with a pointer to the new memory.
	Supersedes []uuid.UUID
	// Judge, when set, decides how the new memory relates to similar ones
	// already in the scope. Without it only near-verbatim copies are merged
	// and similar memories are reported back in SaveResult.Related.
	Judge ConflictJudge
}

type SaveResult struct {
	Memory store.Memory
	// Duplicate is true when an existing memory was refreshed instead of a
	// new row being inserted.
	Duplicate bool
	// Pending is true when the memory was stored without a vector because
	// the embedding service was unavailable; the background re-embed adds it
	// and until then only keyword recall can find it.
	Pending bool
	// Superseded are the memories archived because the new one replaced them.
	Superseded []store.Memory
	// Related are similar memories of the scope that were left as they were;
	// a caller with a person in the loop can offer to retire them.
	Related []store.Memory
	// PinnedConflicts are memories the new one replaces that stay active
	// because the user pinned them.
	PinnedConflicts []store.Memory
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

// numberRuns lists the digit sequences of a text ("端口 2222，保留 14 天"
// gives 2222 and 14).
func numberRuns(text string) []string {
	var runs []string
	var current strings.Builder
	for _, r := range text {
		if r >= '0' && r <= '9' {
			current.WriteRune(r)
			continue
		}
		if current.Len() > 0 {
			runs = append(runs, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		runs = append(runs, current.String())
	}
	slices.Sort(runs)
	return slices.Compact(runs)
}

// sameNumbers reports whether two texts mention the same numbers. Two
// memories that differ in a number ("端口 2222" vs "端口 2200") are never
// merged without someone judging them, however similar their vectors are.
func sameNumbers(a, b string) bool {
	return slices.Equal(numberRuns(a), numberRuns(b))
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

// SaveWith stores a memory. A copy of a memory already in the scope only
// refreshes that memory; similar memories go to the judge (when the caller
// has one), which may mark them as restated or outdated by the new memory;
// outdated ones are archived, never deleted. Secrets are refused.
//
// An embedding outage does not fail the save: the row is written without a
// vector, SaveResult.Pending says so, and the background re-embed fills it in.
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
	// Every network call (embedding, judge) happens before the transaction,
	// so none of them holds the scope lock.
	literal := ""
	if vector, err := s.embedVector(ctx, content, kindPassage, saveEmbedTimeout); err == nil {
		literal = vectorLiteral(vector)
	}
	related := s.relatedTo(ctx, in, content, literal)
	decision := s.resolveConflicts(ctx, in, content, related)

	// The duplicate check and the write run under one lock per scope, so two
	// saves of the same fact at the same moment cannot both insert.
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return SaveResult{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, scopeLockKey(in)); err != nil {
		return SaveResult{}, err
	}
	result, err := s.saveLocked(ctx, s.db.WithTx(tx), in, content, literal, decision, related)
	if err != nil {
		return SaveResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SaveResult{}, err
	}
	return result, nil
}

func scopeLockKey(in SaveInput) string {
	return "memory:" + in.OwnerID.String() + ":" + in.ScopeType + ":" + in.ScopeID.String()
}

// relatedTo returns the memories of the scope similar enough to the new one
// to be a restatement or an older version of it, most similar first.
// Normalized-equal copies are left out: they are merged without asking.
// Lookup failures count as "nothing related"; the save must not fail on them.
func (s *Service) relatedTo(ctx context.Context, in SaveInput, content, literal string) []scoredMemory {
	if literal == "" || s.db == nil {
		return nil
	}
	rows, err := s.searchScored(ctx, s.db.Queries, in.OwnerID, Scope{Type: in.ScopeType, ID: in.ScopeID},
		literal, s.space.RelatedMin, conflictMaxItems+1)
	if err != nil {
		return nil
	}
	key := NormalizeContent(content)
	out := make([]scoredMemory, 0, len(rows))
	for _, row := range rows {
		if NormalizeContent(row.memory.Content) == key {
			continue
		}
		out = append(out, row)
		if len(out) >= conflictMaxItems {
			break
		}
	}
	return out
}

// resolveConflicts asks the judge about the related memories. Without a
// judge, or when it fails, only a near-verbatim copy that mentions the same
// numbers is treated as a restatement; everything else is kept.
func (s *Service) resolveConflicts(ctx context.Context, in SaveInput, content string, related []scoredMemory) ConflictDecision {
	if len(related) == 0 {
		return ConflictDecision{}
	}
	if in.Judge != nil {
		memories := make([]store.Memory, len(related))
		for i, item := range related {
			memories[i] = item.memory
		}
		if decision, err := in.Judge.JudgeConflict(ctx, content, memories); err == nil {
			return decision
		}
	}
	return mergeDecision(s.space, content, related)
}

// mergeDecision is the unattended rule: merge into the most similar memory
// only above the space's MergeMin and only when the numbers agree.
func mergeDecision(space embeddingSpace, content string, related []scoredMemory) ConflictDecision {
	if len(related) == 0 {
		return ConflictDecision{}
	}
	top := related[0]
	if top.similarity >= space.MergeMin && sameNumbers(content, top.memory.Content) {
		return ConflictDecision{DuplicateOf: top.memory.ID}
	}
	return ConflictDecision{}
}

// saveLocked writes inside the caller's transaction: refresh the copy or
// insert, then archive what the new memory replaces.
func (s *Service) saveLocked(ctx context.Context, q *store.Queries, in SaveInput, content, literal string, decision ConflictDecision, related []scoredMemory) (SaveResult, error) {
	scope := Scope{Type: in.ScopeType, ID: in.ScopeID}
	rows, err := q.ListMemories(ctx, store.ListMemoriesParams{
		OwnerID: in.OwnerID, ScopeType: in.ScopeType, ScopeID: in.ScopeID, Limit: scopeScanLimit,
	})
	if err != nil {
		// A missed dedupe is cleaned up later by Tidy; a refused save would
		// lose the fact.
		rows = nil
	}
	existing, found := findNormalizedDuplicate(content, rows)
	if !found && decision.DuplicateOf != uuid.Nil {
		// The judge's pick must still be active in this scope.
		for _, row := range rows {
			if row.ID == decision.DuplicateOf {
				existing, found = row, true
				break
			}
		}
	}

	var result SaveResult
	if found {
		result, err = s.refreshLocked(ctx, q, in, content, literal, existing)
	} else {
		result, err = s.insertLocked(ctx, q, in, content, literal)
	}
	if err != nil {
		return SaveResult{}, err
	}

	retire := make([]uuid.UUID, 0, len(decision.Supersedes)+len(in.Supersedes))
	for _, id := range slices.Concat(decision.Supersedes, in.Supersedes) {
		if id != result.Memory.ID && !slices.Contains(retire, id) {
			retire = append(retire, id)
		}
	}
	handled := map[uuid.UUID]bool{result.Memory.ID: true}
	for _, id := range retire {
		handled[id] = true
		row, err := q.SupersedeMemory(ctx, store.SupersedeMemoryParams{
			ID: id, OwnerID: in.OwnerID, ScopeType: scope.Type, ScopeID: scope.ID,
			SupersededBy: result.Memory.ID.String(), Reason: in.Source,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// Pinned, already archived, or not in this scope. Only the pinned
			// case is worth telling the caller about.
			if current, getErr := q.GetMemory(ctx, store.GetMemoryParams{ID: id, OwnerID: in.OwnerID}); getErr == nil &&
				current.Pinned && current.ArchivedAt == nil && current.ScopeType == scope.Type && current.ScopeID == scope.ID {
				current.Embedding = nil
				result.PinnedConflicts = append(result.PinnedConflicts, current)
			}
			continue
		}
		if err != nil {
			return SaveResult{}, err
		}
		row.Embedding = nil
		result.Superseded = append(result.Superseded, row)
	}
	for _, item := range related {
		if !handled[item.memory.ID] {
			result.Related = append(result.Related, item.memory)
		}
	}
	return result, nil
}

func (s *Service) refreshLocked(ctx context.Context, q *store.Queries, in SaveInput, content, literal string, existing store.Memory) (SaveResult, error) {
	// A reviewed restatement only confirms the fact is still current; it
	// must not overwrite wording the user or the bot chose deliberately, and
	// a bot never rewords what the user pinned.
	next := content
	if in.Source == SourceReview || (existing.Pinned && in.Source != SourceManual) {
		next = existing.Content
	}
	item, err := q.RefreshMemory(ctx, store.RefreshMemoryParams{
		ID: existing.ID, OwnerID: in.OwnerID, Content: next, Pinned: in.Pinned,
	})
	if err != nil {
		return SaveResult{}, err
	}
	result := SaveResult{Duplicate: true}
	upToDate := next == existing.Content && existing.Embedding != nil && existing.EmbeddingModel == s.ModelName()
	switch {
	case upToDate:
	case next == content && literal != "":
		if err := q.SetMemoryEmbedding(ctx, store.SetMemoryEmbeddingParams{
			ID: item.ID, OwnerID: in.OwnerID, Embedding: literal, EmbeddingModel: s.ModelName(),
		}); err != nil {
			return SaveResult{}, err
		}
		item.EmbeddingModel = s.ModelName()
	default:
		// No vector for the text that stays; the background re-embed adds it.
		result.Pending = true
	}
	item.Embedding = nil
	result.Memory = item
	return result, nil
}

func (s *Service) insertLocked(ctx context.Context, q *store.Queries, in SaveInput, content, literal string) (SaveResult, error) {
	item, err := q.CreateMemory(ctx, store.CreateMemoryParams{
		OwnerID: in.OwnerID, ScopeType: in.ScopeType, ScopeID: in.ScopeID,
		Content: content, Metadata: in.Metadata, Source: in.Source, Pinned: in.Pinned,
		SourceConversationID: in.SourceConversationID,
		SourceOrdinal:        pgtype.Int8{Int64: in.SourceOrdinal, Valid: in.SourceOrdinal > 0},
	})
	if err != nil {
		return SaveResult{}, err
	}
	item.Embedding = nil
	if literal == "" {
		return SaveResult{Memory: item, Pending: true}, nil
	}
	if err := q.SetMemoryEmbedding(ctx, store.SetMemoryEmbeddingParams{
		ID: item.ID, OwnerID: in.OwnerID, Embedding: literal, EmbeddingModel: s.ModelName(),
	}); err != nil {
		return SaveResult{}, err
	}
	item.EmbeddingModel = s.ModelName()
	return SaveResult{Memory: item}, nil
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

// Restore un-archives a memory that Tidy, memory_forget or a newer memory put
// away.
func (s *Service) Restore(ctx context.Context, ownerID, memoryID uuid.UUID) (store.Memory, error) {
	item, err := s.db.RestoreMemory(ctx, store.RestoreMemoryParams{ID: memoryID, OwnerID: ownerID})
	if err != nil {
		return store.Memory{}, ErrNotFound
	}
	item.Embedding = nil
	return item, nil
}

// ArchiveInScope archives a memory on the user's request, pinned or not.
// With supersededBy (a memory of the same scope) it is recorded as replaced
// by that memory.
func (s *Service) ArchiveInScope(ctx context.Context, ownerID, memoryID uuid.UUID, scope Scope, supersededBy uuid.UUID) (store.Memory, error) {
	if _, err := s.GetInScope(ctx, ownerID, memoryID, scope); err != nil {
		return store.Memory{}, err
	}
	if supersededBy != uuid.Nil {
		if supersededBy == memoryID {
			return store.Memory{}, errors.New("a memory cannot replace itself")
		}
		if _, err := s.GetInScope(ctx, ownerID, supersededBy, scope); err != nil {
			return store.Memory{}, err
		}
		item, err := s.db.SupersedeMemory(ctx, store.SupersedeMemoryParams{
			ID: memoryID, OwnerID: ownerID, ScopeType: scope.Type, ScopeID: scope.ID,
			SupersededBy: supersededBy.String(), Reason: SourceManual, AllowPinned: true,
		})
		if err != nil {
			return store.Memory{}, ErrNotFound
		}
		item.Embedding = nil
		return item, nil
	}
	item, err := s.db.ArchiveMemory(ctx, store.ArchiveMemoryParams{ID: memoryID, OwnerID: ownerID})
	if err != nil {
		return store.Memory{}, ErrNotFound
	}
	item.Embedding = nil
	return item, nil
}

// Supersede archives a memory of scope as replaced by another memory. Pinned
// memories are left alone (ErrPinned).
func (s *Service) Supersede(ctx context.Context, ownerID, memoryID uuid.UUID, scope Scope, by uuid.UUID, reason string) (store.Memory, error) {
	item, err := s.db.SupersedeMemory(ctx, store.SupersedeMemoryParams{
		ID: memoryID, OwnerID: ownerID, ScopeType: scope.Type, ScopeID: scope.ID,
		SupersededBy: by.String(), Reason: reason,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if current, getErr := s.GetInScope(ctx, ownerID, memoryID, scope); getErr == nil && current.Pinned && current.ArchivedAt == nil {
			return store.Memory{}, ErrPinned
		}
		return store.Memory{}, ErrNotFound
	}
	if err != nil {
		return store.Memory{}, err
	}
	item.Embedding = nil
	return item, nil
}

// ExistingForReview picks the memories a review of texts should see: first
// the ones the texts are about, then the most recently confirmed ones of each
// scope, at most limit in all.
func (s *Service) ExistingForReview(ctx context.Context, ownerID uuid.UUID, scopes []Scope, texts []string, limit int) []store.Memory {
	if len(scopes) == 0 || limit <= 0 {
		return nil
	}
	out := make([]store.Memory, 0, limit)
	seen := make(map[uuid.UUID]bool)
	add := func(item store.Memory) {
		if len(out) < limit && !seen[item.ID] {
			seen[item.ID] = true
			item.Embedding = nil
			out = append(out, item)
		}
	}
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			continue
		}
		vector, err := s.embedVector(ctx, text, kindQuery, backgroundEmbedTimeout)
		if err != nil {
			break
		}
		literal, threshold := vectorLiteral(vector), s.recallThreshold(vector)
		for _, scope := range scopes {
			hits, err := s.searchScored(ctx, s.db.Queries, ownerID, scope, literal, threshold, 3)
			if err != nil {
				continue
			}
			for _, hit := range hits {
				add(hit.memory)
			}
		}
	}
	perScope := max(limit/len(scopes), 5)
	for _, scope := range scopes {
		rows, err := s.db.ListRecentMemories(ctx, store.ListRecentMemoriesParams{
			OwnerID: ownerID, ScopeType: scope.Type, ScopeID: scope.ID, Limit: int32(perScope),
		})
		if err != nil {
			continue
		}
		for _, row := range rows {
			add(row)
		}
	}
	return out
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
