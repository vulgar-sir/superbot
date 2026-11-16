package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

type taskDTO struct {
	ID            uuid.UUID  `json:"id"`
	ProjectID     *uuid.UUID `json:"project_id"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Status        string     `json:"status"`
	Priority      int32      `json:"priority"`
	AssigneeBotID *uuid.UUID `json:"assignee_bot_id"`
	DueAt         *time.Time `json:"due_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type projectDTO struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	Visibility      string     `json:"visibility"`
	CreatedByUserID *uuid.UUID `json:"created_by_user_id"`
	CreatedAt       time.Time  `json:"created_at"`
}

func toTaskDTO(task store.Task) taskDTO {
	return taskDTO{
		ID: task.ID, ProjectID: task.ProjectID, Title: task.Title,
		Description: task.Description, Status: task.Status, Priority: task.Priority,
		AssigneeBotID: task.AssigneeBotID, DueAt: task.DueAt,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}
}

func (s *Server) taskRoutes(r chi.Router) {
	r.Get("/", s.handleListTasks)
	r.Post("/", s.handleCreateTask)
	r.Patch("/{taskID}", s.handleUpdateTask)
	r.Delete("/{taskID}", s.handleDeleteTask)
}

func (s *Server) projectRoutes(r chi.Router) {
	r.Get("/", s.handleListProjects)
	r.Post("/", s.handleCreateProject)
	r.Delete("/{projectID}", s.handleDeleteProject)
	r.Route("/{projectID}/acl", func(r chi.Router) {
		r.Get("/", s.handleListProjectACL)
		r.Put("/", s.handleSetProjectACL)
		r.Delete("/{userID}", s.handleDeleteProjectACL)
	})
}

func (s *Server) projectAccess(ctx context.Context, role string, userID uuid.UUID, project store.Project) (string, bool) {
	if role == "owner" || role == "admin" {
		return "editor", true
	}
	if project.CreatedByUserID != nil && *project.CreatedByUserID == userID {
		return "editor", true
	}
	if acl, err := s.db.GetResourceACLRole(ctx, store.GetResourceACLRoleParams{
		ResourceType: "project", ResourceID: project.ID, UserID: &userID,
	}); err == nil {
		return acl, true
	}
	if project.Visibility == "owner" {
		return "", false
	}
	return "viewer", true
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	var status *string
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		status = &raw
	}
	tasks, err := s.db.ListTasks(r.Context(), store.ListTasksParams{
		OwnerID: accountFrom(r),
		Status:  status,
		Limit:   parseLimit(r.URL.Query().Get("limit"), 100, 300),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list tasks")
		return
	}
	out := make([]taskDTO, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, toTaskDTO(task))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

type taskRequest struct {
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Status        string     `json:"status"`
	Priority      int32      `json:"priority"`
	AssigneeBotID *uuid.UUID `json:"assignee_bot_id"`
	ProjectID     *uuid.UUID `json:"project_id"`
	DueAt         *time.Time `json:"due_at"`
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req taskRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "title is required")
		return
	}
	if req.Status == "" {
		req.Status = "todo"
	}
	task, err := s.db.CreateTask(r.Context(), store.CreateTaskParams{
		OwnerID: accountFrom(r), ProjectID: req.ProjectID, Title: req.Title,
		Description: strings.TrimSpace(req.Description), Status: req.Status,
		Priority: req.Priority, AssigneeBotID: req.AssigneeBotID,
		CreatedByUserID: accountFrom(r), DueAt: req.DueAt,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if task.AssigneeBotID != nil {
		accountID := accountFrom(r)
		userID := userFrom(r).ID
		go s.dispatchTask(accountID, userID, task)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"task": toTaskDTO(task)})
}

