package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"superbot/internal/store"
)

func (s *Server) handleCreateBotShare(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	var req struct {
		ExpiresInHours int   `json:"expires_in_hours"`
		MaxUses        int32 `json:"max_uses"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	var expiresAt *time.Time
	if req.ExpiresInHours > 0 {
		expiry := time.Now().Add(time.Duration(req.ExpiresInHours) * time.Hour)
		expiresAt = &expiry
	}
	var maxUses pgtype.Int4
	if req.MaxUses > 0 {
		maxUses = pgtype.Int4{Int32: req.MaxUses, Valid: true}
	}
	accountID := accountFrom(r)
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "bot not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load bot")
		return
	}
	template := s.buildBotTemplate(r.Context(), accountID, bot)
	raw, err := json.Marshal(template)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not encode template")
		return
	}
	for attempt := 0; attempt < 3; attempt++ {
		token, err := randomShareToken()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not create share token")
			return
		}
		_, err = s.db.CreateBotShareLink(r.Context(), store.CreateBotShareLinkParams{
			Token:     token,
			OwnerID:   accountID,
			BotID:     &bot.ID,
			Template:  raw,
			ExpiresAt: expiresAt,
			MaxUses:   maxUses,
		})
		if err == nil {
			writeJSON(w, http.StatusCreated, map[string]any{"token": token})
			return
		}
	}
	writeError(w, http.StatusInternalServerError, "internal", "could not create share link")
}

func (s *Server) handleGetBotShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	share, err := s.db.GetBotShareLink(r.Context(), token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "share link not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load share link")
		return
	}
	if share.RevokedAt != nil || (share.ExpiresAt != nil && share.ExpiresAt.Before(time.Now())) ||
		(share.MaxUses.Valid && share.Uses >= share.MaxUses.Int32) {
		writeError(w, http.StatusNotFound, "not_found", "share link is no longer available")
		return
	}
	var template botTemplate
	if err := json.Unmarshal(share.Template, &template); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "invalid share template")
		return
	}
	_ = s.db.IncrementBotShareLinkUses(r.Context(), share.Token)
	writeJSON(w, http.StatusOK, map[string]any{
		"template": template,
		"profile": map[string]any{
			"name":         template.Profile.Name,
			"title":        template.Profile.Title,
			"description":  template.Profile.Description,
			"avatar_shape": template.Profile.AvatarShape,
			"avatar_color": template.Profile.AvatarColor,
			"skills":       len(template.Skills),
			"routines":     len(template.Routines),
		},
	})
}

func randomShareToken() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *Server) handleListBotShares(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountFrom(r)}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	rows, err := s.db.ListBotShareLinks(r.Context(), store.ListBotShareLinksParams{
		OwnerID: accountFrom(r), BotID: &botID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list share links")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"token": row.Token, "created_at": row.CreatedAt, "expires_at": row.ExpiresAt,
			"revoked_at": row.RevokedAt, "uses": row.Uses, "max_uses": row.MaxUses,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"shares": out})
}

func (s *Server) handleRevokeBotShare(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	token := chi.URLParam(r, "token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "token is required")
		return
	}
	n, err := s.db.RevokeBotShareLink(r.Context(), store.RevokeBotShareLinkParams{
		Token: token, OwnerID: accountFrom(r), BotID: &botID,
	})
	if err != nil || n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "share link not found")
		return
	}
	writeNoContent(w)
}
