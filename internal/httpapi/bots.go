package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"superbot/internal/store"
)

func (s *Server) botRoutes(r chi.Router) {
	r.Get("/", s.handleListBots)
	r.Post("/", s.handleCreateBot)
	r.Route("/{botID}", func(r chi.Router) {
		r.Get("/", s.handleGetBot)
		r.Patch("/", s.handleUpdateBot)
		r.Patch("/profile", s.handleUpdateBotProfile)
		r.Get("/avatar", s.handleGetBotAvatar)
		r.Post("/avatar", s.handleUploadBotAvatar)
		r.Post("/avatar/generate", s.handleGenerateBotAvatar)
		r.Delete("/avatar", s.handleDeleteBotAvatar)
		r.Patch("/hidden", s.handleSetBotHidden)
		r.Post("/share", s.handleCreateBotShare)
		r.Get("/shares", s.handleListBotShares)
		r.Delete("/shares/{token}", s.handleRevokeBotShare)
		r.Delete("/", s.handleDeleteBot)
		r.Get("/skills", s.handleListBotSkills)
		r.Put("/skills", s.handleSetBotSkills)
		r.Get("/mcp", s.handleListBotMcpServers)
		r.Put("/mcp", s.handleSetBotMcpServers)
		r.Get("/library", s.handleListBotLibrary)
		r.Get("/files", s.handleListBotFiles)
		r.Get("/files/content", s.handleBotFileContent)
		r.Post("/library/files", s.handleUploadLibraryFile)
		r.Get("/template", s.handleExportBotTemplate)
		r.Get("/acl", s.handleListBotACL)
		r.Put("/acl", s.handleSetBotACL)
		r.Delete("/acl/{userID}", s.handleDeleteBotACL)
		r.Get("/memories", s.handleListBotMemories)
		r.Post("/memories/import", s.handleImportBotMemories)
		r.Post("/memories/tidy", s.handleTidyBotMemories)
		r.Post("/memories/{memoryID}/pin", s.handlePinBotMemory)
		r.Post("/memories/{memoryID}/restore", s.handleRestoreBotMemory)
		r.Put("/memories/{memoryID}", s.handleUpdateBotMemory)
		r.Delete("/memories/{memoryID}", s.handleDeleteBotMemory)
		r.Patch("/proactivity", s.handleSetBotProactivity)
		r.Get("/service-tokens", s.handleListBotServiceTokens)
		r.Post("/service-tokens", s.handleCreateBotServiceToken)
		r.Delete("/service-tokens/{tokenID}", s.handleRevokeBotServiceToken)
		r.Route("/computer", s.botComputerRoutes)
	})
}

type botRequest struct {
	Name            string          `json:"name"`
	Slug            string          `json:"slug"`
	Avatar          string          `json:"avatar"`
	Description     string          `json:"description"`
	SystemPrompt    string          `json:"system_prompt"`
	ProviderID      *uuid.UUID      `json:"provider_id"`
	Model           string          `json:"model"`
	Params          json.RawMessage `json:"params"`
	Capabilities    json.RawMessage `json:"capabilities"`
	ToolPolicy      json.RawMessage `json:"tool_policy"`
	MemoryEnabled   *bool           `json:"memory_enabled"`
	ComputerProfile string          `json:"computer_profile"`
	Title           string          `json:"title"`
	AvatarShape     string          `json:"avatar_shape"`
	AvatarColor     string          `json:"avatar_color"`
	SectionID       *uuid.UUID      `json:"section_id"`
	Position        *int32          `json:"position"`
	Visibility      string          `json:"visibility"`
	Kind            string          `json:"kind"`
}

func (s *Server) botAccess(ctx context.Context, role string, userID uuid.UUID, bot store.Bot) (string, bool) {
	if role == "owner" || role == "admin" {
		return "editor", true
	}
	if bot.CreatedByUserID != nil && *bot.CreatedByUserID == userID {
		return "editor", true
	}
	if acl, err := s.db.GetResourceACLRole(ctx, store.GetResourceACLRoleParams{
		ResourceType: "bot", ResourceID: bot.ID, UserID: &userID,
	}); err == nil {
		return acl, true
	}
	if bot.Visibility == "owner" {
		return "", false
	}
	return "viewer", true
}

