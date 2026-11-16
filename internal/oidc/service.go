package oidc

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ErrNotConfigured = errors.New("OIDC is not configured")

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       string
}

type Service struct {
	cfg    Config
	client *http.Client

	mu        sync.Mutex
	discovery *discovery
	fetchedAt time.Time
	jwks      map[string]*rsa.PublicKey
	jwksAt    time.Time
}

type discovery struct {
	Issuer        string `json:"issuer"`
	Authorization string `json:"authorization_endpoint"`
	Token         string `json:"token_endpoint"`
	JWKS          string `json:"jwks_uri"`
}

type Claims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Nonce         string `json:"nonce"`
	jwt.RegisteredClaims
}

func New(cfg Config) *Service {
	if cfg.Scopes == "" {
		cfg.Scopes = "openid email profile"
	}
	return &Service{cfg: cfg, client: &http.Client{Timeout: 20 * time.Second}}
}

func (s *Service) Configured() bool {
	return s.cfg.Issuer != "" && s.cfg.ClientID != "" && s.cfg.ClientSecret != ""
}

func (s *Service) Config() Config { return s.cfg }

func (s *Service) Status() map[string]any {
	status := map[string]any{"configured": s.Configured()}
	if s.Configured() {
		status["issuer"] = s.cfg.Issuer
	} else {
		status["requires_config"] = []string{
			"SUPERBOT_OIDC_ISSUER",
			"SUPERBOT_OIDC_CLIENT_ID",
			"SUPERBOT_OIDC_CLIENT_SECRET",
		}
	}
	return status
}

func (s *Service) AuthorizeURL(ctx context.Context, state, nonce, verifier, redirectURL string) (string, error) {
	if !s.Configured() {
		return "", ErrNotConfigured
	}
	doc, err := s.loadDiscovery(ctx)
	if err != nil {
		return "", err
	}
	challenge := pkceChallenge(verifier)
	values := url.Values{
		"response_type":         {"code"},
		"client_id":             {s.cfg.ClientID},
		"redirect_uri":          {redirectURL},
		"scope":                 {s.cfg.Scopes},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return doc.Authorization + "?" + values.Encode(), nil
}

func (s *Service) Exchange(ctx context.Context, code, verifier, redirectURL string) (string, error) {
	if !s.Configured() {
		return "", ErrNotConfigured
	}
	doc, err := s.loadDiscovery(ctx)
	if err != nil {
		return "", err
	}
	values := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURL},
		"client_id":     {s.cfg.ClientID},
		"client_secret": {s.cfg.ClientSecret},
		"code_verifier": {verifier},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, doc.Token, strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("token endpoint %s: %s", response.Status, truncate(string(raw), 300))
	}
	var payload struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if payload.IDToken == "" {
		return "", errors.New("token response has no id_token")
	}
	return payload.IDToken, nil
}

func (s *Service) VerifyIDToken(ctx context.Context, raw string) (*Claims, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}
	if err := s.loadJWKS(ctx); err != nil {
		return nil, err
	}
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != "RS256" {
			return nil, fmt.Errorf("unexpected signing method %s", token.Method.Alg())
		}
		kid, _ := token.Header["kid"].(string)
		if key, ok := s.jwks[kid]; ok {
			return key, nil
		}
		if len(s.jwks) == 1 {
			for _, key := range s.jwks {
				return key, nil
			}
		}
		return nil, errors.New("no matching JWKS key")
	}, jwt.WithIssuer(s.cfg.Issuer), jwt.WithAudience(s.cfg.ClientID), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid id_token: %w", err)
	}
	return claims, nil
}

func (s *Service) loadDiscovery(ctx context.Context) (*discovery, error) {
	s.mu.Lock()
	if s.discovery != nil && time.Since(s.fetchedAt) < time.Hour {
		doc := s.discovery
		s.mu.Unlock()
		return doc, nil
	}
	s.mu.Unlock()

	endpoint := strings.TrimRight(s.cfg.Issuer, "/") + "/.well-known/openid-configuration"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery %s: %s", response.Status, truncate(string(raw), 200))
	}
	var doc discovery
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Authorization == "" || doc.Token == "" || doc.JWKS == "" {
		return nil, errors.New("discovery document is incomplete")
	}
	if doc.Issuer != "" && strings.TrimRight(doc.Issuer, "/") != strings.TrimRight(s.cfg.Issuer, "/") {
		return nil, fmt.Errorf("discovery issuer mismatch: %s", doc.Issuer)
	}
	s.mu.Lock()
	s.discovery = &doc
	s.fetchedAt = time.Now()
	s.mu.Unlock()
	return &doc, nil
}

func (s *Service) loadJWKS(ctx context.Context) error {
	s.mu.Lock()
	if len(s.jwks) > 0 && time.Since(s.jwksAt) < 10*time.Minute {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	doc, err := s.loadDiscovery(ctx)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, doc.JWKS, nil)
	if err != nil {
		return err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks %s", response.Status)
	}
	var payload struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, key := range payload.Keys {
		if key.Kty != "RSA" || key.N == "" || key.E == "" {
			continue
		}
		modulus, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			continue
		}
		exponent, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil || len(exponent) == 0 {
			continue
		}
		value := 0
		for _, b := range exponent {
			value = value<<8 | int(b)
		}
		keys[key.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: value}
	}
	if len(keys) == 0 {
		return errors.New("JWKS has no usable RSA keys")
	}
	s.mu.Lock()
	s.jwks = keys
	s.jwksAt = time.Now()
	s.mu.Unlock()
	return nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
