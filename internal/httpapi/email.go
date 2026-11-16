package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/mailbox"
	"superbot/internal/store"
)

type emailDraftDTO struct {
	ID        uuid.UUID `json:"id"`
	To        string    `json:"to"`
	CC        string    `json:"cc"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toEmailDraftDTO(draft store.EmailDraft) emailDraftDTO {
	return emailDraftDTO{
		ID: draft.ID, To: draft.ToAddr, CC: draft.CcAddr, Subject: draft.Subject,
		Body: draft.Body, CreatedAt: draft.CreatedAt, UpdatedAt: draft.UpdatedAt,
	}
}

func (s *Server) emailRoutes(r chi.Router) {
	r.Get("/status", s.handleEmailStatus)
	r.Get("/inbox", s.handleEmailInbox)
	r.Post("/send", s.handleEmailSend)
	r.Get("/drafts", s.handleListEmailDrafts)
	r.Post("/drafts", s.handleCreateEmailDraft)
	r.Patch("/drafts/{draftID}", s.handleUpdateEmailDraft)
	r.Delete("/drafts/{draftID}", s.handleDeleteEmailDraft)
}

func (s *Server) emailConnector(r *http.Request) (store.Connector, string, error) {
	connector, err := s.connectors.ByProvider(r.Context(), accountFrom(r), "email")
	if err != nil {
		return store.Connector{}, "", err
	}
	token, err := s.connectors.AccessToken(connector)
	if err != nil {
		return store.Connector{}, "", err
	}
	return connector, token, nil
}

func (s *Server) handleEmailStatus(w http.ResponseWriter, r *http.Request) {
	connector, _, err := s.emailConnector(r)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"configured":      false,
			"requires_config": []string{"连接 Email 连接器（IMAP/SMTP host、username、from、密码）"},
		})
		return
	}
	imapCfg, imapErr := mailbox.ParseConfig(connector.Config)
	smtpCfg, smtpErr := mailbox.ParseSMTPConfig(connector.Config)
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": imapErr == nil || smtpErr == nil,
		"imap":       imapErr == nil,
		"smtp":       smtpErr == nil,
		"from":       smtpCfg.From,
		"imap_host":  imapCfg.Host,
	})
}

func (s *Server) handleEmailInbox(w http.ResponseWriter, r *http.Request) {
	connector, password, err := s.emailConnector(r)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "requires_config", "connect an email account first")
		return
	}
	cfg, err := mailbox.ParseConfig(connector.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_config", err.Error())
		return
	}
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	messages, err := s.mailbox.Recent(ctx, cfg, password, limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, "imap_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"messages": messages,
		"threads":  mailbox.GroupThreads(messages),
	})
}

func (s *Server) handleEmailSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To      string `json:"to"`
		CC      string `json:"cc"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	to := splitEmailAddresses(req.To)
	cc := splitEmailAddresses(req.CC)
	if len(to) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "at least one recipient is required")
		return
	}
	if strings.TrimSpace(req.Subject) == "" && strings.TrimSpace(req.Body) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "subject or body is required")
		return
	}
	connector, password, err := s.emailConnector(r)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "requires_config", "connect an email account first")
		return
	}
	cfg, err := mailbox.ParseSMTPConfig(connector.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_config", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := s.mailbox.Send(ctx, cfg, password, mailbox.Outgoing{
		To: to, CC: cc, Subject: strings.TrimSpace(req.Subject), Body: req.Body,
	}); err != nil {
		writeError(w, http.StatusBadGateway, "smtp_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sent", "to": to})
}

func (s *Server) handleListEmailDrafts(w http.ResponseWriter, r *http.Request) {
	drafts, err := s.db.ListEmailDrafts(r.Context(), store.ListEmailDraftsParams{
		OwnerID: accountFrom(r), Limit: 100,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list drafts")
		return
	}
	out := make([]emailDraftDTO, 0, len(drafts))
	for _, draft := range drafts {
		out = append(out, toEmailDraftDTO(draft))
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": out})
}

type emailDraftRequest struct {
	To      string `json:"to"`
	CC      string `json:"cc"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (s *Server) handleCreateEmailDraft(w http.ResponseWriter, r *http.Request) {
	var req emailDraftRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	draft, err := s.db.CreateEmailDraft(r.Context(), store.CreateEmailDraftParams{
		OwnerID: accountFrom(r), ToAddr: strings.TrimSpace(req.To), CcAddr: strings.TrimSpace(req.CC),
		Subject: strings.TrimSpace(req.Subject), Body: req.Body,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create draft")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"draft": toEmailDraftDTO(draft)})
}

func (s *Server) handleUpdateEmailDraft(w http.ResponseWriter, r *http.Request) {
	draftID, ok := parseUUIDParam(w, r, "draftID")
	if !ok {
		return
	}
	var req emailDraftRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	draft, err := s.db.UpdateEmailDraft(r.Context(), store.UpdateEmailDraftParams{
		ID: draftID, OwnerID: accountFrom(r), ToAddr: strings.TrimSpace(req.To),
		CcAddr: strings.TrimSpace(req.CC), Subject: strings.TrimSpace(req.Subject), Body: req.Body,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "draft not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not update draft")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"draft": toEmailDraftDTO(draft)})
}

func (s *Server) handleDeleteEmailDraft(w http.ResponseWriter, r *http.Request) {
	draftID, ok := parseUUIDParam(w, r, "draftID")
	if !ok {
		return
	}
	rows, err := s.db.DeleteEmailDraft(r.Context(), store.DeleteEmailDraftParams{
		ID: draftID, OwnerID: accountFrom(r),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not delete draft")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "not_found", "draft not found")
		return
	}
	writeNoContent(w)
}

func splitEmailAddresses(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if strings.Contains(value, "@") {
			out = append(out, value)
		}
	}
	return out
}
