package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/connectors"
	"superbot/internal/store"
)

type connectorDTO struct {
	ID        uuid.UUID       `json:"id"`
	Provider  string          `json:"provider"`
	Name      string          `json:"name"`
	AuthType  string          `json:"auth_type"`
	Status    string          `json:"status"`
	Config    json.RawMessage `json:"config"`
	ExpiresAt *time.Time      `json:"expires_at"`
	CreatedAt time.Time       `json:"created_at"`
}

func toConnectorDTO(item store.Connector) connectorDTO {
	config := item.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	return connectorDTO{
		ID: item.ID, Provider: item.Provider, Name: item.Name,
		AuthType: item.AuthType, Status: item.Status, Config: config,
		ExpiresAt: item.ExpiresAt, CreatedAt: item.CreatedAt,
	}
}

func (s *Server) connectorRoutes(r chi.Router) {
	r.Get("/", s.handleListConnectors)
	r.Post("/", s.handleCreateConnector)
	r.Post("/github/device/start", s.handleGitHubDeviceStart)
	r.Post("/github/device/poll", s.handleGitHubDevicePoll)
	r.Get("/oauth/status", s.handleOAuthStatus)
	r.Get("/oauth/{provider}/start", s.handleOAuthStart)
	r.Get("/oauth/{provider}/callback", s.handleOAuthCallback)
	r.Route("/{connectorID}", func(r chi.Router) {
		r.Post("/test", s.handleTestConnector)
		r.Delete("/", s.handleDeleteConnector)
	})
}

func (s *Server) handleListConnectors(w http.ResponseWriter, r *http.Request) {
	items, err := s.connectors.List(r.Context(), accountFrom(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list connectors")
		return
	}
	out := make([]connectorDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toConnectorDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"connectors": out})
}

type connectorCreateRequest struct {
	Provider string          `json:"provider"`
	Name     string          `json:"name"`
	Token    string          `json:"token"`
	Config   json.RawMessage `json:"config"`
}

func (s *Server) handleCreateConnector(w http.ResponseWriter, r *http.Request) {
	var req connectorCreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	item, err := s.connectors.CreateToken(r.Context(), accountFrom(r), connectors.Input{
		Provider: req.Provider, Name: req.Name, Token: req.Token, Config: req.Config,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"connector": toConnectorDTO(item)})
}

func (s *Server) handleTestConnector(w http.ResponseWriter, r *http.Request) {
	connectorID, ok := parseUUIDParam(w, r, "connectorID")
	if !ok {
		return
	}
	identity, err := s.connectors.Test(r.Context(), accountFrom(r), connectorID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "connector_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "identity": identity})
}

func (s *Server) handleDeleteConnector(w http.ResponseWriter, r *http.Request) {
	connectorID, ok := parseUUIDParam(w, r, "connectorID")
	if !ok {
		return
	}
	connector, err := s.connectors.Get(r.Context(), accountFrom(r), connectorID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "connector not found")
		return
	}
	if err := s.connectors.Revoke(r.Context(), connector); err != nil {
		s.logger.Warn("connector revoke failed; deleting local token anyway",
			"provider", connector.Provider, "error", err)
	}
	if err := s.connectors.Delete(r.Context(), accountFrom(r), connectorID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "connector not found")
		return
	}
	writeNoContent(w)
}

type githubDeviceStartRequest struct {
	ClientID string `json:"client_id"`
}

func (s *Server) handleGitHubDeviceStart(w http.ResponseWriter, r *http.Request) {
	var req githubDeviceStartRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	start, err := s.connectors.StartGitHubDevice(r.Context(), req.ClientID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "github_device_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device_code": start.DeviceCode, "user_code": start.UserCode,
		"verification_uri": start.VerificationURI, "expires_in": start.ExpiresIn,
		"interval": start.Interval,
	})
}

type githubDevicePollRequest struct {
	ClientID   string `json:"client_id"`
	DeviceCode string `json:"device_code"`
}

func (s *Server) handleGitHubDevicePoll(w http.ResponseWriter, r *http.Request) {
	var req githubDevicePollRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	token, pending, err := s.connectors.PollGitHubDevice(r.Context(), req.ClientID, req.DeviceCode)
	if err != nil {
		writeError(w, http.StatusBadGateway, "github_device_failed", err.Error())
		return
	}
	if pending {
		writeJSON(w, http.StatusOK, map[string]any{"pending": true})
		return
	}
	item, err := s.connectors.CreateGitHubOAuth(r.Context(), accountFrom(r), token)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save GitHub connector")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pending": false, "connector": toConnectorDTO(item)})
}
