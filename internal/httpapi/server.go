package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"superbot/internal/agentchat"
	"superbot/internal/auth"
	"superbot/internal/chat"
	"superbot/internal/config"
	"superbot/internal/connectors"
	"superbot/internal/devices"
	"superbot/internal/events"
	"superbot/internal/goals"
	"superbot/internal/imagegen"
	libindex "superbot/internal/library"
	"superbot/internal/llm"
	"superbot/internal/mailbox"
	"superbot/internal/mcpservice"
	"superbot/internal/memory"
	"superbot/internal/passkeys"
	"superbot/internal/push"
	routinesvc "superbot/internal/routines"
	"superbot/internal/secrets"
	"superbot/internal/skills"
	"superbot/internal/store"
	"superbot/internal/tunnel"
	"superbot/internal/vault"
	"superbot/internal/webrtcvoice"
)

const sessionCookieName = "superbot_session"

type contextKey string

const (
	userContextKey    contextKey = "superbot_user"
	accountContextKey contextKey = "superbot_account"
)

type Server struct {
	cfg        config.Config
	db         *store.DB
	auth       *auth.Service
	box        *secrets.Box
	chat       *chat.Service
	bus        *events.Bus
	factory    *llm.Factory
	skills     *skills.Service
	mcp        *mcpservice.Service
	routines   *routinesvc.Service
	agentChat  *agentchat.Service
	vault      *vault.Service
	devices    *devices.Service
	memory     *memory.Service
	goals      *goals.Service
	imagegen   *imagegen.Service
	mailbox    *mailbox.Service
	connectors *connectors.Service
	passkeys   *passkeys.Manager
	push       *push.Service
	library    *libindex.Indexer
	logger     *slog.Logger

	loginMu       sync.Mutex
	loginAttempts map[string][]time.Time
	deviceSignKey ed25519.PrivateKey
	collab        *collabHub
	tunnelHub     *tunnel.Hub
	webrtcVoice   *webrtcvoice.Server
}

