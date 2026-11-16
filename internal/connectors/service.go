package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"superbot/internal/secrets"
	"superbot/internal/store"
)

type Service struct {
	db     *store.DB
	box    *secrets.Box
	logger *slog.Logger
	client *http.Client
}

func NewService(db *store.DB, box *secrets.Box, logger *slog.Logger) *Service {
	return &Service{db: db, box: box, logger: logger, client: &http.Client{Timeout: 20 * time.Second}}
}

type Input struct {
	Provider     string
	Name         string
	Token        string
	RefreshToken string
	ExpiresAt    *time.Time
	Config       json.RawMessage
}

func (s *Service) CreateToken(ctx context.Context, ownerID uuid.UUID, input Input) (store.Connector, error) {
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = provider
	}
	if provider == "" || name == "" || strings.TrimSpace(input.Token) == "" {
		return store.Connector{}, errors.New("provider, name and token are required")
	}
	switch provider {
	case "github", "slack", "notion", "onepassword", "email", "custom",
		"linear", "figma", "vercel", "sentry", "postgres", "atlassian",
		"google_docs", "google_drive", "canva", "onedrive", "msteams", "caldav", "x":
	default:
		return store.Connector{}, errors.New("unsupported connector provider")
	}
	enc, err := s.box.EncryptString(strings.TrimSpace(input.Token))
	if err != nil {
		return store.Connector{}, err
	}
	var refreshEnc []byte
	if refresh := strings.TrimSpace(input.RefreshToken); refresh != "" {
		refreshEnc, err = s.box.EncryptString(refresh)
		if err != nil {
			return store.Connector{}, err
		}
	}
	if len(input.Config) == 0 {
		input.Config = json.RawMessage(`{}`)
	}
	return s.db.CreateConnector(ctx, store.CreateConnectorParams{
		OwnerID: ownerID, Provider: provider, Name: name, AuthType: "token",
		AccessTokenEnc: enc, RefreshTokenEnc: refreshEnc, ExpiresAt: input.ExpiresAt,
		Config: input.Config, Status: "active",
	})
}

func (s *Service) List(ctx context.Context, ownerID uuid.UUID) ([]store.Connector, error) {
	return s.db.ListConnectors(ctx, ownerID)
}

func (s *Service) Get(ctx context.Context, ownerID, id uuid.UUID) (store.Connector, error) {
	connector, err := s.db.GetConnector(ctx, store.GetConnectorParams{ID: id, OwnerID: ownerID})
	if err != nil {
		return store.Connector{}, errors.New("connector not found")
	}
	return connector, nil
}

func (s *Service) Delete(ctx context.Context, ownerID, id uuid.UUID) error {
	n, err := s.db.DeleteConnector(ctx, store.DeleteConnectorParams{ID: id, OwnerID: ownerID})
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("connector not found")
	}
	return nil
}

func (s *Service) ByProvider(ctx context.Context, ownerID uuid.UUID, provider string) (store.Connector, error) {
	return s.db.GetConnectorByProvider(ctx, store.GetConnectorByProviderParams{
		OwnerID: ownerID, Provider: provider,
	})
}

func (s *Service) AccessToken(connector store.Connector) (string, error) {
	if len(connector.AccessTokenEnc) == 0 {
		return "", errors.New("connector has no token")
	}
	expiring := connector.ExpiresAt != nil && time.Now().Add(time.Minute).After(*connector.ExpiresAt)
	if expiring && len(connector.RefreshTokenEnc) > 0 {
		token, err := s.refresh(context.Background(), connector)
		if err == nil {
			return token, nil
		}
		s.logger.Warn("connector refresh failed", "provider", connector.Provider, "error", err)
	}
	return s.box.DecryptString(connector.AccessTokenEnc)
}

func (s *Service) refresh(ctx context.Context, connector store.Connector) (string, error) {
	cfg, ok := oauthConfig(connector.Provider)
	if !ok {
		return "", errors.New("oauth is not configured for this provider")
	}
	refreshToken, err := s.box.DecryptString(connector.RefreshTokenEnc)
	if err != nil {
		return "", err
	}
	key := strings.ToUpper(strings.ReplaceAll(connector.Provider, "-", "_"))
	clientID := os.Getenv("SUPERBOT_OAUTH_" + key + "_CLIENT_ID")
	clientSecret := os.Getenv("SUPERBOT_OAUTH_" + key + "_CLIENT_SECRET")
	values := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	if !cfg.BasicAuth {
		values.Set("client_id", clientID)
		values.Set("client_secret", clientSecret)
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	if cfg.BasicAuth {
		request.SetBasicAuth(clientID, clientSecret)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("refresh HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(raw)))
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", err
	}
	if payload.AccessToken == "" {
		return "", fmt.Errorf("refresh failed: %s", payload.Error)
	}
	enc, err := s.box.EncryptString(payload.AccessToken)
	if err != nil {
		return "", err
	}
	refreshEnc := connector.RefreshTokenEnc
	if payload.RefreshToken != "" {
		refreshEnc, err = s.box.EncryptString(payload.RefreshToken)
		if err != nil {
			return "", err
		}
	}
	var expiresAt *time.Time
	if payload.ExpiresIn > 0 {
		value := time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second)
		expiresAt = &value
	}
	if _, err := s.db.UpdateConnectorTokens(ctx, store.UpdateConnectorTokensParams{
		ID:              connector.ID,
		OwnerID:         connector.OwnerID,
		AccessTokenEnc:  enc,
		RefreshTokenEnc: refreshEnc,
		ExpiresAt:       expiresAt,
	}); err != nil {
		s.logger.Warn("persist refreshed connector token failed", "error", err)
	}
	return payload.AccessToken, nil
}

func (s *Service) Revoke(ctx context.Context, connector store.Connector) error {
	token, err := s.AccessToken(connector)
	if err != nil {
		return err
	}
	key := strings.ToUpper(strings.ReplaceAll(connector.Provider, "-", "_"))
	clientID := os.Getenv("SUPERBOT_OAUTH_" + key + "_CLIENT_ID")
	clientSecret := os.Getenv("SUPERBOT_OAUTH_" + key + "_CLIENT_SECRET")
	switch connector.Provider {
	case "github":
		if clientID == "" || clientSecret == "" {
			return errors.New("github oauth client is not configured")
		}
		body, _ := json.Marshal(map[string]string{"access_token": token})
		request, err := http.NewRequestWithContext(ctx, http.MethodDelete,
			"https://api.github.com/applications/"+url.PathEscape(clientID)+"/token", bytes.NewReader(body))
		if err != nil {
			return err
		}
		request.SetBasicAuth(clientID, clientSecret)
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("Content-Type", "application/json")
		response, err := s.client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode >= 300 {
			return fmt.Errorf("github revoke HTTP %d", response.StatusCode)
		}
		return nil
	case "slack":
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/auth.revoke", nil)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := s.client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode >= 300 {
			return fmt.Errorf("slack revoke HTTP %d", response.StatusCode)
		}
		return nil
	default:
		return nil
	}
}

