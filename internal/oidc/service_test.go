package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestVerifyIDToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer":                 issuer,
				"authorization_endpoint": issuer + "/authorize",
				"token_endpoint":         issuer + "/token",
				"jwks_uri":               issuer + "/jwks",
			})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"keys": []map[string]string{{
					"kty": "RSA",
					"kid": "test-key",
					"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	issuer = server.URL

	service := New(Config{Issuer: issuer, ClientID: "superbot", ClientSecret: "secret"})
	claims := Claims{
		Email: "person@example.com",
		Name:  "Person",
		Nonce: "nonce-1",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Audience:  jwt.ClaimStrings{"superbot"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	verified, err := service.VerifyIDToken(context.Background(), signed)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if verified.Email != "person@example.com" || verified.Nonce != "nonce-1" {
		t.Fatalf("unexpected claims: %#v", verified)
	}
}

func TestAuthorizeURLIncludesPKCE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 serverURL(r),
			"authorization_endpoint": serverURL(r) + "/authorize",
			"token_endpoint":         serverURL(r) + "/token",
			"jwks_uri":               serverURL(r) + "/jwks",
		})
	}))
	defer server.Close()
	service := New(Config{Issuer: server.URL, ClientID: "superbot", ClientSecret: "secret"})
	target, err := service.AuthorizeURL(context.Background(), "state-1", "nonce-1", "verifier-1", "http://localhost/callback")
	if err != nil {
		t.Fatalf("authorize url: %v", err)
	}
	for _, fragment := range []string{"state=state-1", "nonce=nonce-1", "code_challenge_method=S256", "client_id=superbot"} {
		if !strings.Contains(target, fragment) {
			t.Fatalf("authorize url %q missing %q", target, fragment)
		}
	}
}

func serverURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
