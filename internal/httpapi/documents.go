package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/store"
)

type documentDTO struct {
	ID              uuid.UUID  `json:"id"`
	Title           string     `json:"title"`
	Content         string     `json:"content"`
	Kind            string     `json:"kind"`
	Revision        int64      `json:"revision"`
	Visibility      string     `json:"visibility"`
	CreatedByUserID *uuid.UUID `json:"created_by_user_id"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func toDocumentDTO(doc store.Document) documentDTO {
	return documentDTO{
		ID: doc.ID, Title: doc.Title, Content: doc.Content, Kind: doc.Kind, Revision: doc.Revision,
		Visibility: doc.Visibility, CreatedByUserID: doc.CreatedByUserID,
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
	}
}

func (s *Server) documentRoutes(r chi.Router) {
	r.Get("/", s.handleListDocuments)
	r.Post("/", s.handleCreateDocument)
	r.Get("/{documentID}", s.handleGetDocument)
	r.Get("/{documentID}/versions", s.handleListDocumentVersions)
	r.Get("/{documentID}/events", s.handleDocumentEvents)
	r.Post("/{documentID}/collab", s.handleDocumentCollab)
	r.Get("/{documentID}/crdt", s.handleListDocumentCRDT)
	r.Post("/{documentID}/crdt/seed", s.handleSeedDocumentCRDT)
	r.Post("/{documentID}/crdt", s.handlePushDocumentCRDT)
	r.Post("/{documentID}/cursor", s.handleDocumentCursor)
	r.Route("/{documentID}/acl", s.documentACLRoutes)
	r.Patch("/{documentID}", s.handleUpdateDocument)
	r.Delete("/{documentID}", s.handleDeleteDocument)
}

func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := s.db.ListDocuments(r.Context(), accountFrom(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list documents")
		return
	}
	role, _ := s.accountRole(r.Context(), accountFrom(r), userFrom(r).ID)
	out := make([]documentDTO, 0, len(docs))
	for _, doc := range docs {
		if _, ok := s.documentAccess(r.Context(), role, userFrom(r).ID, doc); !ok {
			continue
		}
		out = append(out, toDocumentDTO(doc))
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": out})
}

type documentRequest struct {
	Title      string `json:"title"`
	Content    string `json:"content"`
	Kind       string `json:"kind"`
	Visibility string `json:"visibility"`
}

func (s *Server) handleCreateDocument(w http.ResponseWriter, r *http.Request) {
	var req documentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "title is required")
		return
	}
	kind := req.Kind
	if kind != "canvas" {
		kind = "document"
	}
	userID := userFrom(r).ID
	doc, err := s.db.CreateDocument(r.Context(), store.CreateDocumentParams{
		OwnerID: accountFrom(r), Title: title, Content: req.Content, Kind: kind, CreatedByUserID: &userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create document")
		return
	}
	visibility := req.Visibility
	if visibility != "owner" {
		visibility = "account"
	}
	_ = s.db.SetDocumentAccess(r.Context(), store.SetDocumentAccessParams{
		ID: doc.ID, OwnerID: accountFrom(r), Visibility: visibility, CreatedByUserID: &userID,
	})
	doc.Visibility = visibility
	doc.CreatedByUserID = &userID
	// Documents are account-level; surface them in the primary bot's Library.
	if account, accountErr := s.db.GetAccountByID(r.Context(), accountFrom(r)); accountErr == nil && account.PrimaryBotID != nil {
		if _, indexErr := s.library.IndexDocument(r.Context(), accountFrom(r), *account.PrimaryBotID, doc); indexErr != nil {
			s.logger.Warn("index document into library failed", "document_id", doc.ID, "error", indexErr)
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"document": toDocumentDTO(doc)})
}

func (s *Server) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.documentForAccess(w, r, false)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"document": toDocumentDTO(doc)})
}

func (s *Server) handleUpdateDocument(w http.ResponseWriter, r *http.Request) {
	documentID, ok := parseUUIDParam(w, r, "documentID")
	if !ok {
		return
	}
	existing, ok := s.documentForEditor(w, r)
	if !ok {
		return
	}
	var req documentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = existing.Title
	}
	kind := req.Kind
	if kind != "canvas" {
		kind = existing.Kind
	}
	if title != existing.Title || req.Content != existing.Content || kind != existing.Kind {
		userID := userFrom(r).ID
		_, _ = s.db.CreateDocumentVersion(r.Context(), store.CreateDocumentVersionParams{
			DocumentID: existing.ID, OwnerID: accountFrom(r), Title: existing.Title,
			Content: existing.Content, Kind: existing.Kind, CreatedByUserID: &userID,
		})
	}
	doc, err := s.db.UpdateDocument(r.Context(), store.UpdateDocumentParams{
		ID: documentID, OwnerID: accountFrom(r), Title: title, Content: req.Content, Kind: kind,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update document")
		return
	}
	_, _ = s.db.UpdateLibraryItemsBySource(r.Context(), store.UpdateLibraryItemsBySourceParams{
		OwnerID: accountFrom(r), Source: "document", SourceID: documentID.String(),
		Title: doc.Title, Content: doc.Content,
	})
	writeJSON(w, http.StatusOK, map[string]any{"document": toDocumentDTO(doc)})
}

func (s *Server) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	documentID, ok := parseUUIDParam(w, r, "documentID")
	if !ok {
		return
	}
	if _, ok := s.documentForEditor(w, r); !ok {
		return
	}
	n, err := s.db.DeleteDocument(r.Context(), store.DeleteDocumentParams{ID: documentID, OwnerID: accountFrom(r)})
	if err != nil || n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "document not found")
		return
	}
	_, _ = s.db.DeleteLibraryItemsBySource(r.Context(), store.DeleteLibraryItemsBySourceParams{
		OwnerID: accountFrom(r), Source: "document", SourceID: documentID.String(),
	})
	writeNoContent(w)
}

type documentVersionDTO struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) handleListDocumentVersions(w http.ResponseWriter, r *http.Request) {
	documentID, ok := parseUUIDParam(w, r, "documentID")
	if !ok {
		return
	}
	if _, ok := s.documentForAccess(w, r, false); !ok {
		return
	}
	versions, err := s.db.ListDocumentVersions(r.Context(), store.ListDocumentVersionsParams{
		DocumentID: documentID, OwnerID: accountFrom(r),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list versions")
		return
	}
	out := make([]documentVersionDTO, 0, len(versions))
	for _, version := range versions {
		out = append(out, documentVersionDTO{
			ID: version.ID, Title: version.Title, Content: version.Content,
			Kind: version.Kind, CreatedAt: version.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": out})
}
