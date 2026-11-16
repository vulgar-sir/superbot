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
}

func toMemoryDTO(item store.Memory) memoryDTO {
	return memoryDTO{
		ID: item.ID, Content: item.Content, ScopeType: item.ScopeType,
		Source: item.Source, Pinned: item.Pinned, ArchivedAt: item.ArchivedAt,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

// writeMemoryError keeps refused secrets distinguishable from ordinary
// validation errors so the client can explain why nothing was saved.
func writeMemoryError(w http.ResponseWriter, err error) {
	if errors.Is(err, memory.ErrSecretContent) {
		writeError(w, http.StatusBadRequest, "secret_rejected", err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
}

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
	out := make([]memoryDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toMemoryDTO(item))
	}
	return out, nil
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
			result.Memories = append(result.Memories, toMemoryDTO(saved.Memory))
		}
	}
	return result
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

func userMemoryScope(r *http.Request) memory.Scope {
	return memory.Scope{Type: "user", ID: userFrom(r).ID}
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

func (s *Server) handleTidyBotMemories(w http.ResponseWriter, r *http.Request) {
	if scope, ok := s.botMemoryScope(w, r); ok {
		s.tidyScopeMemories(w, r, scope)
	}
}

func (s *Server) handlePinUserMemory(w http.ResponseWriter, r *http.Request) {
	s.pinScopeMemory(w, r, userMemoryScope(r))
}

func (s *Server) handleRestoreUserMemory(w http.ResponseWriter, r *http.Request) {
	s.restoreScopeMemory(w, r, userMemoryScope(r))
}

func (s *Server) handleTidyUserMemories(w http.ResponseWriter, r *http.Request) {
	s.tidyScopeMemories(w, r, userMemoryScope(r))
}

func (s *Server) handleListBotMemories(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	out, err := s.listScopeMemories(r, memory.Scope{Type: "bot", ID: botID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list memories")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memories": out})
}

type importMemoryRequest struct {
	Content string `json:"content"`
}

func (s *Server) handleImportBotMemories(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
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
	writeJSON(w, http.StatusOK, s.importScopeMemories(r, memory.Scope{Type: "bot", ID: botID}, lines))
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

func (s *Server) handleUpdateBotMemory(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	memoryID, ok := parseUUIDParam(w, r, "memoryID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	item, err := s.memory.Update(r.Context(), accountID, memoryID, botID, req.Content)
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memory": toMemoryDTO(item)})
}

func (s *Server) handleDeleteBotMemory(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	memoryID, ok := parseUUIDParam(w, r, "memoryID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	if err := s.memory.Delete(r.Context(), accountID, memoryID, botID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "memory not found")
		return
	}
	writeNoContent(w)
}

func (s *Server) userMemoryRoutes(r chi.Router) {
	r.Get("/", s.handleListUserMemories)
	r.Post("/", s.handleCreateUserMemory)
	r.Post("/import", s.handleImportUserMemories)
	r.Post("/tidy", s.handleTidyUserMemories)
	r.Post("/{memoryID}/pin", s.handlePinUserMemory)
	r.Post("/{memoryID}/restore", s.handleRestoreUserMemory)
	r.Put("/{memoryID}", s.handleUpdateUserMemory)
	r.Delete("/{memoryID}", s.handleDeleteUserMemory)
}

func (s *Server) handleListUserMemories(w http.ResponseWriter, r *http.Request) {
	out, err := s.listScopeMemories(r, userMemoryScope(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list memories")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memories": out})
}

func (s *Server) handleCreateUserMemory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
		Pinned  bool   `json:"pinned"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	scope := userMemoryScope(r)
	saved, err := s.memory.SaveWith(r.Context(), memory.SaveInput{
		OwnerID: accountFrom(r), ScopeType: scope.Type, ScopeID: scope.ID,
		Content: req.Content, Source: memory.SourceManual, Pinned: req.Pinned,
	})
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	status := http.StatusCreated
	if saved.Duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"memory": toMemoryDTO(saved.Memory), "duplicate": saved.Duplicate})
}

func (s *Server) handleUpdateUserMemory(w http.ResponseWriter, r *http.Request) {
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
	item, err := s.memory.UpdateUser(r.Context(), accountFrom(r), memoryID, userFrom(r).ID, req.Content)
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memory": toMemoryDTO(item)})
}

func (s *Server) handleDeleteUserMemory(w http.ResponseWriter, r *http.Request) {
	memoryID, ok := parseUUIDParam(w, r, "memoryID")
	if !ok {
		return
	}
	if err := s.memory.DeleteUser(r.Context(), accountFrom(r), memoryID, userFrom(r).ID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "memory not found")
		return
	}
	writeNoContent(w)
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

func (s *Server) handleImportUserMemories(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, s.importScopeMemories(r, userMemoryScope(r), lines))
}