func (s *Server) handleListBots(w http.ResponseWriter, r *http.Request) {
	bots, err := s.db.ListBots(r.Context(), accountFrom(r))
	if err != nil {
		s.logger.Error("list bots failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not list bots")
		return
	}
	role, _ := s.accountRole(r.Context(), accountFrom(r), userFrom(r).ID)
	out := make([]botDTO, 0, len(bots))
	for _, b := range bots {
		if _, ok := s.botAccess(r.Context(), role, userFrom(r).ID, b); !ok {
			continue
		}
		out = append(out, toBotDTO(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"bots": out})
}

func (s *Server) handleCreateBot(w http.ResponseWriter, r *http.Request) {
	var req botRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	user := userFrom(r)
	accountID := accountFrom(r)
	if err := s.validateBotRequest(r, accountID, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = slugify(req.Name)
	}
	memoryEnabled := true
	if req.MemoryEnabled != nil {
		memoryEnabled = *req.MemoryEnabled
	}
	kind := req.Kind
	if kind != "team" {
		kind = "personal"
	}
	bot, err := s.db.CreateBot(r.Context(), store.CreateBotParams{
		OwnerID:         accountID,
		Name:            req.Name,
		Slug:            slug,
		Avatar:          req.Avatar,
		Description:     req.Description,
		SystemPrompt:    req.SystemPrompt,
		ProviderID:      req.ProviderID,
		Model:           req.Model,
		Params:          rawOrEmptyObject(req.Params),
		Capabilities:    rawOrEmptyObject(req.Capabilities),
		ToolPolicy:      rawOrEmptyObject(req.ToolPolicy),
		MemoryEnabled:   memoryEnabled,
		ComputerProfile: req.ComputerProfile,
		Title:           req.Title,
		AvatarShape:     req.AvatarShape,
		AvatarColor:     req.AvatarColor,
		SectionID:       req.SectionID,
		Position:        positionOrZero(req.Position),
		Kind:            kind,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "conflict", "a bot with this slug already exists")
			return
		}
		s.logger.Error("create bot failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not create bot")
		return
	}
	visibility := req.Visibility
	if kind == "team" {
		visibility = "account"
	} else if visibility != "owner" {
		visibility = "account"
	}
	if err := s.db.SetBotAccess(r.Context(), store.SetBotAccessParams{
		ID: bot.ID, OwnerID: accountID, Visibility: visibility, CreatedByUserID: &user.ID,
	}); err != nil {
		s.logger.Warn("set bot access failed", "bot_id", bot.ID, "error", err)
	} else {
		bot.Visibility = visibility
		bot.CreatedByUserID = &user.ID
	}
	if kind == "personal" {
		account, _ := s.db.GetAccountByID(r.Context(), accountID)
		if account.PrimaryBotID == nil {
			if err := s.db.SetAccountPrimaryBot(r.Context(), store.SetAccountPrimaryBotParams{
				ID: accountID, PrimaryBotID: &bot.ID,
			}); err != nil {
				s.logger.Warn("auto-set account primary bot failed", "error", err, "account_id", accountID, "bot_id", bot.ID)
			}
		}
		if user.PrimaryBotID == nil {
			if err := s.db.SetPrimaryBot(r.Context(), store.SetPrimaryBotParams{
				ID:           user.ID,
				PrimaryBotID: &bot.ID,
			}); err != nil {
				s.logger.Warn("auto-set user primary bot failed", "error", err, "user_id", user.ID, "bot_id", bot.ID)
			}
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"bot": toBotDTO(bot)})
}

func (s *Server) handleGetBot(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountFrom(r)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "bot not found")
			return
		}
		s.logger.Error("get bot failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not load bot")
		return
	}
	role, _ := s.accountRole(r.Context(), accountFrom(r), userFrom(r).ID)
	if _, ok := s.botAccess(r.Context(), role, userFrom(r).ID, bot); !ok {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bot": toBotDTO(bot)})
}

