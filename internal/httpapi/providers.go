package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/botmodel"
	"superbot/internal/llm"
	"superbot/internal/store"
)

func (s *Server) providerRoutes(r chi.Router) {
	r.Get("/", s.handleListProviders)
	r.Post("/", s.handleCreateProvider)
	r.Route("/{providerID}", func(r chi.Router) {
		r.Get("/", s.handleGetProvider)
		r.Patch("/", s.handleUpdateProvider)
		r.Delete("/", s.handleDeleteProvider)
		r.Post("/test", s.handleTestProvider)
		r.Get("/models", s.handleListModels)
		r.Post("/models", s.handleCreateModel)
		r.Delete("/models/{modelID}", s.handleDeleteModel)
	})
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	accountID := accountFrom(r)
	providers, err := s.db.ListProviders(r.Context(), accountID)
	if err != nil {
		s.logger.Error("list providers failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not list providers")
		return
	}
	out := make([]providerDTO, 0, len(providers))
	for _, p := range providers {
		out = append(out, toProviderDTO(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out})
}

type providerRequest struct {
	Type    string          `json:"type"`
	Kind    string          `json:"kind"`
	Name    string          `json:"name"`
	BaseURL string          `json:"base_url"`
	APIKey  string          `json:"api_key"`
	Config  json.RawMessage `json:"config"`
	Enabled *bool           `json:"enabled"`
}

func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	var req providerRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.Type = strings.TrimSpace(req.Type)
	req.Kind = strings.TrimSpace(req.Kind)
	req.Name = strings.TrimSpace(req.Name)
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	if req.Type == "" || req.Kind == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "type, kind and name are required")
		return
	}
	switch req.Type {
	case "llm", "embedding", "search":
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "type must be llm, embedding or search")
		return
	}
	if req.Kind == "openai_compat" && req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "base_url is required for openai_compat providers")
		return
	}
	config := req.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	var keyEnc []byte
	if strings.TrimSpace(req.APIKey) != "" {
		var err error
		keyEnc, err = s.box.EncryptString(strings.TrimSpace(req.APIKey))
		if err != nil {
			s.logger.Error("encrypt provider key failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "could not store provider key")
			return
		}
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	provider, err := s.db.CreateProvider(r.Context(), store.CreateProviderParams{
		OwnerID:   accountFrom(r),
		Type:      req.Type,
		Kind:      req.Kind,
		Name:      req.Name,
		BaseUrl:   req.BaseURL,
		ApiKeyEnc: keyEnc,
		Config:    config,
		Enabled:   enabled,
	})
	if err != nil {
		s.logger.Error("create provider failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not create provider")
		return
	}
	s.repairBotModels(r)
	writeJSON(w, http.StatusCreated, map[string]any{"provider": toProviderDTO(provider)})
}

func (s *Server) handleGetProvider(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "providerID")
	if !ok {
		return
	}
	provider, err := s.db.GetProvider(r.Context(), store.GetProviderParams{ID: providerID, OwnerID: accountFrom(r)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "provider not found")
			return
		}
		s.logger.Error("get provider failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not load provider")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": toProviderDTO(provider)})
}

type providerUpdateRequest struct {
	Name     *string          `json:"name"`
	BaseURL  *string          `json:"base_url"`
	APIKey   *string          `json:"api_key"`
	ClearKey bool             `json:"clear_api_key"`
	Config   *json.RawMessage `json:"config"`
	Enabled  *bool            `json:"enabled"`
}

func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "providerID")
	if !ok {
		return
	}
	var req providerUpdateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	existing, err := s.db.GetProvider(r.Context(), store.GetProviderParams{ID: providerID, OwnerID: accountFrom(r)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "provider not found")
			return
		}
		s.logger.Error("get provider failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not load provider")
		return
	}
	if req.Name != nil {
		existing.Name = strings.TrimSpace(*req.Name)
	}
	if req.BaseURL != nil {
		existing.BaseUrl = strings.TrimSpace(*req.BaseURL)
	}
	if req.Config != nil {
		if len(*req.Config) == 0 {
			existing.Config = json.RawMessage(`{}`)
		} else {
			existing.Config = *req.Config
		}
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.ClearKey {
		existing.ApiKeyEnc = nil
	} else if req.APIKey != nil && strings.TrimSpace(*req.APIKey) != "" {
		enc, err := s.box.EncryptString(strings.TrimSpace(*req.APIKey))
		if err != nil {
			s.logger.Error("encrypt provider key failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "could not store provider key")
			return
		}
		existing.ApiKeyEnc = enc
	}
	if existing.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	updated, err := s.db.UpdateProvider(r.Context(), store.UpdateProviderParams{
		ID:        existing.ID,
		OwnerID:   existing.OwnerID,
		Name:      existing.Name,
		BaseUrl:   existing.BaseUrl,
		ApiKeyEnc: existing.ApiKeyEnc,
		Config:    existing.Config,
		Enabled:   existing.Enabled,
	})
	if err != nil {
		s.logger.Error("update provider failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not update provider")
		return
	}
	s.repairBotModels(r)
	writeJSON(w, http.StatusOK, map[string]any{"provider": toProviderDTO(updated)})
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "providerID")
	if !ok {
		return
	}
	n, err := s.db.DeleteProvider(r.Context(), store.DeleteProviderParams{ID: providerID, OwnerID: accountFrom(r)})
	if err != nil {
		s.logger.Error("delete provider failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not delete provider")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "provider not found")
		return
	}
	s.repairBotModels(r)
	writeNoContent(w)
}