func (s *Service) Test(ctx context.Context, ownerID, id uuid.UUID) (string, error) {
	connector, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return "", err
	}
	token, err := s.AccessToken(connector)
	if err != nil {
		return "", err
	}
	switch connector.Provider {
	case "github":
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		return s.execute(req, "login")
	case "slack":
		body, _ := json.Marshal(map[string]string{})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/auth.test", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		return s.execute(req, "user")
	case "notion":
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.notion.com/v1/users/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Notion-Version", "2022-06-28")
		return s.execute(req, "name")
	case "caldav":
		var cfg struct {
			URL      string `json:"url"`
			Username string `json:"username"`
		}
		if err := json.Unmarshal(connector.Config, &cfg); err != nil || strings.TrimSpace(cfg.URL) == "" {
			return "", errors.New("CalDAV 连接器缺少 url 配置")
		}
		body := `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/></d:prop></d:propfind>`
		req, err := http.NewRequestWithContext(ctx, "PROPFIND", strings.TrimRight(cfg.URL, "/")+"/", strings.NewReader(body))
		if err != nil {
			return "", err
		}
		req.SetBasicAuth(cfg.Username, token)
		req.Header.Set("Depth", "0")
		req.Header.Set("Content-Type", "application/xml; charset=utf-8")
		response, err := s.client.Do(req)
		if err != nil {
			return "", err
		}
		defer response.Body.Close()
		if response.StatusCode != 207 && response.StatusCode != 200 {
			return "", fmt.Errorf("CalDAV HTTP %d", response.StatusCode)
		}
		return "calendar", nil
	default:
		return "", fmt.Errorf("connection test is not implemented for provider %q", connector.Provider)
	}
}

func (s *Service) execute(req *http.Request, field string) (string, error) {
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed map[string]any
	if json.Unmarshal(body, &parsed) == nil {
		if value, ok := parsed[field].(string); ok && value != "" {
			return value, nil
		}
		if ok, _ := parsed["ok"].(bool); ok {
			return "ok", nil
		}
	}
	return "connected", nil
}

type GitHubDeviceStart struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

func (s *Service) StartGitHubDevice(ctx context.Context, clientID string) (GitHubDeviceStart, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return GitHubDeviceStart{}, errors.New("GitHub OAuth client_id is required")
	}
	form := urlValues(map[string]string{"client_id": clientID, "scope": "repo read:user"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/device/code", strings.NewReader(form))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return GitHubDeviceStart{}, err
	}
	defer resp.Body.Close()
	var out GitHubDeviceStart
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return out, fmt.Errorf("GitHub device start HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	if out.Interval <= 0 {
		out.Interval = 5
	}
	return out, nil
}

// PollGitHubDevice returns (token, pending, error).
func (s *Service) PollGitHubDevice(ctx context.Context, clientID, deviceCode string) (string, bool, error) {
	form := urlValues(map[string]string{
		"client_id":   clientID,
		"device_code": deviceCode,
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(form))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	var payload struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", false, err
	}
	if payload.AccessToken != "" {
		return payload.AccessToken, false, nil
	}
	switch payload.Error {
	case "authorization_pending", "slow_down":
		return "", true, nil
	case "":
		return "", false, errors.New("GitHub did not return a token")
	default:
		return "", false, fmt.Errorf("%s: %s", payload.Error, payload.ErrorDesc)
	}
}

func (s *Service) CreateGitHubOAuth(ctx context.Context, ownerID uuid.UUID, token string) (store.Connector, error) {
	return s.CreateToken(ctx, ownerID, Input{Provider: "github", Name: "GitHub", Token: token})
}

func urlValues(values map[string]string) string {
	form := url.Values{}
	for key, value := range values {
		form.Set(key, value)
	}
	return form.Encode()
}
