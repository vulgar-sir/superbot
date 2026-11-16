package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"superbot/internal/store"
)

type usageTotalsDTO struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CachedTokens     int64 `json:"cached_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	CostMicros       int64 `json:"cost_micros"`
}

type usageByBotDTO struct {
	BotID            *uuid.UUID `json:"bot_id"`
	BotName          string     `json:"bot_name"`
	PromptTokens     int64      `json:"prompt_tokens"`
	CompletionTokens int64      `json:"completion_tokens"`
	CostMicros       int64      `json:"cost_micros"`
}

type usageByModelDTO struct {
	Model            string `json:"model"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	CostMicros       int64  `json:"cost_micros"`
}

type usageDailyDTO struct {
	Day        string `json:"day"`
	Tokens     int64  `json:"tokens"`
	CostMicros int64  `json:"cost_micros"`
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	accountID := accountFrom(r)
	now := time.Now()
	from := now.AddDate(0, 0, -7)
	to := now.Add(time.Minute)
	if raw := r.URL.Query().Get("from"); raw != "" {
		if parsed, err := parseUsageTime(raw); err == nil {
			from = parsed
		}
	}
	if raw := r.URL.Query().Get("to"); raw != "" {
		if parsed, err := parseUsageTime(raw); err == nil {
			to = parsed
		}
	}
	totals, err := s.db.UsageTotals(r.Context(), store.UsageTotalsParams{
		UserID: accountID, CreatedAt: from, CreatedAt_2: to,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load usage")
		return
	}
	byBotRows, err := s.db.UsageByBot(r.Context(), store.UsageByBotParams{
		UserID: accountID, CreatedAt: from, CreatedAt_2: to,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load usage")
		return
	}
	byModelRows, err := s.db.UsageByModel(r.Context(), store.UsageByModelParams{
		UserID: accountID, CreatedAt: from, CreatedAt_2: to,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load usage")
		return
	}
	dailyRows, err := s.db.UsageDaily(r.Context(), store.UsageDailyParams{
		UserID: accountID, CreatedAt: from, CreatedAt_2: to,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load usage")
		return
	}
	byBot := make([]usageByBotDTO, 0, len(byBotRows))
	for _, row := range byBotRows {
		byBot = append(byBot, usageByBotDTO{
			BotID: row.BotID, BotName: row.BotName,
			PromptTokens: row.PromptTokens, CompletionTokens: row.CompletionTokens, CostMicros: row.CostMicros,
		})
	}
	byModel := make([]usageByModelDTO, 0, len(byModelRows))
	for _, row := range byModelRows {
		byModel = append(byModel, usageByModelDTO{
			Model: row.Model, PromptTokens: row.PromptTokens,
			CompletionTokens: row.CompletionTokens, CostMicros: row.CostMicros,
		})
	}
	account, _ := s.db.GetAccountByID(r.Context(), accountID)
	weeklySpent, _ := s.db.GetWeeklyUsageCost(r.Context(), accountID)
	weekReset := now.AddDate(0, 0, (8-int(now.Weekday()))%7)
	weekReset = time.Date(weekReset.Year(), weekReset.Month(), weekReset.Day(), 0, 0, 0, 0, weekReset.Location())

	daily := make([]usageDailyDTO, 0, len(dailyRows))
	for _, row := range dailyRows {
		day := ""
		if row.Day.Valid {
			day = row.Day.Time.Format("2006-01-02")
		}
		daily = append(daily, usageDailyDTO{
			Day: day, Tokens: row.Tokens, CostMicros: row.CostMicros,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from,
		"to":   to,
		"totals": usageTotalsDTO{
			PromptTokens: totals.PromptTokens, CachedTokens: totals.CachedTokens,
			CompletionTokens: totals.CompletionTokens, CostMicros: totals.CostMicros,
		},
		"by_bot":               byBot,
		"by_model":             byModel,
		"daily":                daily,
		"weekly_budget_micros": account.WeeklyBudgetMicros,
		"weekly_spent_micros":  weeklySpent,
		"week_reset_at":        weekReset,
	})
}

func (s *Server) handleUpdateUsageBudget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WeeklyBudgetMicros int64 `json:"weekly_budget_micros"`
	}
	if err := decodeJSON(w, r, &req); err != nil || req.WeeklyBudgetMicros < 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "weekly_budget_micros must be non-negative")
		return
	}
	budget, err := s.db.SetAccountWeeklyBudget(r.Context(), store.SetAccountWeeklyBudgetParams{
		ID: accountFrom(r), WeeklyBudgetMicros: req.WeeklyBudgetMicros,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update budget")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"weekly_budget_micros": budget})
}

func parseUsageTime(raw string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}
	return time.ParseInLocation("2006-01-02", raw, time.Local)
}
