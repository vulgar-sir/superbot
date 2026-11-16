package httpapi

import (
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

type computerDTO struct {
	BotID      uuid.UUID           `json:"bot_id"`
	Profile    string              `json:"profile"`
	Status     string              `json:"status"`
	Desired    string              `json:"desired_state"`
	VNCURL     string              `json:"vnc_url"`
	LastAction *workspaceActionDTO `json:"last_action,omitempty"`
	UpdatedAt  time.Time           `json:"updated_at"`
}

type workspaceActionDTO struct {
	ID          uuid.UUID  `json:"id"`
	Kind        string     `json:"kind"`
	Status      string     `json:"status"`
	Error       string     `json:"error"`
	CreatedAt   time.Time  `json:"created_at"`
	ProcessedAt *time.Time `json:"processed_at"`
}

func (s *Server) handleGetComputer(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountFrom(r)})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	workspace, err := s.db.GetWorkspaceByBot(r.Context(), botID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"computer": computerDTO{
				BotID:   botID,
				Profile: bot.ComputerProfile,
				Status:  "stopped",
				Desired: "stopped",
			}})
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load workspace")
		return
	}
	dto := s.computerDTO(botID, bot.ComputerProfile, workspace.Status, workspace.DesiredState, workspace.VncAddr, workspace.UpdatedAt)
	if actions, err := s.db.ListWorkspaceActionsForBot(r.Context(), store.ListWorkspaceActionsForBotParams{
		OwnerID: accountFrom(r), BotID: botID,
	}); err == nil && len(actions) > 0 {
		action := actions[0]
		dto.LastAction = &workspaceActionDTO{
			ID: action.ID, Kind: action.Kind, Status: action.Status, Error: action.Error,
			CreatedAt: action.CreatedAt, ProcessedAt: action.ProcessedAt,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"computer": dto})
}

func (s *Server) computerDTO(botID uuid.UUID, profile, status, desired, vncAddr string, updated time.Time) computerDTO {
	vncURL := ""
	if status == "running" && vncAddr != "" {
		base := "/api/bots/" + botID.String() + "/computer/live"
		vncURL = base + "/vnc.html?autoconnect=1&resize=scale&path=api/bots/" + botID.String() + "/computer/live/websockify"
	}
	return computerDTO{
		BotID:     botID,
		Profile:   profile,
		Status:    status,
		Desired:   desired,
		VNCURL:    vncURL,
		UpdatedAt: updated,
	}
}

func (s *Server) handleStartComputer(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountFrom(r)})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	if bot.ComputerProfile == "" || bot.ComputerProfile == "none" {
		writeError(w, http.StatusBadRequest, "invalid_request", "this bot has no computer profile; set it to browser or desktop first")
		return
	}
	workspace, err := s.db.SetWorkspaceDesiredState(r.Context(), store.SetWorkspaceDesiredStateParams{
		OwnerID:      accountFrom(r),
		BotID:        botID,
		Profile:      bot.ComputerProfile,
		DesiredState: "running",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not request computer start")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"computer": s.computerDTO(botID, bot.ComputerProfile, workspace.Status, workspace.DesiredState, workspace.VncAddr, workspace.UpdatedAt)})
}

func (s *Server) handleStopComputer(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountFrom(r)})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	workspace, err := s.db.SetWorkspaceDesiredState(r.Context(), store.SetWorkspaceDesiredStateParams{
		OwnerID:      accountFrom(r),
		BotID:        botID,
		Profile:      bot.ComputerProfile,
		DesiredState: "stopped",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not request computer stop")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"computer": s.computerDTO(botID, bot.ComputerProfile, workspace.Status, workspace.DesiredState, workspace.VncAddr, workspace.UpdatedAt)})
}

func (s *Server) handleComputerLive(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountFrom(r)}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	workspace, err := s.db.GetWorkspaceByBot(r.Context(), botID)
	if err != nil || workspace.VncAddr == "" || workspace.Status != "running" {
		writeError(w, http.StatusServiceUnavailable, "computer_offline", "computer is not running")
		return
	}
	target, err := url.Parse("http://" + workspace.VncAddr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "invalid computer address")
		return
	}
	prefix := "/api/bots/" + botID.String() + "/computer/live"
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = nil
	proxy.Rewrite = func(pr *httputil.ProxyRequest) {
		pr.SetURL(target)
		pr.Out.URL.Path = strings.TrimPrefix(pr.Out.URL.Path, prefix)
		if pr.Out.URL.Path == "" {
			pr.Out.URL.Path = "/"
		}
		pr.Out.Host = target.Host
		pr.SetXForwarded()
	}
	proxy.FlushInterval = -1
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		s.logger.Warn("computer proxy failed", "bot_id", botID, "error", err)
		http.Error(w, "computer proxy error", http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) handleComputerUpdate(w http.ResponseWriter, r *http.Request) {
	s.requestWorkspaceAction(w, r, "update")
}

