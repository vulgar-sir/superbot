package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/store"
)

type creditLedgerDTO struct {
	ID           uuid.UUID  `json:"id"`
	Kind         string     `json:"kind"`
	AmountMicros int64      `json:"amount_micros"`
	Balance      int64      `json:"balance_after_micros"`
	Note         string     `json:"note"`
	CreatedBy    *uuid.UUID `json:"created_by_user_id"`
	CreatedAt    time.Time  `json:"created_at"`
}

func toCreditLedgerDTO(entry store.CreditLedger) creditLedgerDTO {
	return creditLedgerDTO{
		ID: entry.ID, Kind: entry.Kind, AmountMicros: entry.AmountMicros,
		Balance: entry.BalanceAfterMicros, Note: entry.Note,
		CreatedBy: entry.CreatedByUserID, CreatedAt: entry.CreatedAt,
	}
}

func (s *Server) billingRoutes(r chi.Router) {
	r.Get("/", s.handleGetBilling)
	r.Post("/credits", s.handleAdjustCredits)
}

func (s *Server) handleGetBilling(w http.ResponseWriter, r *http.Request) {
	accountID := accountFrom(r)
	billing, err := s.db.GetAccountBilling(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load billing")
		return
	}
	ledger, err := s.db.ListCreditLedger(r.Context(), store.ListCreditLedgerParams{
		AccountID: accountID, Limit: 50,
	})
	if err != nil {
		ledger = nil
	}
	weeklySpent, _ := s.db.GetWeeklyUsageCost(r.Context(), accountID)
	account, _ := s.db.GetAccountByID(r.Context(), accountID)
	entries := make([]creditLedgerDTO, 0, len(ledger))
	for _, entry := range ledger {
		entries = append(entries, toCreditLedgerDTO(entry))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plan": billing.Plan, "credits_micros": billing.CreditsMicros,
		"weekly_budget_micros": account.WeeklyBudgetMicros,
		"weekly_spent_micros":  weeklySpent,
		"ledger":               entries,
	})
}

func (s *Server) handleAdjustCredits(w http.ResponseWriter, r *http.Request) {
	accountID := accountFrom(r)
	role, err := s.accountRole(r.Context(), accountID, userFrom(r).ID)
	if err != nil || (role != "owner" && role != "admin") {
		writeError(w, http.StatusForbidden, "forbidden", "only account owners and admins can adjust credits")
		return
	}
	var req struct {
		AmountMicros int64  `json:"amount_micros"`
		Note         string `json:"note"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.AmountMicros == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "amount_micros must be non-zero")
		return
	}
	if req.AmountMicros > 1_000_000_000_000 || req.AmountMicros < -1_000_000_000_000 {
		writeError(w, http.StatusBadRequest, "invalid_request", "amount is out of range")
		return
	}
	tx, err := s.db.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not begin transaction")
		return
	}
	defer tx.Rollback(r.Context())
	queries := s.db.WithTx(tx)
	billing, err := queries.GetAccountBilling(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load billing")
		return
	}
	balance := billing.CreditsMicros + req.AmountMicros
	if balance < 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "credits balance cannot become negative")
		return
	}
	if err := queries.UpdateAccountCredits(r.Context(), store.UpdateAccountCreditsParams{
		ID: accountID, CreditsMicros: balance,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update credits")
		return
	}
	user := userFrom(r)
	entry, err := queries.CreateCreditLedgerEntry(r.Context(), store.CreateCreditLedgerEntryParams{
		AccountID: accountID, Kind: "adjustment", AmountMicros: req.AmountMicros,
		BalanceAfterMicros: balance, Note: strings.TrimSpace(req.Note), CreatedByUserID: &user.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not write credit ledger")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not commit credits")
		return
	}
	s.audit(r.Context(), accountID, user.ID, "credits.adjust", "account", &accountID, map[string]any{
		"amount_micros": req.AmountMicros, "balance_after_micros": balance, "note": entry.Note,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"credits_micros": balance, "entry": toCreditLedgerDTO(entry),
	})
}
