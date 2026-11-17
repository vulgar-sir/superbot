package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/memory"
	"superbot/internal/store"
)

type memoryDTO struct {
	ID         uuid.UUID  `json:"id"`
	Content    string     `json:"content"`
	ScopeType  string     `json:"scope_type"`
	Source     string     `json:"source"`
	Pinned     bool       `json:"pinned"`
	ArchivedAt *time.Time `json:"archived_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	// SupersededBy is the newer memory that replaced this archived one.
	SupersededBy *uuid.UUID `json:"superseded_by,omitempty"`
	// Pending is true while the memory has no search vector in the current
	// embedding space (the embedding service was down, or the space changed);
	// the worker adds it in the background.
	Pending bool `json:"pending,omitempty"`
}

func toMemoryDTO(item store.Memory) memoryDTO {
	dto := memoryDTO{
		ID: item.ID, Content: item.Content, ScopeType: item.ScopeType,
		Source: item.Source, Pinned: item.Pinned, ArchivedAt: item.ArchivedAt,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
	if item.ArchivedAt != nil && len(item.Metadata) > 0 {
		var meta struct {
			SupersededBy string `json:"superseded_by"`
		}
		if json.Unmarshal(item.Metadata, &meta) == nil {
			if id, err := uuid.Parse(meta.SupersededBy); err == nil {
				dto.SupersededBy = &id
			}
		}
	}
	return dto
}

func toMemoryDTOs(items []store.Memory) []memoryDTO {
	out := make([]memoryDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toMemoryDTO(item))
	}
	return out
}

// writeMemoryError keeps refused secrets distinguishable from ordinary
// validation errors so the client can explain why nothing was saved.
func writeMemoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, memory.ErrSecretContent):
		writeError(w, http.StatusBadRequest, "secret_rejected", err.Error())
	case errors.Is(err, memory.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "memory not found")
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	}
}

// memoryScopeResolver finds the scope a request addresses and checks the
// caller may use it; on failure it has written the response.
type memoryScopeResolver func(w http.ResponseWriter, r *http.Request) (memory.Scope, bool)

func (s *Server) listScopeMemories(r *http.Request, scope memory.Scope) ([]memoryDTO, error) {
	var items []store.Memory
	var err error
	switch r.URL.Query().Get("include_archived") {
	case "1", "true":
		items, err = s.db.ListMemoriesIncludingArchived(r.Context(), store.ListMemoriesIncludingArchivedParams{
			OwnerID: accountFrom(r), ScopeType: scope.Type, ScopeID: scope.ID, Limit: 200,
		})
	default:
		items, err = s.db.ListMemories(r.Context(), store.ListMemoriesParams{
			OwnerID: accountFrom(r), ScopeType: scope.Type, ScopeID: scope.ID, Limit: 200,
		})
	}
	if err != nil {
		return nil, err
	}
	space := s.memory.ModelName()
	out := make([]memoryDTO, 0, len(items))
	for _, item := range items {
		dto := toMemoryDTO(item)
		dto.Pending = item.ArchivedAt == nil && (item.Embedding == nil || item.EmbeddingModel != space)
		out = append(out, dto)
	}
	return out, nil
}

func (s *Server) handleListScopeMemories(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	out, err := s.listScopeMemories(r, scope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list memories")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memories": out})
}

type createMemoryRequest struct {
	Content string `json:"content"`
	Pinned  bool   `json:"pinned"`
	// Supersedes are memories of the same scope the user says the new one
	// replaces; they are archived and point at the new memory.
	Supersedes []uuid.UUID `json:"supersedes"`
}

// handleCreateScopeMemory saves one memory typed by the user. Similar
// memories that were neither merged nor replaced come back as "related", so
// the client can offer to retire the ones the new memory makes outdated.
func (s *Server) handleCreateScopeMemory(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	var req createMemoryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(req.Supersedes) > 20 {
		writeError(w, http.StatusBadRequest, "invalid_request", "too many memories to replace")
		return
	}
	saved, err := s.memory.SaveWith(r.Context(), memory.SaveInput{
		OwnerID: accountFrom(r), ScopeType: scope.Type, ScopeID: scope.ID,
		Content: req.Content, Source: memory.SourceManual, Pinned: req.Pinned,
		Supersedes: req.Supersedes,
	})
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	status := http.StatusCreated
	if saved.Duplicate {
		status = http.StatusOK
	}
	dto := toMemoryDTO(saved.Memory)
	dto.Pending = saved.Pending
	writeJSON(w, status, map[string]any{
		"memory":           dto,
		"duplicate":        saved.Duplicate,
		"pending":          saved.Pending,
		"superseded":       toMemoryDTOs(saved.Superseded),
		"related":          toMemoryDTOs(saved.Related),
		"pinned_conflicts": toMemoryDTOs(saved.PinnedConflicts),
	})
}

func (s *Server) handleUpdateScopeMemory(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	memoryID, ok := parseUUIDParam(w, r, "memoryID")
	if !ok {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	item, pending, err := s.memory.UpdateInScope(r.Context(), accountFrom(r), memoryID, scope, req.Content)
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	dto := toMemoryDTO(item)
	dto.Pending = pending
	writeJSON(w, http.StatusOK, map[string]any{"memory": dto})
}

func (s *Server) handleDeleteScopeMemory(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	memoryID, ok := parseUUIDParam(w, r, "memoryID")
	if !ok {
		return
	}
	if err := s.memory.DeleteInScope(r.Context(), accountFrom(r), memoryID, scope); err != nil {
		if errors.Is(err, memory.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "memory not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not delete memory")
		return
	}
	writeNoContent(w)
}

// handleArchiveScopeMemory archives a memory on the user's request, pinned or
// not; superseded_by records the newer memory that replaced it.
func (s *Server) handleArchiveScopeMemory(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	memoryID, ok := parseUUIDParam(w, r, "memoryID")
	if !ok {
		return
	}
	var req struct {
		SupersededBy *uuid.UUID `json:"superseded_by"`
	}
	if err := decodeJSON(w, r, &req); err != nil && !errors.Is(err, errEmptyBody) {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	by := uuid.Nil
	if req.SupersededBy != nil {
		by = *req.SupersededBy
	}
	item, err := s.memory.ArchiveInScope(r.Context(), accountFrom(r), memoryID, scope, by)
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memory": toMemoryDTO(item)})
}

type importMemoryResult struct {
	Imported   int         `json:"imported"`
	Duplicates int         `json:"duplicates"`
	Rejected   int         `json:"rejected"`
	Memories   []memoryDTO `json:"memories"`
}

// importScopeMemories saves each line; lines already known only refresh the
// existing memory and lines that look like credentials are refused.
func (s *Server) importScopeMemories(r *http.Request, scope memory.Scope, lines []string) importMemoryResult {
	result := importMemoryResult{Memories: make([]memoryDTO, 0, len(lines))}
	for _, line := range lines {
		saved, err := s.memory.SaveWith(r.Context(), memory.SaveInput{
			OwnerID: accountFrom(r), ScopeType: scope.Type, ScopeID: scope.ID,
			Content: line, Metadata: json.RawMessage(`{"imported":true}`), Source: memory.SourceImport,
		})
		switch {
		case errors.Is(err, memory.ErrSecretContent):
			result.Rejected++
		case err != nil:
		case saved.Duplicate:
			result.Duplicates++
		default:
			result.Imported++
			dto := toMemoryDTO(saved.Memory)
			dto.Pending = saved.Pending
			result.Memories = append(result.Memories, dto)
		}
	}
	return result
}

type importMemoryRequest struct {
	Content string `json:"content"`
}

func (s *Server) handleImportScopeMemories(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	var req importMemoryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	lines := splitMemoryLines(req.Content)
	if len(lines) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "no memory lines found")
		return
	}
	writeJSON(w, http.StatusOK, s.importScopeMemories(r, scope, lines))
}

func (s *Server) pinScopeMemory(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	memoryID, ok := parseUUIDParam(w, r, "memoryID")
	if !ok {
		return
	}
	var req struct {
		Pinned bool `json:"pinned"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if _, err := s.memory.GetInScope(r.Context(), accountFrom(r), memoryID, scope); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "memory not found")
		return
	}
	item, err := s.memory.SetPinned(r.Context(), accountFrom(r), memoryID, req.Pinned)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "memory not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memory": toMemoryDTO(item)})
}