func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "providerID")
	if !ok {
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if err := decodeJSON(w, r, &req); err != nil && !errors.Is(err, errEmptyBody) {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	provider, err := s.db.GetProvider(r.Context(), store.GetProviderParams{ID: providerID, OwnerID: accountFrom(r)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "provider not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load provider")
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		models, err := s.db.ListModelsByProvider(r.Context(), provider.ID)
		if err == nil && len(models) > 0 {
			model = models[0].Name
		}
	}
	if model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "model is required (or add a model first)")
		return
	}
	built, err := s.factory.Build(provider, 30*time.Second)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_provider", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	stream, err := built.Stream(ctx, llm.Request{
		Model:     model,
		Messages:  []llm.Message{llm.TextMessage(llm.RoleUser, "ping. Reply with exactly: OK")},
		MaxTokens: 128,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "provider_error", err.Error())
		return
	}
	defer stream.Close()
	var reply strings.Builder
	var streamErr error
	for ev := range stream.Events() {
		switch ev.Type {
		case llm.EventTextDelta:
			reply.WriteString(ev.Text)
		case llm.EventError:
			streamErr = ev.Err
		}
	}
	if streamErr != nil {
		writeError(w, http.StatusBadGateway, "provider_error", streamErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"model": model,
		"reply": strings.TrimSpace(reply.String()),
	})
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "providerID")
	if !ok {
		return
	}
	if _, err := s.db.GetProvider(r.Context(), store.GetProviderParams{ID: providerID, OwnerID: accountFrom(r)}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "provider not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load provider")
		return
	}
	models, err := s.db.ListModelsByProvider(r.Context(), providerID)
	if err != nil {
		s.logger.Error("list models failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not list models")
		return
	}
	out := make([]modelDTO, 0, len(models))
	for _, m := range models {
		out = append(out, toModelDTO(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": out})
}

type modelRequest struct {
	Name                    string `json:"name"`
	ContextWindow           int32  `json:"context_window"`
	MaxOutput               int32  `json:"max_output"`
	SupportsTools           *bool  `json:"supports_tools"`
	SupportsVision          *bool  `json:"supports_vision"`
	SupportsReasoning       *bool  `json:"supports_reasoning"`
	PriceInMicrosPerMtok    int64  `json:"price_in_micros_per_mtok"`
	PriceCacheMicrosPerMtok int64  `json:"price_cache_micros_per_mtok"`
	PriceOutMicrosPerMtok   int64  `json:"price_out_micros_per_mtok"`
}

func (s *Server) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "providerID")
	if !ok {
		return
	}
	if _, err := s.db.GetProvider(r.Context(), store.GetProviderParams{ID: providerID, OwnerID: accountFrom(r)}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "provider not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load provider")
		return
	}
	var req modelRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "model name is required")
		return
	}
	if req.ContextWindow < 0 || req.MaxOutput < 0 || req.PriceInMicrosPerMtok < 0 || req.PriceCacheMicrosPerMtok < 0 || req.PriceOutMicrosPerMtok < 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "numeric fields must not be negative")
		return
	}
	supportsTools := true
	if req.SupportsTools != nil {
		supportsTools = *req.SupportsTools
	}
	model, err := s.db.CreateModel(r.Context(), store.CreateModelParams{
		ProviderID:              providerID,
		Name:                    req.Name,
		ContextWindow:           req.ContextWindow,
		MaxOutput:               req.MaxOutput,
		SupportsTools:           supportsTools,
		SupportsVision:          req.SupportsVision != nil && *req.SupportsVision,
		SupportsReasoning:       req.SupportsReasoning != nil && *req.SupportsReasoning,
		PriceInMicrosPerMtok:    req.PriceInMicrosPerMtok,
		PriceCacheMicrosPerMtok: req.PriceCacheMicrosPerMtok,
		PriceOutMicrosPerMtok:   req.PriceOutMicrosPerMtok,
	})
	if err != nil {
		s.logger.Error("create model failed", "error", err)
		writeError(w, http.StatusConflict, "conflict", "model already exists for this provider")
		return
	}
	s.repairBotModels(r)
	writeJSON(w, http.StatusCreated, map[string]any{"model": toModelDTO(model)})
}

func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "providerID")
	if !ok {
		return
	}
	modelID, ok := parseUUIDParam(w, r, "modelID")
	if !ok {
		return
	}
	if _, err := s.db.GetProvider(r.Context(), store.GetProviderParams{ID: providerID, OwnerID: accountFrom(r)}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "provider not found")
		return
	}
	n, err := s.db.DeleteModel(r.Context(), store.DeleteModelParams{ID: modelID, ProviderID: providerID})
	if err != nil {
		s.logger.Error("delete model failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not delete model")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "model not found")
		return
	}
	s.repairBotModels(r)
	writeNoContent(w)
}

var _ = errors.New

func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	raw := chi.URLParam(r, name)
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("invalid %s", name))
		return uuid.Nil, false
	}
	return id, true
}

// repairBotModels moves bots whose provider or model just went away to a
// usable one (botmodel.Repair), so changing providers never leaves bots that
// fail on every turn.
func (s *Server) repairBotModels(r *http.Request) {
	if _, err := botmodel.Repair(r.Context(), s.db, accountFrom(r), s.logger); err != nil {
		s.logger.Warn("repair bot models failed", "error", err)
	}
}
