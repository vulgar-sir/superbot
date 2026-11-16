package jobs

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"superbot/internal/agent"
	"superbot/internal/store"
)

// Worker claims one cycle at a time and hands it to the agent engine. The
// database lease (not an in-memory lock) is what prevents two workers from
// running the same cycle.
type Worker struct {
	db           *store.DB
	engine       *agent.Engine
	logger       *slog.Logger
	pollInterval time.Duration
	concurrency  int
}

func NewWorker(db *store.DB, engine *agent.Engine, logger *slog.Logger) *Worker {
	concurrency := 4
	if raw := os.Getenv("SUPERBOT_WORKER_CONCURRENCY"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			concurrency = parsed
		}
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 16 {
		concurrency = 16
	}
	return &Worker{
		db:           db,
		engine:       engine,
		logger:       logger,
		pollInterval: time.Second,
		concurrency:  concurrency,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	w.logger.Info("worker started", "concurrency", w.concurrency)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, w.concurrency)
	for slot := 0; slot < w.concurrency; slot++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			if err := w.runLoop(runCtx, slot); err != nil && runCtx.Err() == nil {
				errCh <- err
				cancel()
			}
		}(slot)
	}
	wg.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return ctx.Err()
	}
}

func (w *Worker) runLoop(ctx context.Context, slot int) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		cycle, err := w.db.ClaimCycle(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			if slot == 0 {
				w.engine.Housekeeping(ctx)
			}
			if !w.sleep(ctx, slot) {
				return ctx.Err()
			}
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.logger.Error("claim cycle failed", "worker_slot", slot, "error", err)
			if !w.sleep(ctx, slot) {
				return ctx.Err()
			}
			continue
		}
		if cycle.LeaseID == nil {
			w.logger.Error("claimed cycle has no lease", "cycle_id", cycle.ID)
			continue
		}
		w.logger.Info("processing cycle", "cycle_id", cycle.ID, "conversation_id", cycle.ConversationID, "attempt", cycle.Attempt, "worker_slot", slot)
		if err := w.engine.ProcessCycle(ctx, cycle, *cycle.LeaseID); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.logger.Error("process cycle failed", "cycle_id", cycle.ID, "error", err)
		}
	}
}

func (w *Worker) sleep(ctx context.Context, slot int) bool {
	delay := w.pollInterval + time.Duration(slot%5)*75*time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
