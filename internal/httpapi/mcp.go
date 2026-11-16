package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/store"
)

type mcpServerDTO struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Transport  string    `json:"transport"`
	URL        string    `json:"url"`
	Command    string    `json:"command"`
	Args       []string  `json:"args"`
	Status     string    `json:"status"`
	CreatedBy  string    `json:"created_by"`
	HasHeaders bool      `json:"has_headers"`
	HasEnv     bool      `json:"has_env"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func toMcpServerDTO(server store.McpServer) mcpServerDTO {
	args := []string{}
	if len(server.Args) > 0 {
		_ = json.Unmarshal(server.Args, &args)
	}
	return mcpServerDTO{
		ID:         server.ID,
		Name:       server.Name,
		Transport:  server.Transport,
		URL:        server.Url,
		Command:    server.Command,
		Args:       args,
		Status:     server.Status,
		CreatedBy:  server.CreatedBy,
		HasHeaders: len(server.HeadersEnc) > 0,
		HasEnv:     len(server.EnvEnc) > 0,
		CreatedAt:  server.CreatedAt,
		UpdatedAt:  server.UpdatedAt,
	}
}

func (s *Server) mcpRoutes(r chi.Router) {
	r.Get("/", s.handleListMcpServers)
	r.Post("/", s.handleCreateMcpServer)
	r.Post("/import", s.handleImportMcpServers)
	r.Route("/{serverID}", func(r chi.Router) {
		r.Post("/test", s.handleTestMcpServer)
		r.Post("/approve", s.handleApproveMcpServer)
		r.Post("/disable", s.handleDisableMcpServer)
		r.Delete("/", s.handleDeleteMcpServer)
	})
}

func (s *Server) handleListMcpServers(w http.ResponseWriter, r *http.Request) {
	items, err := s.mcp.List(r.Context(), accountFrom(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list mcp servers")
		return
	}
	out := make([]mcpServerDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toMcpServerDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"mcp_servers": out})
}

type mcpCreateRequest struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
}

func (s *Server) handleCreateMcpServer(w http.ResponseWriter, r *http.Request) {
	var req mcpCreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	// A stdio server is a command run on the worker host.
	if req.Transport == "stdio" && !s.requireManager(w, r) {
		return
	}
	var server store.McpServer
	var err error
	if req.Transport == "stdio" {
		server, err = s.mcp.CreateStdio(r.Context(), accountFrom(r), req.Name, req.Command, req.Args, req.Env, "user")
	} else {
		server, err = s.mcp.Create(r.Context(), accountFrom(r), req.Name, req.URL, req.Headers, "user")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"mcp_server": toMcpServerDTO(server)})
}

func (s *Server) handleImportMcpServers(w http.ResponseWriter, r *http.Request) {
	var req struct {
		McpServers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	created := make([]mcpServerDTO, 0, len(req.McpServers))
	for name, item := range req.McpServers {
		server, err := s.mcp.Create(r.Context(), accountFrom(r), name, item.URL, item.Headers, "user")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		created = append(created, toMcpServerDTO(server))
	}
	writeJSON(w, http.StatusCreated, map[string]any{"mcp_servers": created})
}

func (s *Server) handleTestMcpServer(w http.ResponseWriter, r *http.Request) {
	serverID, ok := parseUUIDParam(w, r, "serverID")
	if !ok {
		return
	}
	toolsList, err := s.mcp.Test(r.Context(), accountFrom(r), serverID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "mcp_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tools": toolsList})
}

func (s *Server) handleApproveMcpServer(w http.ResponseWriter, r *http.Request) {
	serverID, ok := parseUUIDParam(w, r, "serverID")
	if !ok {
		return
	}
	if !s.requireManager(w, r) {
		return
	}
	server, err := s.mcp.SetStatus(r.Context(), accountFrom(r), serverID, "active")
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "mcp server not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mcp_server": toMcpServerDTO(server)})
}

func (s *Server) handleDisableMcpServer(w http.ResponseWriter, r *http.Request) {
	serverID, ok := parseUUIDParam(w, r, "serverID")
	if !ok {
		return
	}
	server, err := s.mcp.SetStatus(r.Context(), accountFrom(r), serverID, "disabled")
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "mcp server not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mcp_server": toMcpServerDTO(server)})
}

func (s *Server) handleDeleteMcpServer(w http.ResponseWriter, r *http.Request) {
	serverID, ok := parseUUIDParam(w, r, "serverID")
	if !ok {
		return
	}
	if err := s.mcp.Delete(r.Context(), accountFrom(r), serverID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "mcp server not found")
		return
	}
	writeNoContent(w)
}

type botMcpDTO struct {
	McpServer mcpServerDTO `json:"mcp_server"`
	Enabled   bool         `json:"enabled"`
}

func (s *Server) handleListBotMcpServers(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountFrom(r)}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	rows, err := s.db.ListBotMcpServers(r.Context(), botID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list bot mcp servers")
		return
	}
	out := make([]botMcpDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, botMcpDTO{McpServer: toMcpServerDTO(row.McpServer), Enabled: row.Enabled})
	}
	writeJSON(w, http.StatusOK, map[string]any{"mcp_servers": out})
}

func (s *Server) handleSetBotMcpServers(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	var req struct {
		McpServerIDs []uuid.UUID `json:"mcp_server_ids"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := s.mcp.SetBotServers(r.Context(), accountFrom(r), botID, req.McpServerIDs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	rows, _ := s.db.ListBotMcpServers(r.Context(), botID)
	out := make([]botMcpDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, botMcpDTO{McpServer: toMcpServerDTO(row.McpServer), Enabled: row.Enabled})
	}
	writeJSON(w, http.StatusOK, map[string]any{"mcp_servers": out})
}

// requireManager allows the request only for the account's owner or admins.
func (s *Server) requireManager(w http.ResponseWriter, r *http.Request) bool {
	role, err := s.accountRole(r.Context(), accountFrom(r), userFrom(r).ID)
	if err != nil || (role != "owner" && role != "admin") {
		writeError(w, http.StatusForbidden, "forbidden", "owner or admin required")
		return false
	}
	return true
}
