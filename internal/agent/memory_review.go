package agent

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"superbot/internal/llm"
	"superbot/internal/memory"
	"superbot/internal/store"
)

const (
	memoryReviewEvery   = time.Minute
	memoryTidyEvery     = 24 * time.Hour
	memoryReviewTimeout = 10 * time.Minute
	memoryReviewTokens  = 800
	// One maintenance pass re-embeds at most this many memories within this
	// time; a self-hosted E5 takes ~50 ms per memory, so a backlog of a few
	// thousand rows clears within minutes without starving recall.
	memoryReembedRows   = 500
	memoryReembedBudget = 45 * time.Second
)

// memoryMaintenance is the per-engine state of the background memory pass.
type memoryMaintenance struct {
	mu         sync.Mutex
	running    bool
	lastReview time.Time
	lastTidy   time.Time
	reviewer   *memory.Reviewer
	// embeddingDown remembers that the last re-embed pass hit an outage, so
	// the outage is logged once.
	embeddingDown bool
}

// Keyed by engine rather than stored on it so this feature stays out of the
// Engine struct; an engine lives as long as its process.
var memoryMaintenanceByEngine sync.Map

// reviewMemories starts the automatic memory review (at most once a minute)
// and the duplicate tidy (at most once a day) in the background. It never
// blocks the worker loop and never fails: memory upkeep is best effort.
func (e *Engine) reviewMemories(ctx context.Context) {
	if e.memory == nil || e.db == nil {
		return
	}
	value, _ := memoryMaintenanceByEngine.LoadOrStore(e, &memoryMaintenance{})
	state := value.(*memoryMaintenance)

	state.mu.Lock()
	if state.running || time.Since(state.lastReview) < memoryReviewEvery {
		state.mu.Unlock()
		return
	}
	state.running = true
	state.lastReview = time.Now()
	tidy := time.Since(state.lastTidy) >= memoryTidyEvery
	if tidy {
		state.lastTidy = time.Now()
	}
	if state.reviewer == nil {
		state.reviewer = memory.NewReviewer(e.db, e.memory, &memoryExtractor{engine: e}, e.logger)
	}
	reviewer := state.reviewer
	state.mu.Unlock()

	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				e.logger.Error("memory maintenance panicked", "panic", recovered, "stack", string(debug.Stack()))
			}
			state.mu.Lock()
			state.running = false
			state.mu.Unlock()
		}()
		runCtx, cancel := context.WithTimeout(ctx, memoryReviewTimeout)
		defer cancel()
		e.reembedMemories(runCtx, state)
		if reviewed, err := reviewer.ReviewDue(runCtx); err != nil {
			if runCtx.Err() == nil {
				e.logger.Warn("memory review failed", "error", err)
			}
		} else if reviewed > 0 {
			e.logger.Info("memory review finished", "conversations", reviewed)
		}
		if !tidy {
			return
		}
		reports, err := e.memory.TidyDue(runCtx)
		if err != nil && runCtx.Err() == nil {
			e.logger.Warn("memory tidy failed", "error", err)
		}
		for _, report := range reports {
			e.logger.Info("memory tidy archived duplicates",
				"scope_type", report.ScopeType, "scope_id", report.ScopeID, "archived", len(report.Archived))
		}
	}()
}

// reembedMemories gives memories without a current vector one: rows saved
// while the embedding service was down, rows edited since, and rows from an
// older embedding space. While the service is down the circuit breaker
// spaces out the attempts (30 s doubling to 10 min) and a pass costs nothing.
func (e *Engine) reembedMemories(ctx context.Context, state *memoryMaintenance) {
	if err := e.memory.WarmCalibration(ctx); err != nil && ctx.Err() == nil && !errors.Is(err, memory.ErrEmbeddingUnavailable) {
		e.logger.Warn("memory recall calibration failed", "error", err)
	}
	report, err := e.memory.ReembedPending(ctx, memoryReembedRows, memoryReembedBudget)
	state.mu.Lock()
	wasDown := state.embeddingDown
	state.embeddingDown = err != nil && ctx.Err() == nil
	state.mu.Unlock()
	switch {
	case err != nil && ctx.Err() == nil:
		// Logged when the outage starts, not on every pass.
		if !wasDown {
			e.logger.Warn("memory re-embed paused: embedding service unavailable", "error", err)
		}
	case wasDown && err == nil:
		e.logger.Info("memory re-embed resumed")
	}
	if report.Embedded > 0 || report.Skipped > 0 {
		e.logger.Info("re-embedded memories", "count", report.Embedded, "skipped", report.Skipped,
			"remaining", report.Remaining, "space", e.memory.ModelName())
	}
}

