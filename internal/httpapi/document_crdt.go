package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"superbot/internal/store"
)

func (s *Server) handleListDocumentCRDT(w http.ResponseWriter, r *http.Request) {
	documentID, ok := parseUUIDParam(w, r, "documentID")
	if !ok {
		return
	}
	if _, ok := s.documentForAccess(w, r, false); !ok {
		return
	}
	updates, err := s.db.ListDocumentCRDTUpdates(r.Context(), documentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load CRDT updates")
		return
	}
	encoded := make([]string, 0, len(updates))
	for _, update := range updates {
		encoded = append(encoded, base64.StdEncoding.EncodeToString(update))
	}
	writeJSON(w, http.StatusOK, map[string]any{"updates": encoded})
}

func (s *Server) handleSeedDocumentCRDT(w http.ResponseWriter, r *http.Request) {
	documentID, ok := parseUUIDParam(w, r, "documentID")
	if !ok {
		return
	}
	if _, ok := s.documentForEditor(w, r); !ok {
		return
	}
	var req struct {
		Update string `json:"update"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	update, err := base64.StdEncoding.DecodeString(req.Update)
	if err != nil || len(update) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "update must be base64")
		return
	}
	seeded := true
	if _, err := s.db.SeedDocumentCRDTUpdate(r.Context(), store.SeedDocumentCRDTUpdateParams{
		DocumentID: documentID, UpdateData: update,
	}); err != nil {
		seeded = false
	}
	s.appendCRDTEvent(r, documentID, req.Update, "")
	updates, err := s.db.ListDocumentCRDTUpdates(r.Context(), documentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load CRDT updates")
		return
	}
	encoded := make([]string, 0, len(updates))
	for _, item := range updates {
		encoded = append(encoded, base64.StdEncoding.EncodeToString(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"seeded": seeded, "updates": encoded})
}

func (s *Server) handlePushDocumentCRDT(w http.ResponseWriter, r *http.Request) {
	documentID, ok := parseUUIDParam(w, r, "documentID")
	if !ok {
		return
	}
	if _, ok := s.documentForEditor(w, r); !ok {
		return
	}
	var req struct {
		Update   string `json:"update"`
		ClientID string `json:"client_id"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	update, err := base64.StdEncoding.DecodeString(req.Update)
	if err != nil || len(update) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "update must be base64")
		return
	}
	if len(update) > 4<<20 {
		writeError(w, http.StatusBadRequest, "invalid_request", "update is too large")
		return
	}
	if _, err := s.db.CreateDocumentCRDTUpdate(r.Context(), store.CreateDocumentCRDTUpdateParams{DocumentID: documentID, UpdateData: update}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not store CRDT update")
		return
	}
	payload, eventID := s.appendCRDTEvent(r, documentID, req.Update, req.ClientID)
	s.collab.broadcast(documentID, map[string]any{
		"type": "crdt", "update": payload, "client_id": req.ClientID,
		"actor": userFrom(r).Email, "event_id": eventID,
	})
	writeNoContent(w)
}

// appendCRDTEvent persists the CRDT update in document_events so SSE pollers on
// other API instances deliver it too. Yjs updates are idempotent, so local hub
// delivery plus the persisted event is safe.
func (s *Server) appendCRDTEvent(r *http.Request, documentID uuid.UUID, update, clientID string) (string, int64) {
	payload, _ := json.Marshal(map[string]any{
		"type": "crdt", "update": update, "client_id": clientID,
	})
	event, err := s.db.CreateDocumentEvent(r.Context(), store.CreateDocumentEventParams{
		DocumentID: documentID, Payload: payload,
	})
	if err != nil {
		return update, 0
	}
	return update, event.ID
}