func (s *Server) restoreScopeMemory(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	memoryID, ok := parseUUIDParam(w, r, "memoryID")
	if !ok {
		return
	}
	if _, err := s.memory.GetInScope(r.Context(), accountFrom(r), memoryID, scope); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "memory not found")
		return
	}
	item, err := s.memory.Restore(r.Context(), accountFrom(r), memoryID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "memory not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memory": toMemoryDTO(item)})
}

func (s *Server) tidyScopeMemories(w http.ResponseWriter, r *http.Request, scope memory.Scope) {
	report, err := s.memory.Tidy(r.Context(), accountFrom(r), scope.Type, scope.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not tidy memories")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": report})
}

// scopeMemoryRoutes mounts the memory endpoints of one kind of scope.
func (s *Server) scopeMemoryRoutes(r chi.Router, resolve memoryScopeResolver) {
	with := func(handle func(http.ResponseWriter, *http.Request, memory.Scope)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if scope, ok := resolve(w, r); ok {
				handle(w, r, scope)
			}
		}
	}
	r.Get("/", with(s.handleListScopeMemories))
	r.Post("/", with(s.handleCreateScopeMemory))
	r.Post("/import", with(s.handleImportScopeMemories))
	r.Post("/tidy", with(s.tidyScopeMemories))
	r.Post("/{memoryID}/pin", with(s.pinScopeMemory))
	r.Post("/{memoryID}/restore", with(s.restoreScopeMemory))
	r.Post("/{memoryID}/archive", with(s.handleArchiveScopeMemory))
	r.Put("/{memoryID}", with(s.handleUpdateScopeMemory))
	r.Delete("/{memoryID}", with(s.handleDeleteScopeMemory))
}

