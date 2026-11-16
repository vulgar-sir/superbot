package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/store"
)

func (s *Server) documentAccess(ctx context.Context, role string, userID uuid.UUID, doc store.Document) (string, bool) {
	if role == "owner" || role == "admin" {
		return "editor", true
	}
	if doc.CreatedByUserID != nil && *doc.CreatedByUserID == userID {
		return "editor", true
	}
	if acl, err := s.db.GetResourceACLRole(ctx, store.GetResourceACLRoleParams{
		ResourceType: "document", ResourceID: doc.ID, UserID: &userID,
	}); err == nil {
		return acl, true
	}
	if doc.Visibility == "owner" {
		return "", false
	}
	return "viewer", true
}

func (s *Server) documentForAccess(w http.ResponseWriter, r *http.Request, editor bool) (store.Document, bool) {
	documentID, ok := parseUUIDParam(w, r, "documentID")
	if !ok {
		return store.Document{}, false
	}
	doc, err := s.db.GetDocument(r.Context(), store.GetDocumentParams{ID: documentID, OwnerID: accountFrom(r)})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "document not found")
		return store.Document{}, false
	}
	role, _ := s.accountRole(r.Context(), accountFrom(r), userFrom(r).ID)
	access, ok := s.documentAccess(r.Context(), role, userFrom(r).ID, doc)
	if !ok || (editor && access != "editor") {
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
		} else {
			writeError(w, http.StatusForbidden, "forbidden", "document is read-only for this user")
		}
		return store.Document{}, false
	}
	return doc, true
}

func (s *Server) documentForEditor(w http.ResponseWriter, r *http.Request) (store.Document, bool) {
	return s.documentForAccess(w, r, true)
}

func (s *Server) documentACLRoutes(r chi.Router) {
	r.Get("/", s.handleListDocumentACL)
	r.Put("/", s.handleSetDocumentACL)
	r.Delete("/{userID}", s.handleDeleteDocumentACL)
}

func (s *Server) handleListDocumentACL(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.documentForEditor(w, r)
	if !ok {
		return
	}
	rows, err := s.db.ListResourceACL(r.Context(), store.ListResourceACLParams{
		ResourceType: "document", ResourceID: doc.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load document access")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"user_id": row.UserID, "email": row.Email, "display_name": row.DisplayName, "role": row.Role})
	}
	writeJSON(w, http.StatusOK, map[string]any{"acl": out})
}

func (s *Server) handleSetDocumentACL(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.documentForEditor(w, r)
	if !ok {
		return
	}
	var req struct {
		UserID uuid.UUID `json:"user_id"`
		Role   string    `json:"role"`
	}
	if err := decodeJSON(w, r, &req); err != nil || req.UserID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "user_id is required")
		return
	}
	if req.Role != "viewer" && req.Role != "editor" && req.Role != "runner" {
		writeError(w, http.StatusBadRequest, "invalid_request", "role must be viewer, editor or runner")
		return
	}
	if _, err := s.db.GetAccountMembership(r.Context(), store.GetAccountMembershipParams{
		AccountID: accountFrom(r), UserID: req.UserID,
	}); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "user is not a member of this account")
		return
	}
	if err := s.db.UpsertResourceACL(r.Context(), store.UpsertResourceACLParams{
		AccountID: accountFrom(r), ResourceType: "document", ResourceID: doc.ID,
		UserID: &req.UserID, Role: req.Role,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update document access")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteDocumentACL(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.documentForEditor(w, r)
	if !ok {
		return
	}
	userID, ok := parseUUIDParam(w, r, "userID")
	if !ok {
		return
	}
	if err := s.db.DeleteResourceACL(r.Context(), store.DeleteResourceACLParams{
		ResourceType: "document", ResourceID: doc.ID, UserID: &userID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not remove document access")
		return
	}
	writeNoContent(w)
}
