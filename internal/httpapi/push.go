package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) pushRoutes(r chi.Router) {
	r.Get("/status", s.handlePushStatus)
	r.Post("/subscribe", s.handlePushSubscribe)
	r.Post("/unsubscribe", s.handlePushUnsubscribe)
	r.Post("/test", s.handlePushTest)
}

func (s *Server) handlePushStatus(w http.ResponseWriter, r *http.Request) {
	if s.push == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "reason": "push service failed to start"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": true,
		// The browser needs this key to create a subscription.
		"public_key": s.push.PublicKey(),
	})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.push == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "push service is not available")
		return
	}
	var req struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if _, err := s.push.Subscribe(r.Context(), accountFrom(r), userFrom(r).ID,
		req.Endpoint, req.Keys.P256dh, req.Keys.Auth, r.UserAgent()); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "subscribed"})
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if s.push == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "push service is not available")
		return
	}
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := s.push.Unsubscribe(r.Context(), userFrom(r).ID, req.Endpoint); err != nil {
		writeError(w, http.StatusNotFound, "not_found", strings.TrimSpace(err.Error()))
		return
	}
	writeNoContent(w)
}

func (s *Server) handlePushTest(w http.ResponseWriter, r *http.Request) {
	if s.push == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "push service is not available")
		return
	}
	sent, failed, lastErr := s.push.SendTest(r.Context(), accountFrom(r))
	if sent == 0 && failed > 0 && lastErr != nil {
		writeError(w, http.StatusBadGateway, "push_failed", lastErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent, "failed": failed})
}
