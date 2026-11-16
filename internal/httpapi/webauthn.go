package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

func (s *Server) webauthnRoutes(r chi.Router) {
	r.Post("/register/begin", s.handleWebAuthnRegisterBegin)
	r.Post("/register/finish", s.handleWebAuthnRegisterFinish)
	r.Get("/credentials", s.handleWebAuthnCredentials)
	r.Patch("/credentials/{credentialID}", s.handleRenameWebAuthnCredential)
	r.Delete("/credentials/{credentialID}", s.handleDeleteWebAuthnCredential)
}

func (s *Server) webauthnPublicRoutes(r chi.Router) {
	r.Post("/login/begin", s.handleWebAuthnLoginBegin)
	r.Post("/login/finish", s.handleWebAuthnLoginFinish)
}

func (s *Server) handleWebAuthnRegisterBegin(w http.ResponseWriter, r *http.Request) {
	creation, err := s.passkeys.BeginRegistration(r.Context(), r, userFrom(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "webauthn_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, creation)
}

func (s *Server) handleWebAuthnRegisterFinish(w http.ResponseWriter, r *http.Request) {
	credential, err := s.passkeys.FinishRegistration(r.Context(), r, userFrom(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "webauthn_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "credential_id": credential.ID})
}

func (s *Server) handleWebAuthnCredentials(w http.ResponseWriter, r *http.Request) {
	credentials, err := s.db.ListWebAuthnCredentials(r.Context(), userFrom(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list credentials")
		return
	}
	items := make([]map[string]any, 0, len(credentials))
	for _, credential := range credentials {
		items = append(items, map[string]any{
			"id": credential.ID, "name": credential.Name, "created_at": credential.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(items), "credentials": items})
}

func (s *Server) handleRenameWebAuthnCredential(w http.ResponseWriter, r *http.Request) {
	credentialID, ok := parseUUIDParam(w, r, "credentialID")
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if len([]rune(name)) > 100 {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is too long")
		return
	}
	credential, err := s.db.RenameWebAuthnCredential(r.Context(), store.RenameWebAuthnCredentialParams{
		ID: credentialID, UserID: userFrom(r).ID, Name: name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "security key not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not rename security key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credential": map[string]any{
		"id": credential.ID, "name": credential.Name, "created_at": credential.CreatedAt,
	}})
}

func (s *Server) handleDeleteWebAuthnCredential(w http.ResponseWriter, r *http.Request) {
	credentialID, ok := parseUUIDParam(w, r, "credentialID")
	if !ok {
		return
	}
	rows, err := s.db.DeleteWebAuthnCredential(r.Context(), store.DeleteWebAuthnCredentialParams{
		ID: credentialID, UserID: userFrom(r).ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not delete security key")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "not_found", "security key not found")
		return
	}
	writeNoContent(w)
}

func (s *Server) handleWebAuthnLoginBegin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	user, err := s.db.GetUserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "no security key for this account")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load account")
		return
	}
	assertion, err := s.passkeys.BeginLogin(r.Context(), r, user)
	if err != nil {
		writeError(w, http.StatusBadRequest, "webauthn_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, assertion)
}

func (s *Server) handleWebAuthnLoginFinish(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("email")))
	if email == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "email query parameter is required")
		return
	}
	user, err := s.db.GetUserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid security key login")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load account")
		return
	}
	if _, err := s.passkeys.FinishLogin(r.Context(), r, user); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", err.Error())
		return
	}
	token, expires, err := s.auth.CreateSessionForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create session")
		return
	}
	s.setSessionCookie(w, token, expires)
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserDTO(user)})
}
