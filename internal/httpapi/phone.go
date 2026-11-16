package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"superbot/internal/phone"
)

type phoneSendRequest struct {
	To      string `json:"to"`
	Message string `json:"message"`
}

func (s *Server) phoneService() *phone.Service {
	return phone.New(phone.Config{
		AccountSID: s.cfg.TwilioSID,
		AuthToken:  s.cfg.TwilioToken,
		FromNumber: s.cfg.TwilioFrom,
	})
}

func (s *Server) phoneRoutes(r chi.Router) {
	r.Get("/status", s.handlePhoneStatus)
	r.Post("/sms", s.handleSendSMS)
	r.Post("/call", s.handlePhoneCall)
}

func (s *Server) handlePhoneStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.phoneService().Status())
}

func (s *Server) handleSendSMS(w http.ResponseWriter, r *http.Request) {
	var req phoneSendRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	to := strings.TrimSpace(req.To)
	message := strings.TrimSpace(req.Message)
	if !validPhoneNumber(to) {
		writeError(w, http.StatusBadRequest, "invalid_request", "to must be an E.164 phone number")
		return
	}
	if message == "" || len([]rune(message)) > 1600 {
		writeError(w, http.StatusBadRequest, "invalid_request", "message must be 1-1600 characters")
		return
	}
	sid, err := s.phoneService().SendSMS(r.Context(), to, message)
	if err != nil {
		phoneError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sid": sid, "status": "queued"})
}

func (s *Server) handlePhoneCall(w http.ResponseWriter, r *http.Request) {
	var req phoneSendRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	to := strings.TrimSpace(req.To)
	message := strings.TrimSpace(req.Message)
	if !validPhoneNumber(to) {
		writeError(w, http.StatusBadRequest, "invalid_request", "to must be an E.164 phone number")
		return
	}
	if message == "" || len([]rune(message)) > 600 {
		writeError(w, http.StatusBadRequest, "invalid_request", "message must be 1-600 characters")
		return
	}
	sid, err := s.phoneService().Call(r.Context(), to, message)
	if err != nil {
		phoneError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sid": sid, "status": "queued"})
}

func phoneError(w http.ResponseWriter, err error) {
	if errors.Is(err, phone.ErrNotConfigured) {
		writeError(w, http.StatusServiceUnavailable, "requires_config", "phone provider is not configured")
		return
	}
	writeError(w, http.StatusBadGateway, "phone_failed", err.Error())
}

func validPhoneNumber(value string) bool {
	if len(value) < 8 || len(value) > 20 || value[0] != '+' {
		return false
	}
	for _, r := range value[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