func (s *Server) handleComputerReset(w http.ResponseWriter, r *http.Request) {
	s.requestWorkspaceAction(w, r, "reset")
}

func (s *Server) requestWorkspaceAction(w http.ResponseWriter, r *http.Request, kind string) {
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
	if bot.ComputerProfile == "" || bot.ComputerProfile == "none" {
		writeError(w, http.StatusBadRequest, "invalid_request", "this bot has no computer profile")
		return
	}
	action, err := s.db.CreateWorkspaceAction(r.Context(), store.CreateWorkspaceActionParams{
		OwnerID: accountID, BotID: botID, Kind: kind,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not request computer action")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"action": workspaceActionDTO{
		ID: action.ID, Kind: action.Kind, Status: action.Status, Error: action.Error,
		CreatedAt: action.CreatedAt, ProcessedAt: action.ProcessedAt,
	}})
}

func (s *Server) botComputerRoutes(r chi.Router) {
	r.Get("/", s.handleGetComputer)
	r.Post("/start", s.handleStartComputer)
	r.Post("/stop", s.handleStopComputer)
	r.Post("/update", s.handleComputerUpdate)
	r.Post("/reset", s.handleComputerReset)
	r.Post("/snapshot", s.handleComputerSnapshot)
	r.Get("/snapshots", s.handleListComputerSnapshots)
	r.Post("/restore", s.handleComputerRestore)
	r.Delete("/snapshots/{snapshotID}", s.handleDeleteComputerSnapshot)
	r.Handle("/live/*", http.HandlerFunc(s.handleComputerLive))
}

type computerSnapshotDTO struct {
	ID        uuid.UUID `json:"id"`
	Label     string    `json:"label"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) handleComputerSnapshot(w http.ResponseWriter, r *http.Request) {
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
	if bot.ComputerProfile == "" || bot.ComputerProfile == "none" {
		writeError(w, http.StatusBadRequest, "invalid_request", "this bot has no computer profile")
		return
	}
	if _, err := s.db.CreateWorkspaceAction(r.Context(), store.CreateWorkspaceActionParams{
		OwnerID: accountID, BotID: botID, Kind: "snapshot",
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not request snapshot")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) handleListComputerSnapshots(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	items, err := s.db.ListComputerSnapshots(r.Context(), store.ListComputerSnapshotsParams{
		AccountID: accountID, BotID: botID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list snapshots")
		return
	}
	out := make([]computerSnapshotDTO, 0, len(items))
	for _, item := range items {
		out = append(out, computerSnapshotDTO{
			ID: item.ID, Label: item.Label, SizeBytes: item.SizeBytes, CreatedAt: item.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": out})
}

func (s *Server) handleComputerRestore(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	var req struct {
		SnapshotID uuid.UUID `json:"snapshot_id"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if _, err := s.db.GetComputerSnapshot(r.Context(), store.GetComputerSnapshotParams{
		ID: req.SnapshotID, AccountID: accountID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "snapshot not found")
		return
	}
	if _, err := s.db.CreateWorkspaceRestoreAction(r.Context(), store.CreateWorkspaceRestoreActionParams{
		OwnerID: accountID, BotID: botID, SnapshotID: &req.SnapshotID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not request restore")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) handleDeleteComputerSnapshot(w http.ResponseWriter, r *http.Request) {
	snapshotID, ok := parseUUIDParam(w, r, "snapshotID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	snapshot, err := s.db.GetComputerSnapshot(r.Context(), store.GetComputerSnapshotParams{
		ID: snapshotID, AccountID: accountID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "snapshot not found")
		return
	}
	n, err := s.db.DeleteComputerSnapshot(r.Context(), store.DeleteComputerSnapshotParams{
		ID: snapshotID, AccountID: accountID,
	})
	if err != nil || n == 0 {
		writeError(w, http.StatusInternalServerError, "internal", "could not delete snapshot")
		return
	}
	if strings.HasPrefix(snapshot.ArchivePath, s.cfg.DataDir+string(os.PathSeparator)) {
		_ = os.Remove(snapshot.ArchivePath)
	}
	writeNoContent(w)
}
