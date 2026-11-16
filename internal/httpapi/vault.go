package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/store"
	"superbot/internal/vault"
)

type vaultItemDTO struct {
	ID          uuid.UUID `json:"id"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	Username    string    `json:"username"`
	URL         string    `json:"url"`
	HasPassword bool      `json:"has_password"`
	HasNotes    bool      `json:"has_notes"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toVaultItemDTO(item store.VaultItem) vaultItemDTO {
	return vaultItemDTO{
		ID:          item.ID,
		Kind:        item.Kind,
		Name:        item.Name,
		Username:    item.Username,
		URL:         item.Url,
		HasPassword: len(item.PasswordEnc) > 0,
		HasNotes:    len(item.NotesEnc) > 0,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}

func (s *Server) vaultRoutes(r chi.Router) {
	r.Get("/", s.handleListVault)
	r.Post("/", s.handleCreateVault)
	r.Route("/{itemID}", func(r chi.Router) {
		r.Patch("/", s.handleUpdateVault)
		r.Delete("/", s.handleDeleteVault)
		r.Get("/reveal", s.handleRevealVault)
	})
}

type vaultRequest struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
	URL      string `json:"url"`
	Notes    string `json:"notes"`
}

func (s *Server) handleListVault(w http.ResponseWriter, r *http.Request) {
	items, err := s.vault.List(r.Context(), accountFrom(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load vault")
		return
	}
	out := make([]vaultItemDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toVaultItemDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) handleCreateVault(w http.ResponseWriter, r *http.Request) {
	var req vaultRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	item, err := s.vault.Create(r.Context(), accountFrom(r), vault.Input{
		Kind: req.Kind, Name: req.Name, Username: req.Username,
		Password: req.Password, URL: req.URL, Notes: req.Notes,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"item": toVaultItemDTO(item)})
}

func (s *Server) handleUpdateVault(w http.ResponseWriter, r *http.Request) {
	itemID, ok := parseUUIDParam(w, r, "itemID")
	if !ok {
		return
	}
	var req vaultRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	item, err := s.vault.Update(r.Context(), accountFrom(r), itemID, vault.Input{
		Kind: req.Kind, Name: req.Name, Username: req.Username,
		Password: req.Password, URL: req.URL, Notes: req.Notes,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": toVaultItemDTO(item)})
}

func (s *Server) handleDeleteVault(w http.ResponseWriter, r *http.Request) {
	itemID, ok := parseUUIDParam(w, r, "itemID")
	if !ok {
		return
	}
	if err := s.vault.Delete(r.Context(), accountFrom(r), itemID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "vault item not found")
		return
	}
	writeNoContent(w)
}

func (s *Server) handleRevealVault(w http.ResponseWriter, r *http.Request) {
	itemID, ok := parseUUIDParam(w, r, "itemID")
	if !ok {
		return
	}
	item, password, notes, err := s.vault.Reveal(r.Context(), accountFrom(r), itemID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "vault item not found")
		return
	}
	s.audit(r.Context(), accountFrom(r), userFrom(r).ID, "vault.reveal", "vault_item", &itemID, nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"item":     toVaultItemDTO(item),
		"password": password,
		"notes":    notes,
	})
}
