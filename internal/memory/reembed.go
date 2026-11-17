package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"superbot/internal/store"
)

const (
	// reembedBatch is how many rows one background pass reads at a time.
	reembedBatch = 50
	// reembedSkipLimit bounds the per-process list of rows the provider
	// refused, so a bad row cannot pin the head of the queue forever.
	reembedSkipLimit = 1000
)

// ReembedReport describes one re-embed pass.
type ReembedReport struct {
	Embedded int
	// Skipped rows were refused by the provider (not an outage); they are
	// left out of later passes of this process.
	Skipped int
	// Remaining is how many rows still lack a current vector, or -1 when it
	// was not counted.
	Remaining int64
}

// reembedState remembers rows the provider refused.
type reembedState struct {
	mu   sync.Mutex
	skip []uuid.UUID
}

func (r *reembedState) skipped() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uuid.UUID{}, r.skip...)
}

func (r *reembedState) add(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.skip) >= reembedSkipLimit {
		r.skip = r.skip[1:]
	}
	r.skip = append(r.skip, id)
}

// ReembedPending gives memories without a vector in the current space one:
// rows saved while the embedding service was down, rows edited since, and
// rows from an older space (another model, or E5 vectors made before the
// input prefixes). Active rows go first, newest first. A pass stops after
// maxRows rows, when budget is used up, or at the first sign of an outage:
// the circuit breaker then spaces out the next attempts.
func (s *Service) ReembedPending(ctx context.Context, maxRows int, budget time.Duration) (ReembedReport, error) {
	report := ReembedReport{Remaining: -1}
	if s.Configured() && !s.breaker.Allow() {
		return report, ErrEmbeddingUnavailable
	}
	deadline := s.now().Add(budget)
	state := &s.reembed
	for report.Embedded+report.Skipped < maxRows {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if budget > 0 && s.now().After(deadline) {
			break
		}
		limit := min(reembedBatch, maxRows-report.Embedded-report.Skipped)
		rows, err := s.db.ListMemoriesNeedingReembed(ctx, store.ListMemoriesNeedingReembedParams{
			EmbeddingModel: s.ModelName(), ExcludeIds: state.skipped(), MaxRows: int32(limit),
		})
		if err != nil {
			return report, err
		}
		if len(rows) == 0 {
			report.Remaining = 0
			return report, nil
		}
		for _, row := range rows {
			if budget > 0 && s.now().After(deadline) {
				break
			}
			vector, err := s.embedVector(ctx, row.Content, kindPassage, backgroundEmbedTimeout)
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, ErrEmbeddingUnavailable) || isOutage(err) {
					return report, err
				}
				state.add(row.ID)
				report.Skipped++
				continue
			}
			// A row edited while its vector was computed keeps waiting for
			// the vector of its new text.
			if _, err := s.db.SetMemoryEmbeddingForContent(ctx, store.SetMemoryEmbeddingForContentParams{
				ID: row.ID, OwnerID: row.OwnerID, Content: row.Content,
				Embedding: vectorLiteral(vector), EmbeddingModel: s.ModelName(),
			}); err != nil {
				return report, err
			}
			report.Embedded++
		}
	}
	if count, err := s.db.CountMemoriesNeedingReembed(ctx, s.ModelName()); err == nil {
		report.Remaining = count
	}
	return report, nil
}
