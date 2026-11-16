package httpapi

import (
	"net/http"
	"strings"
)

type capabilityDTO struct {
	Key      string   `json:"key"`
	Status   string   `json:"status"`
	Reason   string   `json:"reason,omitempty"`
	Requires []string `json:"requires,omitempty"`
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.ToLower(strings.Split(forwarded, ",")[0])
	}
	https := scheme == "https"
	oauthProviders := s.connectors.OAuthProviders()

	items := []capabilityDTO{
		{
			Key:      "webauthn",
			Status:   statusFor(https || len(s.cfg.WebAuthnOrigins) > 0),
			Reason:   reasonFor(https || len(s.cfg.WebAuthnOrigins) > 0, "需要 HTTPS 域名或配置 SUPERBOT_WEBAUTHN_ORIGINS"),
			Requires: []string{"https"},
		},
		{
			Key:      "oauth",
			Status:   statusFor(len(oauthProviders) > 0),
			Reason:   reasonFor(len(oauthProviders) > 0, "尚未配置任何 Provider OAuth Client ID"),
			Requires: []string{"oauth_app_credentials"},
		},
		{
			Key:      "web_push",
			Status:   statusFor(s.push != nil),
			Reason:   reasonFor(s.push != nil, "Web Push 服务未启动"),
			Requires: []string{"secure_context"},
		},
		{
			Key:      "image_generation",
			Status:   statusFor(s.cfg.ImageAPIKey != ""),
			Reason:   reasonFor(s.cfg.ImageAPIKey != "", "未配置 SUPERBOT_IMAGE_API_KEY"),
			Requires: []string{"image_api_key"},
		},
		{
			Key:    "desktop_client",
			Status: "available",
		},
		{
			Key:    "local_device",
			Status: "available",
		},
		{
			Key:      "voice",
			Status:   statusFor(s.cfg.STTURL != "" && s.cfg.TTSURL != ""),
			Reason:   reasonFor(s.cfg.STTURL != "" && s.cfg.TTSURL != "", "未配置 STT/TTS Provider；浏览器 Web Speech 仅作降级"),
			Requires: []string{"stt_tts_provider"},
		},
		{
			Key:      "memory_semantic",
			Status:   statusFor(s.cfg.EmbeddingAPIKey != ""),
			Reason:   reasonFor(s.cfg.EmbeddingAPIKey != "", "未配置 SUPERBOT_EMBEDDING_API_KEY；记忆召回使用本地哈希向量（非语义模型）"),
			Requires: []string{"embedding_api_key"},
		},
		{
			Key:      "oidc",
			Status:   statusFor(s.cfg.OIDCIssuer != "" && s.cfg.OIDCClientID != "" && s.cfg.OIDCClientSecret != ""),
			Reason:   reasonFor(s.cfg.OIDCIssuer != "" && s.cfg.OIDCClientID != "" && s.cfg.OIDCClientSecret != "", "未配置 OIDC Issuer/Client"),
			Requires: []string{"oidc_credentials"},
		},
		{
			Key:    "meetings",
			Status: "degraded",
			Reason: "本地会议转写/摘要可用；平台自动加入与实时音频未接入",
		},
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"capabilities":    items,
		"oauth_providers": oauthProviders,
		"public_scheme":   scheme,
	})
}

func statusFor(available bool) string {
	if available {
		return "available"
	}
	return "requires_config"
}

func reasonFor(available bool, reason string) string {
	if available {
		return ""
	}
	return reason
}
