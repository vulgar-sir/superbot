package phone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrNotConfigured = errors.New("phone provider is not configured")

type Config struct {
	AccountSID string
	AuthToken  string
	FromNumber string
	BaseURL    string
}

type Service struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) *Service {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.twilio.com"
	}
	return &Service{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second}}
}

func (s *Service) Configured() bool {
	return s.cfg.AccountSID != "" && s.cfg.AuthToken != "" && s.cfg.FromNumber != ""
}

func (s *Service) Status() map[string]any {
	status := map[string]any{"configured": s.Configured(), "from_number": s.cfg.FromNumber}
	if !s.Configured() {
		status["requires_config"] = []string{
			"SUPERBOT_TWILIO_ACCOUNT_SID",
			"SUPERBOT_TWILIO_AUTH_TOKEN",
			"SUPERBOT_TWILIO_FROM_NUMBER",
		}
	}
	return status
}

func (s *Service) SendSMS(ctx context.Context, to, body string) (string, error) {
	if !s.Configured() {
		return "", ErrNotConfigured
	}
	values := url.Values{
		"To":   {strings.TrimSpace(to)},
		"From": {s.cfg.FromNumber},
		"Body": {strings.TrimSpace(body)},
	}
	return s.post(ctx, "Messages.json", values)
}

func (s *Service) Call(ctx context.Context, to, message string) (string, error) {
	if !s.Configured() {
		return "", ErrNotConfigured
	}
	twiml := fmt.Sprintf("<Response><Say>%s</Say></Response>", xmlEscape(strings.TrimSpace(message)))
	values := url.Values{
		"To":    {strings.TrimSpace(to)},
		"From":  {s.cfg.FromNumber},
		"Twiml": {twiml},
	}
	return s.post(ctx, "Calls.json", values)
}

func (s *Service) post(ctx context.Context, path string, values url.Values) (string, error) {
	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/%s", strings.TrimRight(s.cfg.BaseURL, "/"), url.PathEscape(s.cfg.AccountSID), path)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	request.SetBasicAuth(s.cfg.AccountSID, s.cfg.AuthToken)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("twilio %s: %s", response.Status, strings.TrimSpace(string(raw)))
	}
	var payload struct {
		SID string `json:"sid"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", nil
	}
	return payload.SID, nil
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}