// dispatchTask starts the assigned Bot in its DM so the task executes in the
// background; the Bot reports back through the existing task tools.
func (s *Server) dispatchTask(accountID, userID uuid.UUID, task store.Task) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conversation, err := s.chat.CreateDirectConversation(ctx, accountID, userID, *task.AssigneeBotID, "")
	if err != nil {
		s.logger.Warn("dispatch task: create DM failed", "task_id", task.ID, "error", err)
		return
	}
	content := fmt.Sprintf("请执行任务「%s」。\n\n%s\n\n任务 ID：%s\n完成后请调用 update_task 把状态改为 done，并用一句话汇报结果。", task.Title, task.Description, task.ID)
	metadata, _ := json.Marshal(map[string]string{"task_id": task.ID.String(), "source": "task_dispatch"})
	if _, err := s.chat.SendUserMessage(ctx, accountID, userID, conversation.ID, content, "", nil, nil, metadata); err != nil {
		s.logger.Warn("dispatch task: send failed", "task_id", task.ID, "error", err)
		return
	}
	if _, err := s.db.UpdateTask(ctx, store.UpdateTaskParams{
		ID: task.ID, OwnerID: accountID, Title: task.Title, Description: task.Description,
		Status: "in_progress", Priority: task.Priority, AssigneeBotID: task.AssigneeBotID, DueAt: task.DueAt,
	}); err != nil {
		s.logger.Warn("dispatch task: update status failed", "task_id", task.ID, "error", err)
	}
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	taskID, ok := parseUUIDParam(w, r, "taskID")
	if !ok {
		return
	}
	var req taskRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	existing, err := s.db.GetTaskForOwner(r.Context(), store.GetTaskForOwnerParams{ID: taskID, OwnerID: accountFrom(r)})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "not_found", "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load task")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = existing.Title
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = existing.Status
	}
	task, err := s.db.UpdateTask(r.Context(), store.UpdateTaskParams{
		ID: taskID, OwnerID: accountFrom(r), Title: title,
		Description: strings.TrimSpace(req.Description), Status: status,
		Priority: req.Priority, AssigneeBotID: req.AssigneeBotID, DueAt: req.DueAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update task")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": toTaskDTO(task)})
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	taskID, ok := parseUUIDParam(w, r, "taskID")
	if !ok {
		return
	}
	n, err := s.db.DeleteTask(r.Context(), store.DeleteTaskParams{ID: taskID, OwnerID: accountFrom(r)})
	if err != nil || n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}
	writeNoContent(w)
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.db.ListProjects(r.Context(), accountFrom(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list projects")
		return
	}
	role, _ := s.accountRole(r.Context(), accountFrom(r), userFrom(r).ID)
	out := make([]projectDTO, 0, len(projects))
	for _, project := range projects {
		if _, ok := s.projectAccess(r.Context(), role, userFrom(r).ID, project); !ok {
			continue
		}
		out = append(out, projectDTO{
			ID: project.ID, Name: project.Name, Description: project.Description,
			Visibility: project.Visibility, CreatedByUserID: project.CreatedByUserID,
			CreatedAt: project.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": out})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Visibility  string `json:"visibility"`
	}
	if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	userID := userFrom(r).ID
	project, err := s.db.CreateProject(r.Context(), store.CreateProjectParams{
		OwnerID: accountFrom(r), Name: strings.TrimSpace(req.Name), Description: strings.TrimSpace(req.Description),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create project")
		return
	}
	visibility := req.Visibility
	if visibility != "owner" {
		visibility = "account"
	}
	_ = s.db.SetProjectAccess(r.Context(), store.SetProjectAccessParams{
		ID: project.ID, OwnerID: accountFrom(r), Visibility: visibility, CreatedByUserID: &userID,
	})
	project.Visibility = visibility
	project.CreatedByUserID = &userID
	writeJSON(w, http.StatusCreated, map[string]any{"project": projectDTO{
		ID: project.ID, Name: project.Name, Description: project.Description,
		Visibility: project.Visibility, CreatedByUserID: project.CreatedByUserID, CreatedAt: project.CreatedAt,
	}})
}

func (s *Server) projectForAccess(w http.ResponseWriter, r *http.Request) (store.Project, bool) {
	projectID, ok := parseUUIDParam(w, r, "projectID")
	if !ok {
		return store.Project{}, false
	}
	project, err := s.db.GetProjectForOwner(r.Context(), store.GetProjectForOwnerParams{
		ID: projectID, OwnerID: accountFrom(r),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return store.Project{}, false
	}
	role, _ := s.accountRole(r.Context(), accountFrom(r), userFrom(r).ID)
	if access, ok := s.projectAccess(r.Context(), role, userFrom(r).ID, project); !ok || access != "editor" {
		writeError(w, http.StatusForbidden, "forbidden", "project is read-only for this user")
		return store.Project{}, false
	}
	return project, true
}

func (s *Server) handleListProjectACL(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectForAccess(w, r)
	if !ok {
		return
	}
	rows, err := s.db.ListResourceACL(r.Context(), store.ListResourceACLParams{
		ResourceType: "project", ResourceID: project.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load project access")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"user_id": row.UserID, "email": row.Email, "display_name": row.DisplayName, "role": row.Role})
	}
	writeJSON(w, http.StatusOK, map[string]any{"acl": out})
}

func (s *Server) handleSetProjectACL(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectForAccess(w, r)
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
	if _, err := s.db.GetAccountMembership(r.Context(), store.GetAccountMembershipParams{
		AccountID: accountFrom(r), UserID: req.UserID,
	}); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "user is not a member of this account")
		return
	}
	if err := s.db.UpsertResourceACL(r.Context(), store.UpsertResourceACLParams{
		AccountID: accountFrom(r), ResourceType: "project", ResourceID: project.ID,
		UserID: &req.UserID, Role: req.Role,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update project access")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteProjectACL(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectForAccess(w, r)
	if !ok {
		return
	}
	userID, ok := parseUUIDParam(w, r, "userID")
	if !ok {
		return
	}
	if err := s.db.DeleteResourceACL(r.Context(), store.DeleteResourceACLParams{
		ResourceType: "project", ResourceID: project.ID, UserID: &userID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not remove project access")
		return
	}
	writeNoContent(w)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	projectID, ok := parseUUIDParam(w, r, "projectID")
	if !ok {
		return
	}
	project, err := s.db.GetProjectForOwner(r.Context(), store.GetProjectForOwnerParams{
		ID: projectID, OwnerID: accountFrom(r),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}
	role, _ := s.accountRole(r.Context(), accountFrom(r), userFrom(r).ID)
	if access, ok := s.projectAccess(r.Context(), role, userFrom(r).ID, project); !ok || access != "editor" {
		writeError(w, http.StatusForbidden, "forbidden", "project is read-only for this user")
		return
	}
	n, err := s.db.DeleteProject(r.Context(), store.DeleteProjectParams{
		ID: projectID, OwnerID: accountFrom(r),
	})
	if err != nil || n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}
	writeNoContent(w)
}
