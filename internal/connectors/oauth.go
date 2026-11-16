package connectors

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

type oauthProviderConfig struct {
	AuthorizeURL string
	TokenURL     string
	Scope        string
	BasicAuth    bool
	ExtraAuth    map[string]string
	Name         string
}

func hashOAuthState(state string) string {
	sum := sha256.Sum256([]byte(state))
	return hex.EncodeToString(sum[:])
}

func oauthConfig(provider string) (oauthProviderConfig, bool) {
	key := strings.ToUpper(strings.ReplaceAll(provider, "-", "_"))
	clientID := strings.TrimSpace(os.Getenv("SUPERBOT_OAUTH_" + key + "_CLIENT_ID"))
	if clientID == "" {
		return oauthProviderConfig{}, false
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("SUPERBOT_PUBLIC_BASE_URL")), "/")
	override := func(name, fallback string) string {
		if value := strings.TrimSpace(os.Getenv("SUPERBOT_OAUTH_" + key + "_" + name)); value != "" {
			return value
		}
		return fallback
	}
	cfg := oauthProviderConfig{
		AuthorizeURL: override("AUTHORIZE_URL", ""),
		TokenURL:     override("TOKEN_URL", ""),
		Scope:        override("SCOPE", ""),
		Name:         provider,
	}
	switch provider {
	case "github":
		cfg.AuthorizeURL = override("AUTHORIZE_URL", "https://github.com/login/oauth/authorize")
		cfg.TokenURL = override("TOKEN_URL", "https://github.com/login/oauth/access_token")
		cfg.Scope = override("SCOPE", "repo read:user")
	case "slack":
		cfg.AuthorizeURL = override("AUTHORIZE_URL", "https://slack.com/oauth/v2/authorize")
		cfg.TokenURL = override("TOKEN_URL", "https://slack.com/api/oauth.v2.access")
		cfg.Scope = override("SCOPE", "chat:write,channels:read,users:read")
	case "notion":
		cfg.AuthorizeURL = override("AUTHORIZE_URL", "https://api.notion.com/v1/oauth/authorize")
		cfg.TokenURL = override("TOKEN_URL", "https://api.notion.com/v1/oauth/token")
		cfg.BasicAuth = true
		cfg.ExtraAuth = map[string]string{"owner": "user"}
	case "linear":
		cfg.AuthorizeURL = override("AUTHORIZE_URL", "https://linear.app/oauth/authorize")
		cfg.TokenURL = override("TOKEN_URL", "https://api.linear.app/oauth/token")
		cfg.Scope = override("SCOPE", "read,write")
	case "figma":
		cfg.AuthorizeURL = override("AUTHORIZE_URL", "https://www.figma.com/oauth")
		cfg.TokenURL = override("TOKEN_URL", "https://www.figma.com/api/oauth/token")
		cfg.Scope = override("SCOPE", "file_read")
	case "vercel":
		cfg.AuthorizeURL = override("AUTHORIZE_URL", "https://vercel.com/oauth/authorize")
		cfg.TokenURL = override("TOKEN_URL", "https://api.vercel.com/v2/oauth/access_token")
	case "sentry":
		cfg.AuthorizeURL = override("AUTHORIZE_URL", "https://sentry.io/oauth/authorize/")
		cfg.TokenURL = override("TOKEN_URL", "https://sentry.io/oauth/token/")
		cfg.Scope = override("SCOPE", "project:read event:read")
	case "atlassian":
		cfg.AuthorizeURL = override("AUTHORIZE_URL", "https://auth.atlassian.com/authorize")
		cfg.TokenURL = override("TOKEN_URL", "https://auth.atlassian.com/oauth/token")
		cfg.Scope = override("SCOPE", "read:jira-work read:jira-user offline_access")
		cfg.ExtraAuth = map[string]string{"audience": "api.atlassian.com", "prompt": "consent"}
	case "canva":
		cfg.AuthorizeURL = override("AUTHORIZE_URL", "https://www.canva.com/api/oauth/authorize")
		cfg.TokenURL = override("TOKEN_URL", "https://api.canva.com/rest/v1/oauth/token")
		cfg.Scope = override("SCOPE", "design:read")
		cfg.BasicAuth = true
	case "google_docs":
		cfg.AuthorizeURL = override("AUTHORIZE_URL", "https://accounts.google.com/o/oauth2/v2/auth")
		cfg.TokenURL = override("TOKEN_URL", "https://oauth2.googleapis.com/token")
		cfg.Scope = override("SCOPE", "https://www.googleapis.com/auth/documents.readonly")
		cfg.ExtraAuth = map[string]string{"access_type": "offline", "prompt": "consent"}
	case "google_drive":
		cfg.AuthorizeURL = override("AUTHORIZE_URL", "https://accounts.google.com/o/oauth2/v2/auth")
		cfg.TokenURL = override("TOKEN_URL", "https://oauth2.googleapis.com/token")
		cfg.Scope = override("SCOPE", "https://www.googleapis.com/auth/drive.readonly")
		cfg.ExtraAuth = map[string]string{"access_type": "offline", "prompt": "consent"}
	case "onedrive", "msteams":
		cfg.AuthorizeURL = override("AUTHORIZE_URL", "https://login.microsoftonline.com/common/oauth2/v2.0/authorize")
		cfg.TokenURL = override("TOKEN_URL", "https://login.microsoftonline.com/common/oauth2/v2.0/token")
		cfg.Scope = override("SCOPE", "Files.Read offline_access")
		if provider == "msteams" {
			cfg.Scope = override("SCOPE", "Chat.ReadWrite offline_access")
		}
	default:
		return oauthProviderConfig{}, false
	}
	if cfg.AuthorizeURL == "" || cfg.TokenURL == "" || base == "" {
		return oauthProviderConfig{}, false
	}
	return cfg, true
}

