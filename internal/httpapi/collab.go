package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/store"
)

type collabSubscriber struct {
	id     string
	events chan []byte
}

type collabHub struct {
	mu    sync.Mutex
	rooms map[uuid.UUID]map[*collabSubscriber]struct{}
}

func newCollabHub() *collabHub {
	return &collabHub{rooms: make(map[uuid.UUID]map[*collabSubscriber]struct{})}
}

func (h *collabHub) join(documentID uuid.UUID, clientID string) *collabSubscriber {
	h.mu.Lock()
	defer h.mu.Unlock()
	sub := &collabSubscriber{id: clientID, events: make(chan []byte, 32)}
	if h.rooms[documentID] == nil {
		h.rooms[documentID] = make(map[*collabSubscriber]struct{})
	}
	h.rooms[documentID][sub] = struct{}{}
	return sub
}

func (h *collabHub) leave(documentID uuid.UUID, sub *collabSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if room := h.rooms[documentID]; room != nil {
		delete(room, sub)
		if len(room) == 0 {
			delete(h.rooms, documentID)
		}
	}
	close(sub.events)
}

func (h *collabHub) count(documentID uuid.UUID) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms[documentID])
}

func (h *collabHub) broadcast(documentID uuid.UUID, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.rooms[documentID] {
		select {
		case sub.events <- data:
		default:
		}
	}
}

func (s *Server) handleDocumentEvents(w http.ResponseWriter, r *http.Request) {
	documentID, ok := parseUUIDParam(w, r, "documentID")
	if !ok {
		return
	}
	if _, ok := s.documentForAccess(w, r, false); !ok {
		return
	}
	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		clientID = uuid.NewString()
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	lastEventID, _ := s.db.GetLatestDocumentEventID(r.Context(), documentID)
	_ = s.db.UpsertDocumentPresence(r.Context(), store.UpsertDocumentPresenceParams{
		DocumentID: documentID, ClientID: clientID, Actor: userFrom(r).Email,
	})
	defer s.db.DeleteDocumentPresence(context.WithoutCancel(r.Context()), store.DeleteDocumentPresenceParams{
		DocumentID: documentID, ClientID: clientID,
	})
	sub := s.collab.join(documentID, clientID)
	defer s.collab.leave(documentID, sub)
	s.collab.broadcast(documentID, map[string]any{"type": "presence", "count": s.collab.count(documentID)})
	heartbeat := time.NewTicker(20 * time.Second)
	poll := time.NewTicker(time.Second)
	defer heartbeat.Stop()
	defer poll.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case data, ok := <-sub.events:
			if !ok {
				return
			}
			var envelope struct {
				EventID int64 `json:"event_id"`
			}
			if json.Unmarshal(data, &envelope) == nil && envelope.EventID > lastEventID {
				lastEventID = envelope.EventID
			}
			_, _ = w.Write([]byte("data: " + string(data) + "\n\n"))
			flusher.Flush()
		case <-poll.C:
			presence, presenceErr := s.db.ListDocumentPresence(r.Context(), documentID)
			if presenceErr == nil {
				cursors := make([]map[string]any, 0, len(presence))
				for _, item := range presence {
					cursors = append(cursors, map[string]any{
						"id": item.ClientID, "actor": item.Actor, "x": item.X, "y": item.Y,
					})
				}
				state, _ := json.Marshal(map[string]any{"type": "presence_state", "cursors": cursors, "count": len(cursors)})
				_, _ = w.Write([]byte("data: " + string(state) + "\n\n"))
				flusher.Flush()
			}
			rows, err := s.db.ListDocumentEventsAfter(r.Context(), store.ListDocumentEventsAfterParams{
				DocumentID: documentID, ID: lastEventID,
			})
			if err != nil {
				continue
			}
			for _, row := range rows {
				_, _ = w.Write([]byte("id: " + strconv.FormatInt(row.ID, 10) + "\ndata: " + string(row.Payload) + "\n\n"))
				lastEventID = row.ID
			}
			if len(rows) > 0 {
				flusher.Flush()
			}
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

func (s *Server) handleDocumentCollab(w http.ResponseWriter, r *http.Request) {
	documentID, ok := parseUUIDParam(w, r, "documentID")
	if !ok {
		return
	}
	existing, ok := s.documentForEditor(w, r)
	if !ok {
		return
	}
	var req struct {
		Title        string `json:"title"`
		Content      string `json:"content"`
		Kind         string `json:"kind"`
		ClientID     string `json:"client_id"`
		BaseRevision int64  `json:"base_revision"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.BaseRevision > 0 && req.BaseRevision != existing.Revision {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":    map[string]string{"code": "conflict", "message": "document changed since base revision"},
			"document": toDocumentDTO(existing),
		})
		return
	}
	title := existing.Title
	if len(req.Title) > 0 {
		title = req.Title
	}
	kind := existing.Kind
	if req.Kind == "canvas" || req.Kind == "document" {
		kind = req.Kind
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
	payload := map[string]any{
		"type": "update", "client_id": req.ClientID,
		"document": toDocumentDTO(doc), "actor": userFrom(r).Email,
		"count": s.collab.count(documentID), "at": time.Now(),
	}
	encoded, _ := json.Marshal(payload)
	event, _ := s.db.CreateDocumentEvent(r.Context(), store.CreateDocumentEventParams{
		DocumentID: documentID, Payload: encoded,
	})
	payload["event_id"] = event.ID
	s.collab.broadcast(documentID, payload)
	writeJSON(w, http.StatusOK, map[string]any{"document": toDocumentDTO(doc)})
}

func (s *Server) collabRoutes(r chi.Router) {
	r.Get("/{documentID}/events", s.handleDocumentEvents)
	r.Post("/{documentID}/collab", s.handleDocumentCollab)
}

func (s *Server) handleDocumentCursor(w http.ResponseWriter, r *http.Request) {
	documentID, ok := parseUUIDParam(w, r, "documentID")
	if !ok {
		return
	}
	if _, ok := s.documentForAccess(w, r, false); !ok {
		return
	}
	var req struct {
		ClientID string  `json:"client_id"`
		X        float64 `json:"x"`
		Y        float64 `json:"y"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	_ = s.db.UpsertDocumentPresence(r.Context(), store.UpsertDocumentPresenceParams{
		DocumentID: documentID, ClientID: req.ClientID, Actor: userFrom(r).Email, X: req.X, Y: req.Y,
	})
	s.collab.broadcast(documentID, map[string]any{
		"type": "cursor", "client_id": req.ClientID, "actor": userFrom(r).Email,
		"x": req.X, "y": req.Y, "at": time.Now(),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