// memoryExtractor runs the review prompt on the bot's own provider and model
// and records the usage against the conversation's creator.
type memoryExtractor struct {
	engine *Engine
}

func (x *memoryExtractor) Extract(ctx context.Context, req memory.ExtractRequest) (string, error) {
	e := x.engine
	bot, err := e.db.GetBot(ctx, store.GetBotParams{ID: req.BotID, OwnerID: req.OwnerID})
	if err != nil {
		return "", fmt.Errorf("load bot: %w", err)
	}
	if !bot.MemoryEnabled {
		return "", errors.New("memory is disabled for this bot")
	}
	if bot.ProviderID == nil {
		return "", fmt.Errorf("bot %q has no provider configured", bot.Name)
	}
	providerRow, err := e.db.GetProvider(ctx, store.GetProviderParams{ID: *bot.ProviderID, OwnerID: req.UserID})
	if errors.Is(err, pgx.ErrNoRows) && req.OwnerID != req.UserID {
		providerRow, err = e.db.GetProvider(ctx, store.GetProviderParams{ID: *bot.ProviderID, OwnerID: req.OwnerID})
	}
	if err != nil {
		return "", fmt.Errorf("load provider: %w", err)
	}
	if !providerRow.Enabled {
		return "", fmt.Errorf("provider %q is disabled", providerRow.Name)
	}
	provider, err := e.providers.Build(providerRow, e.cfg.LLMTimeout)
	if err != nil {
		return "", err
	}
	var modelRow *store.Model
	if row, err := e.db.GetModelByName(ctx, store.GetModelByNameParams{ProviderID: providerRow.ID, Name: bot.Model}); err == nil {
		modelRow = &row
	}
	// The review spends the same money as a turn, so it answers to the same
	// limits: the account's weekly budget and the conversation's budget.
	if err := e.checkWeeklyBudget(ctx, req.OwnerID); err != nil {
		return "", err
	}

	request := llm.Request{
		Model: bot.Model,
		Messages: []llm.Message{
			llm.TextMessage(llm.RoleSystem, req.System),
			llm.TextMessage(llm.RoleUser, req.User),
		},
		MaxTokens: memoryReviewTokens,
		JSONMode:  true,
	}
	reserved := estimateRequestCost(modelRow, request)
	if reserved > 0 {
		n, err := e.db.ReserveConversationBudget(ctx, store.ReserveConversationBudgetParams{ID: req.ConversationID, ReservedMicros: reserved})
		if err != nil {
			return "", fmt.Errorf("reserve conversation budget: %w", err)
		}
		if n == 0 {
			return "", errors.New("会话预算不足，跳过记忆回顾")
		}
	}
	settled := false
	defer func() {
		if !settled && reserved > 0 {
			_ = e.db.ReleaseConversationReservation(context.WithoutCancel(ctx), store.ReleaseConversationReservationParams{ID: req.ConversationID, ReservedMicros: reserved})
		}
	}()
	stream, err := provider.Stream(ctx, request)
	if err != nil {
		return "", err
	}
	defer stream.Close()
	var text strings.Builder
	var usage *llm.Usage
	var streamErr error
	for event := range stream.Events() {
		switch event.Type {
		case llm.EventTextDelta:
			text.WriteString(event.Text)
		case llm.EventUsage:
			usage = event.Usage
		case llm.EventError:
			streamErr = event.Err
		}
	}
	if streamErr != nil {
		return "", streamErr
	}
	estimated := usage == nil
	if estimated {
		usage = &llm.Usage{
			PromptTokens:     EstimateMessagesTokens(request.Messages),
			CompletionTokens: EstimateTokens(text.String()),
		}
	}
	cost := computeCost(modelRow, usage)
	if err := e.db.SettleConversationUsage(ctx, store.SettleConversationUsageParams{
		ID: req.ConversationID, ReservedMicros: reserved, SpentMicros: cost,
	}); err != nil {
		e.logger.Warn("settle memory review cost failed", "conversation_id", req.ConversationID, "error", err)
	}
	settled = true
	if _, err := e.db.CreateUsageRecord(ctx, store.CreateUsageRecordParams{
		UserID:           req.UserID,
		BotID:            &bot.ID,
		ProviderID:       &providerRow.ID,
		Model:            bot.Model,
		PromptTokens:     int64(usage.PromptTokens),
		CachedTokens:     int64(usage.CachedTokens),
		CompletionTokens: int64(usage.CompletionTokens),
		CostMicros:       cost,
		Estimated:        estimated,
	}); err != nil {
		e.logger.Warn("record memory review usage failed", "conversation_id", req.ConversationID, "error", err)
	}
	return text.String(), nil
}
