package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"superbot/internal/auth"
	"superbot/internal/store"
)

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r).Role != "admin" {
			writeError(w, http.StatusForbidden, "forbidden", "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) userRoutes(r chi.Router) {
	r.Use(s.requireAdmin)
	r.Get("/", s.handleListUsers)
	r.Post("/", s.handleCreateUser)
	r.Delete("/{userID}", s.handleDeleteUser)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.db.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list users")
		return
	}
	out := make([]userDTO, 0, len(users))
	for _, user := range users {
		out = append(out, toUserDTO(user))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

type createUserRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		writeError(w, http.StatusBadRequest, "invalid_request", "valid email is required")
		return
	}
	if err := auth.ValidateNewPassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	if req.Role != "admin" {
		req.Role = "member"
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not hash password")
		return
	}
	user, err := s.db.CreateUser(r.Context(), store.CreateUserParams{
		Email: req.Email, PasswordHash: hash, DisplayName: strings.TrimSpace(req.DisplayName),
	})
	if err != nil {
		writeError(w, http.StatusConflict, "conflict", "email already exists")
		return
	}
	if req.Role == "admin" {
		_ = s.db.SetUserRole(r.Context(), store.SetUserRoleParams{ID: user.ID, Role: "admin"})
		user.Role = "admin"
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": toUserDTO(user)})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseUUIDParam(w, r, "userID")
	if !ok {
		return
	}
	user := userFrom(r)
	if user.ID == userID {
		writeError(w, http.StatusBadRequest, "invalid_request", "cannot delete your own account")
		return
	}
	target, err := s.db.GetUserByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load user")
		return
	}
	if target.Role == "admin" {
		count, err := s.db.CountAdmins(r.Context())
		if err != nil || count <= 1 {
			writeError(w, http.StatusBadRequest, "invalid_request", "cannot delete the last admin")
			return
		}
	}
	n, err := s.db.DeleteUser(r.Context(), userID)
	if err != nil || n == 0 {
		writeError(w, http.StatusInternalServerError, "internal", "could not delete user")
		return
	}
	writeNoContent(w)
}
