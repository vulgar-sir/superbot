package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

type botTemplateProfile struct {
	Name            string          `json:"name"`
	Title           string          `json:"title"`
	Description     string          `json:"description"`
	SystemPrompt    string          `json:"system_prompt"`
	AvatarShape     string          `json:"avatar_shape"`
	AvatarColor     string          `json:"avatar_color"`
	ComputerProfile string          `json:"computer_profile"`
	MemoryEnabled   bool            `json:"memory_enabled"`
	Params          json.RawMessage `json:"params"`
	Capabilities    json.RawMessage `json:"capabilities"`
	ToolPolicy      json.RawMessage `json:"tool_policy"`
}

type botTemplateSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

type botTemplateRoutine struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Timezone string `json:"timezone"`
	Timing   string `json:"timing"`
	Prompt   string `json:"prompt"`
	Enabled  bool   `json:"enabled"`
}

type botTemplateMemory struct {
	Content  string          `json:"content"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

type botTemplateMcp struct {
	Name      string          `json:"name"`
	Transport string          `json:"transport"`
	URL       string          `json:"url,omitempty"`
	Command   string          `json:"command,omitempty"`
	Args      []string        `json:"args,omitempty"`
	Config    json.RawMessage `json:"config,omitempty"`
}

type botTemplate struct {
	Version  int                  `json:"version"`
	Profile  botTemplateProfile   `json:"profile"`
	Skills   []botTemplateSkill   `json:"skills"`
	Routines []botTemplateRoutine `json:"routines"`
	Memories []botTemplateMemory  `json:"memories"`
	Mcp      []botTemplateMcp     `json:"mcp_servers"`
}

func (s *Server) handleExportBotTemplate(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, map[string]any{"template": s.buildBotTemplate(r.Context(), accountID, bot)})
}

type importTemplateRequest struct {
	Template botTemplate `json:"template"`
}

func (s *Server) handleImportBotTemplate(w http.ResponseWriter, r *http.Request) {
	var req importTemplateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Template.Version != 1 {
		writeError(w, http.StatusBadRequest, "invalid_template", "unsupported template version")
		return
	}
	profile := req.Template.Profile
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_template", "template profile name is required")
		return
	}
	accountID := accountFrom(r)
	botReq := botRequest{
		Name: name, Slug: slugify(name), Description: profile.Description,
		SystemPrompt: profile.SystemPrompt, AvatarShape: profile.AvatarShape,
		AvatarColor: profile.AvatarColor, ComputerProfile: profile.ComputerProfile,
		Params: profile.Params, Capabilities: profile.Capabilities, ToolPolicy: profile.ToolPolicy,
		MemoryEnabled: &profile.MemoryEnabled,
	}
	if err := s.resolveBotModel(r.Context(), accountID, &botReq); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_template", err.Error())
		return
	}
	if botReq.ComputerProfile == "" {
		botReq.ComputerProfile = "none"
	}
	bot, err := s.db.CreateBot(r.Context(), store.CreateBotParams{
		OwnerID: accountID, Name: name, Slug: slugify(name), Avatar: "",
		Description: profile.Description, SystemPrompt: profile.SystemPrompt,
		ProviderID: botReq.ProviderID, Model: botReq.Model,
		Params: rawOrEmptyObject(profile.Params), Capabilities: rawOrEmptyObject(profile.Capabilities),
		ToolPolicy: rawOrEmptyObject(profile.ToolPolicy), MemoryEnabled: profile.MemoryEnabled,
		ComputerProfile: botReq.ComputerProfile, Title: profile.Title,
		AvatarShape: profile.AvatarShape, AvatarColor: profile.AvatarColor,
		SectionID: nil, Position: 0, Kind: "personal",
	})
	if err != nil {
		writeError(w, http.StatusConflict, "conflict", "could not create bot from template; rename it and retry")
		return
	}
	skillIDs := make([]uuid.UUID, 0, len(req.Template.Skills))
	for _, item := range req.Template.Skills {
		name := strings.TrimSpace(item.Name)
		if name == "" || strings.TrimSpace(item.Content) == "" {
			continue
		}
		if existing, err := s.db.GetSkillByName(r.Context(), store.GetSkillByNameParams{
			OwnerID: accountID, Name: name,
		}); err == nil {
			skillIDs = append(skillIDs, existing.ID)
			continue
		} else if !errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		skill, err := s.skills.Create(r.Context(), accountID, name, item.Description, item.Content, "user")
		if err == nil {
			skillIDs = append(skillIDs, skill.ID)
		}
	}
	if len(skillIDs) > 0 {
		_ = s.skills.SetBotSkills(r.Context(), accountID, bot.ID, skillIDs)
	}
	for _, item := range req.Template.Routines {
		if strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Prompt) == "" {
			continue
		}
		_, _ = s.routines.CreateBotRoutine(
			r.Context(), accountID, bot.ID, item.Name, item.Schedule, item.Timezone,
			item.Timing, item.Prompt, item.Enabled,
		)
	}
	for index, item := range req.Template.Memories {
		if index >= 200 {
			break
		}
		content := strings.TrimSpace(item.Content)
		if content == "" || len([]rune(content)) > 4000 {
			continue
		}
		metadata := item.Metadata
		if len(metadata) == 0 {
			metadata = json.RawMessage(`{}`)
		}
		_, _ = s.memory.Save(r.Context(), accountID, "bot", bot.ID, content, metadata)
	}
	if len(req.Template.Mcp) > 0 {
		existing, _ := s.db.ListMcpServers(r.Context(), accountID)
		byName := make(map[string]store.McpServer, len(existing))
		for _, server := range existing {
			byName[strings.ToLower(strings.TrimSpace(server.Name))] = server
		}
		serverIDs := make([]uuid.UUID, 0, len(req.Template.Mcp))
		for _, item := range req.Template.Mcp {
			name := strings.TrimSpace(item.Name)
			if name == "" {
				continue
			}
			if server, ok := byName[strings.ToLower(name)]; ok {
				serverIDs = append(serverIDs, server.ID)
				continue
			}
			var server store.McpServer
			var err error
			if strings.EqualFold(item.Transport, "stdio") && strings.TrimSpace(item.Command) != "" {
				server, err = s.mcp.CreateStdio(r.Context(), accountID, name, item.Command, item.Args, nil, "template")
			} else if strings.TrimSpace(item.URL) != "" {
				server, err = s.mcp.Create(r.Context(), accountID, name, item.URL, nil, "template")
			}
			if err != nil {
				s.logger.Warn("template mcp server import failed", "name", name, "error", err)
				continue
			}
			byName[strings.ToLower(name)] = server
			serverIDs = append(serverIDs, server.ID)
		}
		if len(serverIDs) > 0 {
			if err := s.mcp.SetBotServers(r.Context(), accountID, bot.ID, serverIDs); err != nil {
				s.logger.Warn("template mcp bind failed", "bot_id", bot.ID, "error", err)
			}
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"bot": toBotDTO(bot)})
}

func (s *Server) buildBotTemplate(ctx context.Context, userID uuid.UUID, bot store.Bot) botTemplate {
	template := botTemplate{
		Version: 1,
		Profile: botTemplateProfile{
			Name: bot.Name, Title: bot.Title, Description: bot.Description,
			SystemPrompt: bot.SystemPrompt, AvatarShape: bot.AvatarShape,
			AvatarColor: bot.AvatarColor, ComputerProfile: bot.ComputerProfile,
			MemoryEnabled: bot.MemoryEnabled, Params: bot.Params,
			Capabilities: bot.Capabilities, ToolPolicy: bot.ToolPolicy,
		},
		Skills:   []botTemplateSkill{},
		Routines: []botTemplateRoutine{},
		Memories: []botTemplateMemory{},
		Mcp:      []botTemplateMcp{},
	}
	if botSkills, err := s.skills.ListForBot(ctx, bot.ID); err == nil {
		for _, skill := range botSkills {
			if skill.Source == "builtin" {
				continue
			}
			content := ""
			if data, readErr := os.ReadFile(skill.FilesPath); readErr == nil {
				content = string(data)
			}
			template.Skills = append(template.Skills, botTemplateSkill{
				Name: skill.Name, Description: skill.Description, Content: content,
			})
		}
	}
	if routines, err := s.routines.ListBotRoutines(ctx, userID, &bot.ID); err == nil {
		for _, routine := range routines {
			template.Routines = append(template.Routines, botTemplateRoutine{
				Name: routine.Name, Schedule: routine.Schedule, Timezone: routine.Timezone,
				Timing: routine.Timing, Prompt: routine.Prompt, Enabled: routine.Enabled,
			})
		}
	}
	if memories, err := s.db.ListMemories(ctx, store.ListMemoriesParams{
		OwnerID: userID, ScopeType: "bot", ScopeID: bot.ID, Limit: 200,
	}); err == nil {
		for _, item := range memories {
			template.Memories = append(template.Memories, botTemplateMemory{
				Content: item.Content, Metadata: item.Metadata,
			})
		}
	}
	if servers, err := s.db.ListBotMcpServers(ctx, bot.ID); err == nil {
		for _, row := range servers {
			args := []string{}
			_ = json.Unmarshal(row.McpServer.Args, &args)
			template.Mcp = append(template.Mcp, botTemplateMcp{
				Name: row.McpServer.Name, Transport: row.McpServer.Transport,
				URL: row.McpServer.Url, Command: row.McpServer.Command,
				Args: args, Config: row.McpServer.Config,
			})
		}
	}
	return template
}
