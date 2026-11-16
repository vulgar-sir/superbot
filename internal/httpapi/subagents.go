package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/store"
)

type subagentTaskDTO struct {
	ID                 uuid.UUID  `json:"id"`
	ParentConversation uuid.UUID  `json:"parent_conversation_id"`
	ChildConversation  uuid.UUID  `json:"child_conversation_id"`
	ParentTask         *uuid.UUID `json:"parent_task_id"`
	Depth              int32      `json:"depth"`
	Objective          string     `json:"objective"`
	Status             string     `json:"status"`
	ResultSummary      string     `json:"result_summary"`
	Error              string     `json:"error"`
	CreatedAt          time.Time  `json:"created_at"`
	FinishedAt         *time.Time `json:"finished_at"`
}

func toSubagentTaskDTO(task store.SubagentTask) subagentTaskDTO {
	return subagentTaskDTO{
		ID: task.ID, ParentConversation: task.ParentConversationID,
		ChildConversation: task.ChildConversationID, Objective: task.Objective,
		Status: task.Status, ResultSummary: task.ResultSummary, Error: task.Error,
		CreatedAt: task.CreatedAt, FinishedAt: task.FinishedAt,
	}
}

func toSubagentTaskRowDTO(task store.ListSubagentTasksByParentRow) subagentTaskDTO {
	return subagentTaskDTO{
		ID: task.ID, ParentConversation: task.ParentConversationID,
		ChildConversation: task.ChildConversationID, ParentTask: task.ParentTaskID, Depth: task.Depth,
		Objective: task.Objective, Status: task.Status, ResultSummary: task.ResultSummary, Error: task.Error,
		CreatedAt: task.CreatedAt, FinishedAt: task.FinishedAt,
	}
}

func (s *Server) subagentRoutes(r chi.Router) {
	r.Get("/", s.handleListSubagents)
	r.Post("/{taskID}/stop", s.handleStopSubagent)
	r.Post("/{taskID}/message", s.handleMessageSubagent)
}

func (s *Server) handleListSubagents(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("conversation_id")
	conversationID, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "conversation_id is required")
		return
	}
	tasks, err := s.db.ListSubagentTasksByParent(r.Context(), store.ListSubagentTasksByParentParams{
		OwnerID:              accountFrom(r),
		ParentConversationID: conversationID,
		Limit:                50,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list subagents")
		return
	}
	out := make([]subagentTaskDTO, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, toSubagentTaskRowDTO(task))
	}
	writeJSON(w, http.StatusOK, map[string]any{"subagents": out})
}

func (s *Server) handleStopSubagent(w http.ResponseWriter, r *http.Request) {
	taskID, ok := parseUUIDParam(w, r, "taskID")
	if !ok {
		return
	}
	task, err := s.db.GetSubagentTaskForOwner(r.Context(), store.GetSubagentTaskForOwnerParams{
		ID: taskID, OwnerID: accountFrom(r),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "subagent not found")
		return
	}
	if approvals, err := s.db.ListPendingApprovalsForConversation(r.Context(), task.ChildConversationID); err == nil {
		for _, approval := range approvals {
			_ = s.db.ExpireToolApproval(r.Context(), approval.ID)
			if approval.CheckpointID != nil {
				_ = s.db.DiscardCheckpoint(r.Context(), *approval.CheckpointID)
				_ = s.db.CancelCycleForCheckpoint(r.Context(), *approval.CheckpointID)
			}
		}
	}
	_, _ = s.db.RequestCycleCancel(r.Context(), task.ChildConversationID)
	n, err := s.db.CancelSubagentTask(r.Context(), store.CancelSubagentTaskParams{
		ID: taskID, OwnerID: accountFrom(r),
	})
	if err != nil || n == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "subagent is not running")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}

func (s *Server) handleMessageSubagent(w http.ResponseWriter, r *http.Request) {
	taskID, ok := parseUUIDParam(w, r, "taskID")
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
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "content is required")
		return
	}
	task, err := s.db.GetSubagentTaskForOwner(r.Context(), store.GetSubagentTaskForOwnerParams{
		ID: taskID, OwnerID: accountFrom(r),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "subagent not found")
		return
	}
	if task.Status != "running" {
		writeError(w, http.StatusBadRequest, "invalid_request", "subagent is not running")
		return
	}
	meta, _ := json.Marshal(map[string]any{"subagent_task_id": task.ID.String(), "parent_steer": true})
	if _, err := s.db.CreateMessage(r.Context(), store.CreateMessageParams{
		ConversationID: task.ChildConversationID,
		SenderType:     "user",
		Content:        content,
		Metadata:       meta,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not send message")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "queued", "note": "子代理会在下一步或审批恢复后读取该消息",
	})
}
