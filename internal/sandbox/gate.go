package sandbox

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/google/uuid"
)

// Every computer listens on the sandbox network only through gate.py
// (deploy/images/computer/gate.py), which forwards a connection to the
// browser's DevTools port or to noVNC only when its first request carries
// the computer's secret. Without it any container on the network, another
// bot's computer included, could drive this bot's browser and screen.
//
// The secret is derived from the server key and the bot id, so the worker
// (which creates computers and drives the browser) and the API (which
// proxies the live view) agree on it without storing it anywhere.

// GateHeader is the request header that carries the computer's secret.
const GateHeader = "X-Superbot-Gate"

// gateTokenEnv passes the secret to the computer's gate.
const gateTokenEnv = "SUPERBOT_GATE_TOKEN"

// GateToken returns the secret of botID's computer. An empty key (tests
// without configuration) yields an empty token, which the gate refuses.
func GateToken(secretKey []byte, botID uuid.UUID) string {
	if len(secretKey) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte("superbot computer gate v1\x00"))
	mac.Write(botID[:])
	return hex.EncodeToString(mac.Sum(nil))[:40]
}

// GateHeaders returns the headers a request to the computer's CDP or noVNC
// endpoint must carry.
func (i Info) GateHeaders() http.Header {
	header := http.Header{}
	if i.GateToken != "" {
		header.Set(GateHeader, i.GateToken)
	}
	return header
}