func NewServer(
	cfg config.Config,
	db *store.DB,
	authService *auth.Service,
	box *secrets.Box,
	chatService *chat.Service,
	bus *events.Bus,
	factory *llm.Factory,
	logger *slog.Logger,
) *Server {
	deviceSignKey := deriveDeviceSignKey(cfg.SecretKey)
	server := &Server{
		cfg:           cfg,
		db:            db,
		auth:          authService,
		box:           box,
		chat:          chatService,
		bus:           bus,
		factory:       factory,
		skills:        skills.NewService(db, cfg.DataDir, logger),
		mcp:           mcpservice.NewService(db, box, logger),
		routines:      routinesvc.NewService(db, bus, logger),
		agentChat:     agentchat.NewService(db, bus, logger),
		goals:         goals.NewService(db),
		vault:         vault.NewService(db, box),
		devices:       devices.NewService(db, cfg.DataDir),
		memory:        memory.NewService(db, cfg.EmbeddingAPIKey, cfg.EmbeddingBaseURL, cfg.EmbeddingModel),
		imagegen:      imagegen.New(cfg.ImageAPIKey, cfg.ImageBaseURL, cfg.ImageModel),
		connectors:    connectors.NewService(db, box, logger),
		passkeys:      passkeys.NewManager(db, cfg.WebAuthnRPID, cfg.WebAuthnOrigins),
		library:       libindex.NewIndexer(db, cfg.DataDir, logger),
		logger:        logger,
		loginAttempts: make(map[string][]time.Time),
		deviceSignKey: deviceSignKey,
		collab:        newCollabHub(),
		tunnelHub:     tunnel.NewHub(),
	}
	if pushService, err := push.NewService(db, cfg.DataDir, logger); err != nil {
		logger.Warn("web push disabled", "error", err)
	} else {
		server.push = pushService
	}
	if cfg.WebRTCUDPPort > 0 && cfg.WebRTCPublicIP != "" {
		voice, err := webrtcvoice.New(webrtcvoice.Config{
			UDPServerPort: cfg.WebRTCUDPPort,
			PublicIP:      cfg.WebRTCPublicIP,
			STTURL:        cfg.STTURL,
			STTAPIKey:     cfg.STTAPIKey,
			STTModel:      cfg.STTModel,
		}, logger)
		if err != nil {
			logger.Warn("webrtc voice disabled", "error", err)
		} else {
			server.webrtcVoice = voice
		}
	}
	return server
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(s.requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(s.requireJSON)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", s.handleReady)

	r.Route("/api", func(r chi.Router) {
		r.Route("/device", s.deviceAgentRoutes)
		r.Get("/device/tunnel", s.handleDeviceTunnel)
		r.HandleFunc("/egress/proxy", s.handleEgressProxy)
		r.Post("/auth/webauthn/login/begin", s.handleWebAuthnLoginBegin)
		r.Post("/auth/webauthn/login/finish", s.handleWebAuthnLoginFinish)
		r.Post("/auth/login", s.handleLogin)
		r.Route("/auth/oidc", s.oidcRoutes)
		r.Get("/capabilities", s.handleCapabilities)
		r.Get("/shares/{token}", s.handleGetBotShare)
		r.Post("/webhooks/slack/{connectorID}", s.handleSlackWebhook)
		r.Post("/hooks/routines/{token}", s.handleRoutineWebhook)
		r.Post("/service-bots/{token}/messages", s.handleServiceBotMessage)
		r.Get("/oauth-mock/authorize", s.handleOAuthMockAuthorize)
		r.Post("/oauth-mock/token", s.handleOAuthMockToken)
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Post("/auth/password", s.handleChangePassword)
			r.Post("/auth/webauthn/register/begin", s.handleWebAuthnRegisterBegin)
			r.Post("/auth/webauthn/register/finish", s.handleWebAuthnRegisterFinish)
			r.Get("/auth/webauthn/credentials", s.handleWebAuthnCredentials)
			r.Patch("/auth/webauthn/credentials/{credentialID}", s.handleRenameWebAuthnCredential)
			r.Delete("/auth/webauthn/credentials/{credentialID}", s.handleDeleteWebAuthnCredential)
			r.Route("/accounts", s.accountsRoutes)
			r.Route("/account", s.accountRoutes)
			r.Post("/invitations/accept", s.handleAcceptInvitation)
			r.Route("/users", s.userRoutes)

			r.Route("/providers", s.providerRoutes)
			r.Get("/sidebar", s.handleGetSidebar)
			r.Route("/sections", s.sectionRoutes)
			r.Route("/bots", s.botRoutes)
			r.Route("/teams", s.teamRoutes)
			r.Route("/skills", s.skillRoutes)
			r.Route("/mcp", s.mcpRoutes)
			r.Route("/routines", s.routineRoutes)
			r.Route("/bot-routines", s.botRoutineRoutes)
			r.Route("/agent-threads", s.agentThreadRoutes)
			r.Route("/library", s.libraryRoutes)
			r.Route("/vault", s.vaultRoutes)
			r.Route("/connectors", s.connectorRoutes)
			r.Route("/devices", s.deviceRoutes)
			r.Post("/bot-templates/import", s.handleImportBotTemplate)
			r.Route("/templates", s.botTemplateRoutes)
			r.Get("/usage", s.handleUsage)
			r.Put("/usage/budget", s.handleUpdateUsageBudget)
			r.Route("/activities", s.activityRoutes)
			r.Get("/search", s.handleSearch)
			r.Route("/notifications", s.notificationRoutes)
			r.Route("/settings", s.settingsRoutes)
			r.Route("/push", s.pushRoutes)
			r.Route("/billing", s.billingRoutes)
			r.Route("/store", s.storeRoutes)
			r.Get("/audit-logs", s.handleListAuditLogs)
			r.Route("/diagnostics", s.diagnosticRoutes)
			r.Route("/phone", s.phoneRoutes)
			r.Route("/email", s.emailRoutes)
			r.Route("/x", s.xRoutes)
			r.Post("/captures/analyze", s.handleAnalyzeCapture)
			r.Route("/team-reviews", s.teamReviewRoutes)
			r.Route("/subagents", s.subagentRoutes)
			r.Route("/tasks", s.taskRoutes)
			r.Route("/documents", s.documentRoutes)
			r.Route("/calendar", s.calendarRoutes)
			r.Route("/voice", s.voiceRoutes)
			r.Route("/meetings", s.meetingRoutes)
			r.Route("/projects", s.projectRoutes)
			r.Route("/conversations", s.conversationRoutes)
			r.Route("/messages", s.messageRoutes)
			r.Route("/memories", s.userMemoryRoutes)
			r.Route("/permissions", s.permissionRoutes)
			r.Route("/tool-approvals", s.approvalRoutes)
			r.Get("/drive/files", s.handleListDriveFiles)
			r.Get("/drive/files/content", s.handleDriveFileContent)
			r.Route("/attachments", s.attachmentRoutes)
			r.Route("/runs", s.runRoutes)
		})
	})
	return r
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.Pool.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "db_unavailable", "database is not reachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if strings.HasPrefix(r.URL.Path, "/healthz") || strings.HasPrefix(r.URL.Path, "/readyz") {
			return
		}
		s.logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}

