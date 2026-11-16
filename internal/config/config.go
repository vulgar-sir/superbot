package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is the process configuration loaded from environment variables.
// Environment variables are the only source of configuration; this keeps
// Docker Compose and .env files the single place to look for deployment knobs.
type Config struct {
	Env              string
	HTTPAddr         string
	DatabaseURL      string
	SecretKey        []byte
	AdminEmail       string
	AdminPassword    string
	CookieInsecure   bool
	DataDir          string
	LLMTimeout       time.Duration
	SessionLifetime  time.Duration
	DeepSeekAPIKey   string
	WebAuthnRPID     string
	WebAuthnOrigins  []string
	STTURL           string
	STTAPIKey        string
	STTModel         string
	TTSURL           string
	TTSAPIKey        string
	TTSModel         string
	TTSVoice         string
	TwilioSID        string
	TwilioToken      string
	TwilioFrom       string
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	TunnelToken      string
	EgressAddr       string
	SocksAddr        string
	WebRTCUDPPort    int
	WebRTCPublicIP   string
	ImageAPIKey      string
	ImageBaseURL     string
	ImageModel       string
	EmbeddingAPIKey  string
	EmbeddingBaseURL string
	EmbeddingModel   string
}

func (c Config) IsDevelopment() bool {
	return c.Env == "development"
}

