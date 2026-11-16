package httpapi

import (
	"crypto/ed25519"
	"crypto/sha256"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// clientIP returns the address a request really came from. Behind the
// reverse proxy every request arrives from the proxy's private address, so
// the proxy's own record of the client (the last X-Forwarded-For entry,
// which it appends) is used instead. A client cannot forge that entry: any
// value it sends itself ends up earlier in the list.
func clientIP(r *http.Request) string {
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	addr, err := netip.ParseAddr(remote)
	if err != nil || !(addr.IsPrivate() || addr.IsLoopback()) {
		return remote
	}
	forwarded := r.Header.Values("X-Forwarded-For")
	if len(forwarded) == 0 {
		return remote
	}
	parts := strings.Split(forwarded[len(forwarded)-1], ",")
	last := strings.TrimSpace(parts[len(parts)-1])
	if _, err := netip.ParseAddr(last); err != nil {
		return remote
	}
	return last
}

// deriveDeviceSignKey derives the key that signs jobs sent to paired
// devices from the platform secret, so it is the same on every API instance
// and across restarts. A key generated at start-up changed on every deploy,
// which made devices re-fetch it after each restart and left their pinned
// copy meaningless.
func deriveDeviceSignKey(secret []byte) ed25519.PrivateKey {
	seed := sha256.Sum256(append([]byte("superbot device job signing v1\x00"), secret...))
	return ed25519.NewKeyFromSeed(seed[:])
}
