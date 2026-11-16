package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

type accountDTO struct {
	ID           uuid.UUID  `json:"id"`
	Name         string     `json:"name"`
	Role         string     `json:"role"`
	Active       bool       `json:"active"`
	PrimaryBotID *uuid.UUID `json:"primary_bot_id"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (s *Server) accountsRoutes(r chi.Router) {
	r.Get("/", s.handleListAccounts)
	r.Post("/", s.handleCreateAccount)
	r.Post("/{accountID}/switch", s.handleSwitchAccount)
	r.Get("/{accountID}/members", s.handleListAccountMembers)
	r.Get("/{accountID}/invitations", s.handleListAccountInvitations)
	r.Post("/{accountID}/invitations", s.handleCreateAccountInvitation)
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	user := userFrom(r)
	tx, err := s.db.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create account")
		return
	}
	defer tx.Rollback(r.Context())
	q := s.db.WithTx(tx)
	account, err := q.CreateAccount(r.Context(), store.CreateAccountParams{
		Name: strings.TrimSpace(req.Name), CreatedByUserID: user.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create account")
		return
	}
	if err := q.AddAccountMember(r.Context(), store.AddAccountMemberParams{
		AccountID: account.ID, UserID: user.ID, Role: "owner",
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not add account owner")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create account")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"account": accountDTO{
		ID: account.ID, Name: account.Name, Role: "owner", Active: false,
		PrimaryBotID: account.PrimaryBotID, CreatedAt: account.CreatedAt,
	}})
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.ListAccountsForUser(r.Context(), userFrom(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list accounts")
		return
	}
	active := accountFrom(r)
	out := make([]accountDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, accountDTO{
			ID: row.ID, Name: row.Name, Role: row.Role, Active: row.ID == active,
			PrimaryBotID: row.PrimaryBotID, CreatedAt: row.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out, "active_account_id": active})
}

func (s *Server) handleSwitchAccount(w http.ResponseWriter, r *http.Request) {
	accountID, ok := parseUUIDParam(w, r, "accountID")
	if !ok {
		return
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "login required")
		return
	}
	if err := s.auth.SwitchAccount(r.Context(), cookie.Value, accountID); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
		return
	}
	rows, _ := s.db.ListAccountsForUser(r.Context(), userFrom(r).ID)
	out := make([]accountDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, accountDTO{
			ID: row.ID, Name: row.Name, Role: row.Role, Active: row.ID == accountID,
			PrimaryBotID: row.PrimaryBotID, CreatedAt: row.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out, "active_account_id": accountID})
}

func (s *Server) accountRole(ctx context.Context, accountID, userID uuid.UUID) (string, error) {
	role, err := s.db.GetAccountMembership(ctx, store.GetAccountMembershipParams{
		AccountID: accountID, UserID: userID,
	})
	if err != nil {
		return "", err
	}
	return role, nil
}

func (s *Server) handleListAccountMembers(w http.ResponseWriter, r *http.Request) {
	accountID, ok := parseUUIDParam(w, r, "accountID")
	if !ok {
		return
	}
	if _, err := s.accountRole(r.Context(), accountID, userFrom(r).ID); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "not a member of this account")
		return
	}
	rows, err := s.db.ListAccountMembers(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list account members")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"id": row.ID, "email": row.Email, "display_name": row.DisplayName,
			"role": row.Role, "created_at": row.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out})
}

func (s *Server) handleListAccountInvitations(w http.ResponseWriter, r *http.Request) {
	accountID, ok := parseUUIDParam(w, r, "accountID")
	if !ok {
		return
	}
	role, err := s.accountRole(r.Context(), accountID, userFrom(r).ID)
	if err != nil || (role != "owner" && role != "admin") {
		writeError(w, http.StatusForbidden, "forbidden", "owner or admin required")
		return
	}
	rows, err := s.db.ListAccountInvitations(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list invitations")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"id": row.ID, "email": row.Email, "role": row.Role,
			"expires_at": row.ExpiresAt, "created_at": row.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"invitations": out})
}

func (s *Server) handleCreateAccountInvitation(w http.ResponseWriter, r *http.Request) {
	accountID, ok := parseUUIDParam(w, r, "accountID")
	if !ok {
		return
	}
	role, err := s.accountRole(r.Context(), accountID, userFrom(r).ID)
	if err != nil || (role != "owner" && role != "admin") {
		writeError(w, http.StatusForbidden, "forbidden", "owner or admin required")
		return
	}
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.Email) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "email is required")
		return
	}
	if req.Role != "admin" {
		req.Role = "member"
	}
	token, err := randomInviteToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not generate invitation")
		return
	}
	invitation, err := s.db.CreateAccountInvitation(r.Context(), store.CreateAccountInvitationParams{
		AccountID: accountID, Email: strings.ToLower(strings.TrimSpace(req.Email)),
		Role: req.Role, TokenHash: hashInviteToken(token),
		InvitedBy: userFrom(r).ID, ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create invitation")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"invitation": map[string]any{
			"id": invitation.ID, "email": invitation.Email, "role": invitation.Role,
			"expires_at": invitation.ExpiresAt,
		},
		"token": token,
		"url":   "/invite/" + token,
	})
}

func (s *Server) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "token is required")
		return
	}
	invitation, err := s.db.GetAccountInvitationByHash(r.Context(), hashInviteToken(strings.TrimSpace(req.Token)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "invitation is invalid or expired")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load invitation")
		return
	}
	user := userFrom(r)
	if strings.ToLower(user.Email) != strings.ToLower(invitation.Email) {
		writeError(w, http.StatusForbidden, "forbidden", "invitation email does not match this account")
		return
	}
	if err := s.db.AddAccountMember(r.Context(), store.AddAccountMemberParams{
		AccountID: invitation.AccountID, UserID: user.ID, Role: invitation.Role,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not join account")
		return
	}
	_ = s.db.AcceptAccountInvitation(r.Context(), invitation.ID)
	writeJSON(w, http.StatusOK, map[string]any{"account_id": invitation.AccountID})
}

func randomInviteToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashInviteToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