// Load reads and validates configuration. It returns an error instead of
// calling os.Exit so the caller can decide how to report problems.
func Load() (Config, error) {
	cfg := Config{
		Env:              envOr("SUPERBOT_ENV", "development"),
		HTTPAddr:         envOr("SUPERBOT_HTTP_ADDR", "127.0.0.1:8080"),
		DatabaseURL:      strings.TrimSpace(os.Getenv("SUPERBOT_DATABASE_URL")),
		AdminEmail:       strings.TrimSpace(os.Getenv("SUPERBOT_ADMIN_EMAIL")),
		AdminPassword:    os.Getenv("SUPERBOT_ADMIN_PASSWORD"),
		CookieInsecure:   envBool("SUPERBOT_COOKIE_INSECURE", false),
		DataDir:          strings.TrimSpace(os.Getenv("SUPERBOT_DATA_DIR")),
		SessionLifetime:  30 * 24 * time.Hour,
		DeepSeekAPIKey:   strings.TrimSpace(os.Getenv("SUPERBOT_DEEPSEEK_API_KEY")),
		WebAuthnRPID:     strings.TrimSpace(os.Getenv("SUPERBOT_WEBAUTHN_RPID")),
		STTURL:           strings.TrimSpace(os.Getenv("SUPERBOT_STT_URL")),
		STTAPIKey:        strings.TrimSpace(os.Getenv("SUPERBOT_STT_API_KEY")),
		STTModel:         envOr("SUPERBOT_STT_MODEL", "whisper-1"),
		TTSURL:           strings.TrimSpace(os.Getenv("SUPERBOT_TTS_URL")),
		TTSAPIKey:        strings.TrimSpace(os.Getenv("SUPERBOT_TTS_API_KEY")),
		TTSModel:         envOr("SUPERBOT_TTS_MODEL", "tts-1"),
		TTSVoice:         envOr("SUPERBOT_TTS_VOICE", "alloy"),
		TwilioSID:        strings.TrimSpace(os.Getenv("SUPERBOT_TWILIO_ACCOUNT_SID")),
		TwilioToken:      strings.TrimSpace(os.Getenv("SUPERBOT_TWILIO_AUTH_TOKEN")),
		TwilioFrom:       strings.TrimSpace(os.Getenv("SUPERBOT_TWILIO_FROM_NUMBER")),
		OIDCIssuer:       strings.TrimSpace(os.Getenv("SUPERBOT_OIDC_ISSUER")),
		OIDCClientID:     strings.TrimSpace(os.Getenv("SUPERBOT_OIDC_CLIENT_ID")),
		OIDCClientSecret: strings.TrimSpace(os.Getenv("SUPERBOT_OIDC_CLIENT_SECRET")),
		OIDCRedirectURL:  strings.TrimSpace(os.Getenv("SUPERBOT_OIDC_REDIRECT_URL")),
		TunnelToken:      strings.TrimSpace(os.Getenv("SUPERBOT_TUNNEL_TOKEN")),
		EgressAddr:       envOr("SUPERBOT_EGRESS_ADDR", "0.0.0.0:8081"),
		SocksAddr:        envOr("SUPERBOT_SOCKS_ADDR", "0.0.0.0:8082"),
		WebRTCUDPPort:    envInt("SUPERBOT_WEBRTC_UDP_PORT", 0),
		WebRTCPublicIP:   strings.TrimSpace(os.Getenv("SUPERBOT_WEBRTC_PUBLIC_IP")),
		ImageAPIKey:      strings.TrimSpace(os.Getenv("SUPERBOT_IMAGE_API_KEY")),
		ImageBaseURL:     envOr("SUPERBOT_IMAGE_BASE_URL", "https://duomiapi.com"),
		ImageModel:       envOr("SUPERBOT_IMAGE_MODEL", "gpt-image-2.5-flare"),
		EmbeddingAPIKey:  strings.TrimSpace(os.Getenv("SUPERBOT_EMBEDDING_API_KEY")),
		EmbeddingBaseURL: envOr("SUPERBOT_EMBEDDING_BASE_URL", "https://api.openai.com"),
		EmbeddingModel:   envOr("SUPERBOT_EMBEDDING_MODEL", "text-embedding-3-small"),
	}
	if raw := strings.TrimSpace(os.Getenv("SUPERBOT_WEBAUTHN_ORIGINS")); raw != "" {
		for _, origin := range strings.Split(raw, ",") {
			if value := strings.TrimSpace(origin); value != "" {
				cfg.WebAuthnOrigins = append(cfg.WebAuthnOrigins, value)
			}
		}
	}

	rawKey := strings.TrimSpace(os.Getenv("SUPERBOT_SECRET_KEY"))
	if rawKey == "" {
		return Config{}, fmt.Errorf("SUPERBOT_SECRET_KEY is required (32 bytes, base64-encoded); generate one with: openssl rand -base64 32")
	}
	key, err := base64.StdEncoding.DecodeString(rawKey)
	if err != nil {
		return Config{}, fmt.Errorf("SUPERBOT_SECRET_KEY must be valid base64: %w", err)
	}
	if len(key) != 32 {
		return Config{}, fmt.Errorf("SUPERBOT_SECRET_KEY must decode to exactly 32 bytes, got %d", len(key))
	}
	cfg.SecretKey = key

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("SUPERBOT_DATABASE_URL is required")
	}

	timeoutSeconds := envInt("SUPERBOT_LLM_TIMEOUT_SECONDS", 180)
	if timeoutSeconds <= 0 {
		return Config{}, fmt.Errorf("SUPERBOT_LLM_TIMEOUT_SECONDS must be positive")
	}
	cfg.LLMTimeout = time.Duration(timeoutSeconds) * time.Second

	if cfg.DataDir == "" {
		cfg.DataDir = "data"
	}
	abs, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve SUPERBOT_DATA_DIR: %w", err)
	}
	cfg.DataDir = abs

	if cfg.AdminEmail == "" {
		cfg.AdminEmail = "admin@example.com"
	}
	if cfg.AdminPassword == "" {
		return Config{}, fmt.Errorf("SUPERBOT_ADMIN_PASSWORD is required for first boot")
	}
	if cfg.Env == "production" && cfg.AdminPassword == "change-me-please" {
		return Config{}, fmt.Errorf("refusing to start in production with the example admin password")
	}
	if cfg.Env == "production" && cfg.CookieInsecure && !envBool("SUPERBOT_ALLOW_INSECURE_COOKIE", false) {
		return Config{}, fmt.Errorf("SUPERBOT_COOKIE_INSECURE requires SUPERBOT_ALLOW_INSECURE_COOKIE=true in production (only for plain HTTP deployments)")
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return parsed
}
