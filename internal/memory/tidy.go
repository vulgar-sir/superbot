package memory

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	"superbot/internal/store"
)

const (
	// The near-duplicate threshold is the space's TidyMin: Tidy acts without
	// anyone watching, so it is at least as strict as the write-time merge.
	tidyMaxFraction = 0.2
	// TidyMinRows is the scope size above which the scheduled tidy bothers.
	TidyMinRows   = 20
	tidyMaxScopes = 50
)

// TidyArchived describes one memory Tidy put away and the copy it kept.
type TidyArchived struct {
	ID         uuid.UUID `json:"id"`
	Content    string    `json:"content"`
	KeptID     uuid.UUID `json:"kept_id"`
	Reason     string    `json:"reason"`
	Similarity float64   `json:"similarity"`
}

type TidyReport struct {
	ScopeType string         `json:"scope_type"`
	ScopeID   uuid.UUID      `json:"scope_id"`
	Scanned   int            `json:"scanned"`
	Archived  []TidyArchived `json:"archived"`
}

// tidyRow is the slice of a memory the duplicate planner needs.
type tidyRow struct {
	ID        uuid.UUID
	Content   string
	Pinned    bool
	UpdatedAt time.Time
	Model     string
	Vector    []float32
}

const (
	tidyReasonExact = "duplicate"
	tidyReasonNear  = "near_duplicate"
)

// planTidy decides which rows to archive. Within a group of duplicates the
// keeper is a pinned row if there is one, otherwise the newest; pinned rows
// are never archived, and at most maxFraction of the rows (minimum one) are
// archived per run so a bad similarity signal cannot empty a scope.
func planTidy(rows []tidyRow, similarity func(a, b tidyRow) float64, threshold, maxFraction float64) []TidyArchived {
	if len(rows) < 2 {
		return nil
	}
	budget := max(int(float64(len(rows))*maxFraction), 1)
	ordered := append([]tidyRow(nil), rows...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Pinned != ordered[j].Pinned {
			return ordered[i].Pinned
		}
		return ordered[i].UpdatedAt.After(ordered[j].UpdatedAt)
	})
	keys := make([]string, len(ordered))
	for i, row := range ordered {
		keys[i] = NormalizeContent(row.Content)
	}
	archived := make([]bool, len(ordered))
	var plan []TidyArchived
	for i := range ordered {
		if archived[i] {
			continue
		}
		for j := i + 1; j < len(ordered); j++ {
			if archived[j] || ordered[j].Pinned {
				continue
			}
			entry := TidyArchived{ID: ordered[j].ID, Content: ordered[j].Content, KeptID: ordered[i].ID}
			switch {
			case keys[i] != "" && keys[i] == keys[j]:
				entry.Reason, entry.Similarity = tidyReasonExact, 1
			default:
				score := similarity(ordered[i], ordered[j])
				if score < threshold {
					continue
				}
				entry.Reason, entry.Similarity = tidyReasonNear, score
			}
			archived[j] = true
			plan = append(plan, entry)
			if len(plan) >= budget {
				return plan
			}
		}
	}
	return plan
}

// vectorSimilarity is the cosine similarity of two stored (unit length)
// vectors; rows from different embedding spaces are never comparable.
func vectorSimilarity(a, b tidyRow) float64 {
	if a.Model != b.Model || len(a.Vector) == 0 || len(a.Vector) != len(b.Vector) {
		return 0
	}
	var dot float64
	for i := range a.Vector {
		dot += float64(a.Vector[i]) * float64(b.Vector[i])
	}
	return dot
}

// tidySimilarityOf is the vector similarity of two rows, except that rows
// mentioning different numbers are never near duplicates ("端口 2222" vs
// "端口 2200" embed almost identically).
func tidySimilarityOf(a, b tidyRow) float64 {
	if !sameNumbers(a.Content, b.Content) {
		return 0
	}
	return vectorSimilarity(a, b)
}

func tidyRows(items []store.Memory) []tidyRow {
	rows := make([]tidyRow, 0, len(items))
	for _, item := range items {
		row := tidyRow{
			ID: item.ID, Content: item.Content, Pinned: item.Pinned,
			UpdatedAt: item.UpdatedAt, Model: item.EmbeddingModel,
		}
		if item.Embedding != nil {
			row.Vector = item.Embedding.Slice()
		}
		rows = append(rows, row)
	}
	return rows
}

// Tidy merges exact and near duplicates in one scope without calling a
// model. Nothing is deleted: the losers are soft-archived and can be brought
// back with Restore.
func (s *Service) Tidy(ctx context.Context, ownerID uuid.UUID, scopeType string, scopeID uuid.UUID) (TidyReport, error) {
	report := TidyReport{ScopeType: scopeType, ScopeID: scopeID, Archived: []TidyArchived{}}
	items, err := s.db.ListMemories(ctx, store.ListMemoriesParams{
		OwnerID: ownerID, ScopeType: scopeType, ScopeID: scopeID, Limit: scopeScanLimit,
	})
	if err != nil {
		return report, err
	}
	report.Scanned = len(items)
	plan := planTidy(tidyRows(items), tidySimilarityOf, s.space.TidyMin, tidyMaxFraction)
	if len(plan) == 0 {
		return report, nil
	}
	ids := make([]uuid.UUID, 0, len(plan))
	for _, entry := range plan {
		ids = append(ids, entry.ID)
	}
	done, err := s.db.ArchiveUnpinnedMemories(ctx, store.ArchiveUnpinnedMemoriesParams{OwnerID: ownerID, Ids: ids})
	if err != nil {
		return report, err
	}
	// A row pinned between the scan and the update is skipped by the query.
	applied := make(map[uuid.UUID]bool, len(done))
	for _, id := range done {
		applied[id] = true
	}
	for _, entry := range plan {
		if applied[entry.ID] {
			report.Archived = append(report.Archived, entry)
		}
	}
	return report, nil
}

// TidyDue tidies every scope large enough to be worth it and returns the
// reports of the scopes where something was archived.
func (s *Service) TidyDue(ctx context.Context) ([]TidyReport, error) {
	scopes, err := s.db.ListMemoryScopesForTidy(ctx, store.ListMemoryScopesForTidyParams{
		MinRows: TidyMinRows, MaxScopes: tidyMaxScopes,
	})
	if err != nil {
		return nil, err
	}
	var reports []TidyReport
	for _, scope := range scopes {
		if err := ctx.Err(); err != nil {
			return reports, err
		}
		report, err := s.Tidy(ctx, scope.OwnerID, scope.ScopeType, scope.ScopeID)
		if err != nil {
			return reports, err
		}
		if len(report.Archived) > 0 {
			reports = append(reports, report)
		}
	}
	return reports, nil
}