func (s *Server) requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if strings.HasPrefix(r.URL.Path, "/api/device/jobs/") && strings.HasSuffix(r.URL.Path, "/artifact") {
				next.ServeHTTP(w, r)
				return
			}
			routineHook := r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/hooks/routines/")
			multipartAttachment := r.Method == http.MethodPost &&
				(r.URL.Path == "/api/attachments" ||
					r.URL.Path == "/api/voice/transcribe" ||
					(strings.HasPrefix(r.URL.Path, "/api/bots/") &&
						(strings.HasSuffix(r.URL.Path, "/library/files") || strings.HasSuffix(r.URL.Path, "/avatar"))))
			oauthMockToken := r.Method == http.MethodPost && r.URL.Path == "/api/oauth-mock/token"
			contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
			if !multipartAttachment && !oauthMockToken && !routineHook && !strings.HasPrefix(contentType, "application/json") {
				writeError(w, http.StatusUnsupportedMediaType, "invalid_content_type", "Content-Type must be application/json")
				return
			}
			if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
				parsed, err := url.Parse(origin)
				if err != nil || !s.originAllowed(r, parsed) {
					writeError(w, http.StatusForbidden, "bad_origin", "cross-origin request rejected")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(r *http.Request, origin *url.URL) bool {
	if strings.EqualFold(origin.Host, r.Host) {
		return true
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" &&
		strings.EqualFold(origin.Host, forwarded) {
		return true
	}
	// The Next.js dev server proxies /api to the Go API, so the browser Origin
	// (localhost:3000) differs from the proxied Host (127.0.0.1:8080).
	if s.cfg.IsDevelopment() {
		switch origin.Hostname() {
		case "127.0.0.1", "localhost", "::1":
			return true
		}
	}
	return false
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "login required")
			return
		}
		user, accountID, err := s.auth.SessionUser(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, auth.ErrInvalidSession) {
				s.clearSessionCookie(w)
				writeError(w, http.StatusUnauthorized, "unauthorized", "session expired")
				return
			}
			s.logger.Error("session lookup failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "could not verify session")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		ctx = context.WithValue(ctx, accountContextKey, accountID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func userFrom(r *http.Request) store.User {
	user, _ := r.Context().Value(userContextKey).(store.User)
	return user
}

func accountFrom(r *http.Request) uuid.UUID {
	if accountID, ok := r.Context().Value(accountContextKey).(uuid.UUID); ok {
		return accountID
	}
	return userFrom(r).ID
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "email and password are required")
		return
	}
	if !s.allowLogin(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many login attempts, try again later")
		return
	}
	user, token, expires, err := s.auth.Login(r.Context(), email, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
			return
		}
		s.logger.Error("login failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "login failed")
		return
	}
	s.clearLoginAttempts(clientIP(r))
	s.setSessionCookie(w, token, expires)
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserDTO(user)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		_ = s.auth.Logout(r.Context(), cookie.Value)
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	accountID := accountFrom(r)
	if account, err := s.db.GetAccountByID(r.Context(), accountID); err == nil {
		user.PrimaryBotID = account.PrimaryBotID
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":              toUserDTO(user),
		"active_account_id": accountID,
	})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "current_password and new_password are required")
		return
	}
	user := userFrom(r)
	token, expires, err := s.auth.ChangePassword(r.Context(), user.ID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrCurrentPassword):
			writeError(w, http.StatusBadRequest, "invalid_current_password", "current password is incorrect")
		case errors.Is(err, auth.ErrPasswordTooShort):
			writeError(w, http.StatusBadRequest, "weak_password", "new password must be at least 10 characters")
		case errors.Is(err, auth.ErrPasswordTooLong):
			writeError(w, http.StatusBadRequest, "weak_password", "new password must be at most 128 characters")
		case errors.Is(err, auth.ErrPasswordUnchanged):
			writeError(w, http.StatusBadRequest, "password_unchanged", "new password must differ from the current password")
		default:
			s.logger.Error("change password failed", "error", err, "user_id", user.ID)
			writeError(w, http.StatusInternalServerError, "internal", "could not change password")
		}
		return
	}
	s.setSessionCookie(w, token, expires)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   !s.cfg.CookieInsecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   !s.cfg.CookieInsecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (s *Server) allowLogin(ip string) bool {
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	attempts := s.loginAttempts[ip]
	filtered := attempts[:0]
	for _, t := range attempts {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) >= 10 {
		s.loginAttempts[ip] = filtered
		return false
	}
	s.loginAttempts[ip] = append(filtered, now)
	return true
}

// clearLoginAttempts resets the counter after a successful login. It must
// use the same key as allowLogin, or successes would never clear anything.
func (s *Server) clearLoginAttempts(ip string) {
	s.loginMu.Lock()
	delete(s.loginAttempts, ip)
	s.loginMu.Unlock()
}

var errEmptyBody = errors.New("empty body")

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: request body is required", errEmptyBody)
		}
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if dec.More() {
		return fmt.Errorf("unexpected data after JSON body")
	}
	return nil
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}