// botMemoryScope resolves the bot in the URL and checks the caller owns it.
func (s *Server) botMemoryScope(w http.ResponseWriter, r *http.Request) (memory.Scope, bool) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return memory.Scope{}, false
	}
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountFrom(r)}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return memory.Scope{}, false
	}
	return memory.Scope{Type: "bot", ID: botID}, true
}

// teamMemoryScope resolves the group in the URL and checks the caller's
// account owns it. Team memories are shared by everyone in the group.
func (s *Server) teamMemoryScope(w http.ResponseWriter, r *http.Request) (memory.Scope, bool) {
	teamID, ok := parseUUIDParam(w, r, "teamID")
	if !ok {
		return memory.Scope{}, false
	}
	if _, err := s.db.GetTeam(r.Context(), store.GetTeamParams{ID: teamID, OwnerID: accountFrom(r)}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "team not found")
		return memory.Scope{}, false
	}
	return memory.Scope{Type: "team", ID: teamID}, true
}

// userMemoryScope is the signed-in person's own scope; nobody reaches
// another user's memories through the API.
func userMemoryScope(r *http.Request) memory.Scope {
	return memory.Scope{Type: "user", ID: userFrom(r).ID}
}

func (s *Server) resolveUserMemoryScope(_ http.ResponseWriter, r *http.Request) (memory.Scope, bool) {
	return userMemoryScope(r), true
}

func (s *Server) handlePinBotMemory(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.botMemoryScope(w, r); ok {
		s.pinScopeMemory(w, r, scope)
	}
}

func (s *Server) handleRestoreBotMemory(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.botMemoryScope(w, r); ok {
		s.restoreScopeMemory(w, r, scope)
	}
}

func (s *Server) handleArchiveBotMemory(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.botMemoryScope(w, r); ok {
		s.handleArchiveScopeMemory(w, r, scope)
	}
}

func (s *Server) handleTidyBotMemories(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.botMemoryScope(w, r); ok {
		s.tidyScopeMemories(w, r, scope)
	}
}

func (s *Server) handleListBotMemories(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.botMemoryScope(w, r); ok {
		s.handleListScopeMemories(w, r, scope)
	}
}

func (s *Server) handleImportBotMemories(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.botMemoryScope(w, r); ok {
		s.handleImportScopeMemories(w, r, scope)
	}
}

func (s *Server) handleUpdateBotMemory(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.botMemoryScope(w, r); ok {
		s.handleUpdateScopeMemory(w, r, scope)
	}
}

func (s *Server) handleDeleteBotMemory(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.botMemoryScope(w, r); ok {
		s.handleDeleteScopeMemory(w, r, scope)
	}
}

func (s *Server) userMemoryRoutes(r chi.Router) {
	s.scopeMemoryRoutes(r, s.resolveUserMemoryScope)
}

func (s *Server) teamMemoryRoutes(r chi.Router) {
	s.scopeMemoryRoutes(r, s.teamMemoryScope)
}

func splitMemoryLines(content string) []string {
	if parsed := memoryStringsFromJSON(content); len(parsed) > 0 {
		return parsed
	}
	raw := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool)
	for _, line := range raw {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "-*• \t")
		if len([]rune(line)) < 4 || strings.HasPrefix(line, "#") {
			continue
		}
		if len([]rune(line)) > 4000 {
			line = string([]rune(line)[:4000])
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
		if len(out) >= 100 {
			break
		}
	}
	return out
}

// memoryStringsFromJSON understands common AI export shapes: arrays of
// strings, or objects with content/text/memory/rule fields (ChatGPT, Claude,
// Cursor rules). It walks nested structures but only collects those keys.
func memoryStringsFromJSON(content string) []string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return nil
	}
	var root any
	if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
		return nil
	}
	out := make([]string, 0, 100)
	seen := map[string]bool{}
	var walk func(value any, key string)
	walk = func(value any, key string) {
		if len(out) >= 100 {
			return
		}
		switch typed := value.(type) {
		case string:
			switch strings.ToLower(key) {
			case "content", "text", "memory", "rule", "rules":
				text := strings.TrimSpace(typed)
				if len([]rune(text)) >= 4 {
					if len([]rune(text)) > 4000 {
						text = string([]rune(text)[:4000])
					}
					if !seen[text] {
						seen[text] = true
						out = append(out, text)
					}
				}
			}
		case []any:
			for _, item := range typed {
				walk(item, key)
			}
		case map[string]any:
			for childKey, child := range typed {
				walk(child, childKey)
			}
		}
	}
	walk(root, "")
	return out
}
