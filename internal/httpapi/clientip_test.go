package httpapi

import (
	"bytes"
	"crypto/ed25519"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name      string
		remote    string
		forwarded []string
		want      string
	}{
		{"direct public client", "203.0.113.9:4000", nil, "203.0.113.9"},
		{"direct client may not spoof", "203.0.113.9:4000", []string{"1.2.3.4"}, "203.0.113.9"},
		{"behind proxy", "172.18.0.5:5000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"proxy appends after a forged value", "172.18.0.5:5000", []string{"1.2.3.4, 198.51.100.7"}, "198.51.100.7"},
		{"several headers", "10.0.0.2:1", []string{"1.2.3.4", "198.51.100.8"}, "198.51.100.8"},
		{"garbage forwarded value", "127.0.0.1:1", []string{"not-an-ip"}, "127.0.0.1"},
		{"proxy without header", "127.0.0.1:1", nil, "127.0.0.1"},
	}
	for _, tc := range cases {
		r := httptest.NewRequest("POST", "/api/auth/login", nil)
		r.RemoteAddr = tc.remote
		for _, value := range tc.forwarded {
			r.Header.Add("X-Forwarded-For", value)
		}
		if got := clientIP(r); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDeviceSignKeyIsStableAndSecretBound(t *testing.T) {
	a := deriveDeviceSignKey([]byte("secret-one-0123456789abcdef0123"))
	again := deriveDeviceSignKey([]byte("secret-one-0123456789abcdef0123"))
	other := deriveDeviceSignKey([]byte("secret-two-0123456789abcdef0123"))
	if !bytes.Equal(a, again) {
		t.Fatal("the same secret must give the same key across restarts")
	}
	if bytes.Equal(a, other) {
		t.Fatal("different secrets must give different keys")
	}
	signature := ed25519.Sign(a, []byte("job|shell|ls|nonce"))
	if !ed25519.Verify(a.Public().(ed25519.PublicKey), []byte("job|shell|ls|nonce"), signature) {
		t.Fatal("derived key does not sign")
	}
}

func TestLoginLimiterClearsOnSuccessForTheSameClient(t *testing.T) {
	s := &Server{loginAttempts: map[string][]time.Time{}}
	client := "198.51.100.7"
	for i := 0; i < 10; i++ {
		if !s.allowLogin(client) {
			t.Fatalf("attempt %d refused too early", i+1)
		}
		// Every login in this loop succeeds.
		s.clearLoginAttempts(client)
	}
	if !s.allowLogin(client) {
		t.Fatal("successful logins must not use up the failure budget")
	}
	for i := 0; i < 10; i++ {
		s.allowLogin("203.0.113.1")
	}
	if s.allowLogin("203.0.113.1") {
		t.Fatal("eleven attempts without a success within a minute must be refused")
	}
	if !s.allowLogin(client) {
		t.Fatal("another client's failures must not lock this one out")
	}
}