func (s *Server) handleUpdateBot(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	existing, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "bot not found")
			return
		}
		s.logger.Error("get bot failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not load bot")
		return
	}
	role, _ := s.accountRole(r.Context(), accountID, userFrom(r).ID)
	if access, ok := s.botAccess(r.Context(), role, userFrom(r).ID, existing); !ok || access != "editor" {
		writeError(w, http.StatusForbidden, "forbidden", "bot is read-only for this user")
		return
	}
	var req botRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := s.validateBotRequest(r, accountID, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(req.Slug) == "" {
		req.Slug = existing.Slug
	}
	memoryEnabled := existing.MemoryEnabled
	if req.MemoryEnabled != nil {
		memoryEnabled = *req.MemoryEnabled
	}
	bot, err := s.db.UpdateBot(r.Context(), store.UpdateBotParams{
		ID:              existing.ID,
		OwnerID:         accountID,
		Name:            req.Name,
		Slug:            req.Slug,
		Avatar:          req.Avatar,
		Description:     req.Description,
		SystemPrompt:    req.SystemPrompt,
		ProviderID:      req.ProviderID,
		Model:           req.Model,
		Params:          rawOrEmptyObject(req.Params),
		Capabilities:    rawOrEmptyObject(req.Capabilities),
		ToolPolicy:      rawOrEmptyObject(req.ToolPolicy),
		MemoryEnabled:   memoryEnabled,
		ComputerProfile: req.ComputerProfile,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "conflict", "a bot with this slug already exists")
			return
		}
		s.logger.Error("update bot failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not update bot")
		return
	}
	if req.Visibility == "owner" || req.Visibility == "account" {
		if err := s.db.SetBotAccess(r.Context(), store.SetBotAccessParams{
			ID: bot.ID, OwnerID: accountID, Visibility: req.Visibility, CreatedByUserID: nil,
		}); err != nil {
			s.logger.Warn("set bot visibility failed", "bot_id", bot.ID, "error", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"bot": toBotDTO(bot)})
}