func (s *Service) OAuthConfigured(provider string) bool {
	_, ok := oauthConfig(provider)
	return ok
}

func (s *Service) OAuthProviders() []string {
	providers := []string{}
	for _, provider := range []string{
		"github", "slack", "notion", "linear", "figma", "vercel", "sentry",
		"atlassian", "canva", "google_docs", "google_drive", "onedrive", "msteams",
	} {
		if s.OAuthConfigured(provider) {
			providers = append(providers, provider)
		}
	}
	return providers
}

func (s *Service) OAuthStart(ctx context.Context, ownerID uuid.UUID, provider string) (string, error) {
	cfg, ok := oauthConfig(provider)
	if !ok {
		return "", errors.New("oauth is not configured for this provider")
	}
	token := randomToken()
	expires := time.Now().Add(10 * time.Minute)
	_ = s.db.DeleteExpiredOAuthStates(ctx)
	if err := s.db.CreateOAuthState(ctx, store.CreateOAuthStateParams{
		StateHash: hashOAuthState(token),
		OwnerID:   ownerID,
		Provider:  provider,
		ExpiresAt: expires,
	}); err != nil {
		return "", err
	}

	key := strings.ToUpper(strings.ReplaceAll(provider, "-", "_"))
	redirectURI := strings.TrimRight(os.Getenv("SUPERBOT_PUBLIC_BASE_URL"), "/") +
		"/api/connectors/oauth/" + provider + "/callback"
	values := url.Values{}
	values.Set("client_id", os.Getenv("SUPERBOT_OAUTH_"+key+"_CLIENT_ID"))
	values.Set("redirect_uri", redirectURI)
	values.Set("response_type", "code")
	values.Set("state", token)
	if cfg.Scope != "" {
		values.Set("scope", cfg.Scope)
	}
	for k, v := range cfg.ExtraAuth {
		values.Set(k, v)
	}
	return cfg.AuthorizeURL + "?" + values.Encode(), nil
}

func (s *Service) OAuthComplete(ctx context.Context, ownerID uuid.UUID, provider, code, state string) error {
	entry, err := s.db.TakeOAuthState(ctx, hashOAuthState(state))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("oauth state is invalid or expired")
		}
		return err
	}
	if entry.OwnerID != ownerID || entry.Provider != provider || entry.ExpiresAt.Before(time.Now()) {
		return errors.New("oauth state is invalid or expired")
	}
	cfg, ok := oauthConfig(provider)
	if !ok {
		return errors.New("oauth is not configured for this provider")
	}
	key := strings.ToUpper(strings.ReplaceAll(provider, "-", "_"))
	clientID := os.Getenv("SUPERBOT_OAUTH_" + key + "_CLIENT_ID")
	clientSecret := os.Getenv("SUPERBOT_OAUTH_" + key + "_CLIENT_SECRET")
	redirectURI := strings.TrimRight(os.Getenv("SUPERBOT_PUBLIC_BASE_URL"), "/") +
		"/api/connectors/oauth/" + provider + "/callback"
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", redirectURI)
	if !cfg.BasicAuth {
		values.Set("client_id", clientID)
		values.Set("client_secret", clientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if cfg.BasicAuth {
		req.SetBasicAuth(clientID, clientSecret)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("oauth token exchange HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	if payload.AccessToken == "" {
		if payload.Error != "" {
			return fmt.Errorf("%s: %s", payload.Error, payload.ErrorDesc)
		}
		return errors.New("oauth provider did not return an access token")
	}
	var expiresAt *time.Time
	if payload.ExpiresIn > 0 {
		value := time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second)
		expiresAt = &value
	}
	_, err = s.CreateToken(ctx, ownerID, Input{
		Provider:     provider,
		Name:         oauthDisplayName(provider),
		Token:        payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		ExpiresAt:    expiresAt,
	})
	return err
}

func oauthDisplayName(provider string) string {
	names := map[string]string{
		"github": "GitHub", "slack": "Slack", "notion": "Notion",
		"linear": "Linear", "figma": "Figma", "vercel": "Vercel",
		"sentry": "Sentry", "atlassian": "Atlassian", "canva": "Canva",
		"google_docs": "Google Docs", "google_drive": "Google Drive",
		"onedrive": "OneDrive", "msteams": "Microsoft Teams",
	}
	if name, ok := names[provider]; ok {
		return name
	}
	return provider
}

func randomToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return uuid.NewString()
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
