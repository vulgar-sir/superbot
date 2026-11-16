package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"superbot/internal/auth"
	"superbot/internal/oidc"
	"superbot/internal/store"
)

const oidcCookieName = "superbot_oidc_state"

type oidcStatePayload struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Expires  int64  `json:"expires"`
}

func (s *Server) oidcService() *oidc.Service {
	return oidc.New(oidc.Config{
		Issuer:       s.cfg.OIDCIssuer,
		ClientID:     s.cfg.OIDCClientID,
		ClientSecret: s.cfg.OIDCClientSecret,
		RedirectURL:  s.cfg.OIDCRedirectURL,
	})
}

func (s *Server) oidcRoutes(r chi.Router) {
	r.Get("/status", s.handleOIDCStatus)
	r.Get("/start", s.handleOIDCStart)
	r.Get("/callback", s.handleOIDCCallback)
}

func (s *Server) handleOIDCStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.oidcService().Status())
}

func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	service := s.oidcService()
	if !service.Configured() {
		writeError(w, http.StatusServiceUnavailable, "requires_config", "OIDC is not configured")
		return
	}
	state, err := randomToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not start OIDC login")
		return
	}
	nonce, err := randomToken(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not start OIDC login")
		return
	}
	verifier, err := randomToken(48)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not start OIDC login")
		return
	}
	redirectURL := s.oidcRedirectURL(r)
	target, err := service.AuthorizeURL(r.Context(), state, nonce, verifier, redirectURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "oidc_failed", err.Error())
		return
	}
	cookieValue, err := s.signOIDCState(oidcStatePayload{
		State:    state,
		Nonce:    nonce,
		Verifier: verifier,
		Expires:  time.Now().Add(10 * time.Minute).Unix(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not encode OIDC state")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oidcCookieName,
		Value:    cookieValue,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !s.cfg.CookieInsecure,
		MaxAge:   600,
	})
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	service := s.oidcService()
	if !service.Configured() {
		oidcFail(w, r, "OIDC 未配置")
		return
	}
	cookie, err := r.Cookie(oidcCookieName)
	if err != nil {
		oidcFail(w, r, "登录状态已过期，请重试")
		return
	}
	payload, ok := s.verifyOIDCState(cookie.Value)
	if !ok || payload.Expires < time.Now().Unix() {
		oidcFail(w, r, "登录状态无效，请重试")
		return
	}
	if r.URL.Query().Get("state") != payload.State {
		oidcFail(w, r, "state 校验失败")
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		oidcFail(w, r, "身份提供方返回错误："+errParam)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		oidcFail(w, r, "缺少授权码")
		return
	}
	idToken, err := service.Exchange(r.Context(), code, payload.Verifier, s.oidcRedirectURL(r))
	if err != nil {
		s.logger.Warn("oidc token exchange failed", "error", err)
		oidcFail(w, r, "交换令牌失败")
		return
	}
	claims, err := service.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		s.logger.Warn("oidc id_token verification failed", "error", err)
		oidcFail(w, r, "校验 ID Token 失败")
		return
	}
	if claims.Nonce != payload.Nonce {
		oidcFail(w, r, "nonce 校验失败")
		return
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		oidcFail(w, r, "身份提供方没有返回邮箱")
		return
	}
	user, err := s.db.GetUserByEmail(r.Context(), email)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.logger.Error("oidc load user failed", "error", err)
			oidcFail(w, r, "读取账号失败")
			return
		}
		randomPassword, err := randomToken(32)
		if err != nil {
			oidcFail(w, r, "创建账号失败")
			return
		}
		hash, err := auth.HashPassword(randomPassword)
		if err != nil {
			oidcFail(w, r, "创建账号失败")
			return
		}
		displayName := strings.TrimSpace(claims.Name)
		if displayName == "" {
			displayName = email
		}
		user, err = s.db.CreateUser(r.Context(), store.CreateUserParams{
			Email:        email,
			PasswordHash: hash,
			DisplayName:  displayName,
		})
		if err != nil {
			// A concurrent callback may have just created the same account.
			if existing, lookupErr := s.db.GetUserByEmail(r.Context(), email); lookupErr == nil {
				user = existing
			} else {
				s.logger.Error("oidc create user failed", "error", err)
				oidcFail(w, r, "创建账号失败")
				return
			}
		}
		if err := s.auth.EnsurePersonalAccount(r.Context(), user); err != nil {
			s.logger.Error("oidc ensure personal account failed", "error", err)
			oidcFail(w, r, "初始化账号失败")
			return
		}
	}
	token, expires, err := s.auth.CreateSessionForUser(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("oidc create session failed", "error", err)
		oidcFail(w, r, "创建登录会话失败")
		return
	}
	s.setSessionCookie(w, token, expires)
	http.SetCookie(w, &http.Cookie{Name: oidcCookieName, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) oidcRedirectURL(r *http.Request) string {
	if s.cfg.OIDCRedirectURL != "" {
		return s.cfg.OIDCRedirectURL
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/api/auth/oidc/callback"
}

func (s *Server) signOIDCState(payload oidcStatePayload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.cfg.SecretKey)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Server) verifyOIDCState(value string) (oidcStatePayload, bool) {
	var payload oidcStatePayload
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return payload, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return payload, false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return payload, false
	}
	mac := hmac.New(sha256.New, s.cfg.SecretKey)
	mac.Write(raw)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return payload, false
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, false
	}
	return payload, true
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func oidcFail(w http.ResponseWriter, r *http.Request, message string) {
	target := "/login?oidc_error=" + url.QueryEscape(message)
	http.Redirect(w, r, target, http.StatusFound)
}