type botProfileRequest struct {
	Name        string     `json:"name"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Avatar      string     `json:"avatar"`
	AvatarShape string     `json:"avatar_shape"`
	AvatarColor string     `json:"avatar_color"`
	SectionID   *uuid.UUID `json:"section_id"`
	Position    *int32     `json:"position"`
	Pinned      *bool      `json:"pinned"`
	Visibility  string     `json:"visibility"`
}

func (s *Server) handleUpdateBotProfile(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	existing, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "bot not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load bot")
		return
	}
	role, _ := s.accountRole(r.Context(), accountID, userFrom(r).ID)
	if access, ok := s.botAccess(r.Context(), role, userFrom(r).ID, existing); !ok || access != "editor" {
		writeError(w, http.StatusForbidden, "forbidden", "bot is read-only for this user")
		return
	}
	var req botProfileRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if len([]rune(name)) > 80 {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is too long")
		return
	}
	if err := s.ensureSectionOwned(r.Context(), accountID, req.SectionID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	position := existing.Position
	if req.Position != nil {
		position = clampPosition(*req.Position)
	}
	pinned := existing.Pinned
	if req.Pinned != nil {
		pinned = *req.Pinned
	}
	bot, err := s.db.UpdateBotProfile(r.Context(), store.UpdateBotProfileParams{
		ID:          botID,
		OwnerID:     accountID,
		Name:        name,
		Title:       strings.TrimSpace(req.Title),
		Description: strings.TrimSpace(req.Description),
		Avatar:      strings.TrimSpace(req.Avatar),
		AvatarShape: strings.TrimSpace(req.AvatarShape),
		AvatarColor: strings.TrimSpace(req.AvatarColor),
		SectionID:   req.SectionID,
		Position:    position,
		Pinned:      pinned,
	})
	if err != nil {
		s.logger.Error("update bot profile failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not update bot")
		return
	}
	if req.Visibility == "owner" || req.Visibility == "account" {
		_ = s.db.SetBotAccess(r.Context(), store.SetBotAccessParams{
			ID: bot.ID, OwnerID: accountID, Visibility: req.Visibility, CreatedByUserID: nil,
		})
		bot.Visibility = req.Visibility
	}
	writeJSON(w, http.StatusOK, map[string]any{"bot": toBotDTO(bot)})
}

type setHiddenRequest struct {
	Hidden bool `json:"hidden"`
}

func (s *Server) handleSetBotHidden(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	var req setHiddenRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	bot, err := s.db.SetBotHidden(r.Context(), store.SetBotHiddenParams{
		ID:      botID,
		OwnerID: accountFrom(r),
		Hidden:  req.Hidden,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "bot not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not update bot visibility")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bot": toBotDTO(bot)})
}

func (s *Server) handleDeleteBot(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	user := userFrom(r)
	accountID := accountFrom(r)
	account, _ := s.db.GetAccountByID(r.Context(), accountID)
	if (account.PrimaryBotID != nil && *account.PrimaryBotID == botID) ||
		(user.PrimaryBotID != nil && *user.PrimaryBotID == botID) {
		writeError(w, http.StatusConflict, "primary_bot_required", "choose another primary Bot before deleting this one")
		return
	}
	deleteExisting, accessErr := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID})
	if accessErr != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	role, _ := s.accountRole(r.Context(), accountID, user.ID)
	if access, ok := s.botAccess(r.Context(), role, user.ID, deleteExisting); !ok || access != "editor" {
		writeError(w, http.StatusForbidden, "forbidden", "bot is read-only for this user")
		return
	}
	if err := s.db.DeleteAgentConversationsForBot(r.Context(), botID); err != nil {
		s.logger.Warn("delete agent conversations for bot failed", "bot_id", botID, "error", err)
	}
	n, err := s.db.DeleteBot(r.Context(), store.DeleteBotParams{ID: botID, OwnerID: accountID})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			writeError(w, http.StatusConflict, "conflict", "bot has conversation history and cannot be deleted")
			return
		}
		s.logger.Error("delete bot failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not delete bot")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	s.audit(r.Context(), accountID, user.ID, "bot.delete", "bot", &botID, map[string]any{"name": deleteExisting.Name})
	writeNoContent(w)
}

func (s *Server) validateBotRequest(r *http.Request, userID uuid.UUID, req *botRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Model = strings.TrimSpace(req.Model)
	req.Slug = strings.TrimSpace(req.Slug)
	if req.Name == "" {
		return errors.New("name is required")
	}
	if err := s.ensureSectionOwned(r.Context(), userID, req.SectionID); err != nil {
		return err
	}
	if err := s.resolveBotModel(r.Context(), userID, req); err != nil {
		return err
	}
	if req.ComputerProfile == "" {
		req.ComputerProfile = "none"
	}
	switch req.ComputerProfile {
	case "none", "browser", "desktop":
	default:
		return errors.New("computer_profile must be none, browser or desktop")
	}
	if req.ProviderID != nil {
		if _, err := s.db.GetProvider(r.Context(), store.GetProviderParams{ID: *req.ProviderID, OwnerID: userID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errors.New("provider not found")
			}
			return errors.New("could not validate provider")
		}
	}
	if req.Slug != "" {
		req.Slug = slugify(req.Slug)
	}
	return nil
}

// resolveBotModel lets the Grok-style "name + avatar" creation flow work:
// when the caller omits a model, use the first model of the selected provider,
// or the first enabled LLM provider with at least one model.
func (s *Server) resolveBotModel(ctx context.Context, userID uuid.UUID, req *botRequest) error {
	if req.ProviderID != nil {
		models, err := s.db.ListModelsByProvider(ctx, *req.ProviderID)
		if err != nil {
			return errors.New("could not load models")
		}
		if len(models) == 0 {
			return errors.New("selected provider has no models")
		}
		if req.Model == "" {
			req.Model = models[0].Name
		}
		return nil
	}

	providers, err := s.db.ListProvidersByType(ctx, store.ListProvidersByTypeParams{
		OwnerID: userID,
		Type:    "llm",
	})
	if err != nil {
		return errors.New("could not load providers")
	}
	for _, provider := range providers {
		if !provider.Enabled {
			continue
		}
		models, err := s.db.ListModelsByProvider(ctx, provider.ID)
		if err != nil || len(models) == 0 {
			continue
		}
		if req.Model != "" {
			for _, model := range models {
				if model.Name == req.Model {
					providerID := provider.ID
					req.ProviderID = &providerID
					return nil
				}
			}
			continue
		}
		providerID := provider.ID
		req.ProviderID = &providerID
		req.Model = models[0].Name
		return nil
	}
	if req.Model != "" {
		return errors.New("selected model does not belong to any configured provider")
	}
	return errors.New("no language model configured; add a provider and model in settings first")
}

func positionOrZero(position *int32) int32 {
	if position == nil {
		return 0
	}
	return clampPosition(*position)
}

func rawOrEmptyObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func slugify(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	var b strings.Builder
	lastDash := false
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 60 {
		slug = strings.Trim(slug[:60], "-")
	}
	if slug == "" {
		slug = "bot-" + uuid.NewString()[:8]
	}
	return slug
}

func (s *Server) handleListBotACL(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	role, _ := s.accountRole(r.Context(), accountID, userFrom(r).ID)
	if access, ok := s.botAccess(r.Context(), role, userFrom(r).ID, bot); !ok || access != "editor" {
		writeError(w, http.StatusForbidden, "forbidden", "bot is read-only for this user")
		return
	}
	rows, err := s.db.ListResourceACL(r.Context(), store.ListResourceACLParams{
		ResourceType: "bot", ResourceID: botID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load bot access")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"user_id": row.UserID, "email": row.Email, "display_name": row.DisplayName, "role": row.Role,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"acl": out})
}

func (s *Server) handleSetBotACL(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
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
	accountID := accountFrom(r)
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	role, _ := s.accountRole(r.Context(), accountID, userFrom(r).ID)
	if access, ok := s.botAccess(r.Context(), role, userFrom(r).ID, bot); !ok || access != "editor" {
		writeError(w, http.StatusForbidden, "forbidden", "bot is read-only for this user")
		return
	}
	if _, err := s.db.GetAccountMembership(r.Context(), store.GetAccountMembershipParams{
		AccountID: accountID, UserID: req.UserID,
	}); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "user is not a member of this account")
		return
	}
	if err := s.db.UpsertResourceACL(r.Context(), store.UpsertResourceACLParams{
		AccountID: accountID, ResourceType: "bot", ResourceID: botID,
		UserID: &req.UserID, Role: req.Role,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update bot access")
		return
	}
	s.audit(r.Context(), accountID, userFrom(r).ID, "acl.set", "bot", &botID, map[string]any{
		"user_id": req.UserID, "role": req.Role,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteBotACL(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	userID, ok := parseUUIDParam(w, r, "userID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	role, _ := s.accountRole(r.Context(), accountID, userFrom(r).ID)
	if access, ok := s.botAccess(r.Context(), role, userFrom(r).ID, bot); !ok || access != "editor" {
		writeError(w, http.StatusForbidden, "forbidden", "bot is read-only for this user")
		return
	}
	if err := s.db.DeleteResourceACL(r.Context(), store.DeleteResourceACLParams{
		ResourceType: "bot", ResourceID: botID, UserID: &userID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not remove bot access")
		return
	}
	s.audit(r.Context(), accountID, userFrom(r).ID, "acl.delete", "bot", &botID, map[string]any{"user_id": userID})
	writeNoContent(w)
}

func (s *Server) handleSetBotProactivity(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	role, _ := s.accountRole(r.Context(), accountFrom(r), userFrom(r).ID)
	if role != "owner" && role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "only account owners can change proactivity")
		return
	}
	raw, _ := json.Marshal(map[string]bool{"enabled": req.Enabled})
	bot, err := s.db.SetBotProactivity(r.Context(), store.SetBotProactivityParams{
		ID: botID, OwnerID: accountFrom(r), Proactivity: raw,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bot": toBotDTO(bot)})
}
