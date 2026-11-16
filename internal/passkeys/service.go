package passkeys

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"superbot/internal/store"
)

type Manager struct {
	db      *store.DB
	rpID    string
	origins []string
}

func NewManager(db *store.DB, rpID string, origins []string) *Manager {
	return &Manager{db: db, rpID: strings.TrimSpace(rpID), origins: origins}
}

type userAdapter struct {
	user        store.User
	credentials []webauthn.Credential
}

func (u *userAdapter) WebAuthnID() []byte   { return []byte(u.user.ID.String()) }
func (u *userAdapter) WebAuthnName() string { return u.user.Email }
func (u *userAdapter) WebAuthnDisplayName() string {
	if strings.TrimSpace(u.user.DisplayName) != "" {
		return u.user.DisplayName
	}
	return u.user.Email
}
func (u *userAdapter) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func (m *Manager) loadUser(ctx context.Context, user store.User) (*userAdapter, error) {
	rows, err := m.db.ListWebAuthnCredentials(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	credentials := make([]webauthn.Credential, 0, len(rows))
	for _, row := range rows {
		var credential webauthn.Credential
		if err := json.Unmarshal(row.Credential, &credential); err != nil {
			continue
		}
		credentials = append(credentials, credential)
	}
	return &userAdapter{user: user, credentials: credentials}, nil
}

func (m *Manager) webAuthn(r *http.Request) (*webauthn.WebAuthn, error) {
	host := r.Host
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		host = forwarded
	}
	rpID := host
	if index := strings.LastIndex(host, ":"); index > 0 && !strings.Contains(host[index+1:], "]") {
		rpID = host[:index]
	}
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}
	origin := scheme + "://" + host
	if m.rpID != "" {
		rpID = m.rpID
	}
	origins := []string{origin}
	if len(m.origins) > 0 {
		origins = m.origins
	}
	return webauthn.New(&webauthn.Config{
		RPDisplayName: "superbot",
		RPID:          rpID,
		RPOrigins:     origins,
	})
}

func (m *Manager) BeginRegistration(ctx context.Context, r *http.Request, user store.User) (*protocol.CredentialCreation, error) {
	wa, err := m.webAuthn(r)
	if err != nil {
		return nil, err
	}
	adapter, err := m.loadUser(ctx, user)
	if err != nil {
		return nil, err
	}
	creation, session, err := wa.BeginRegistration(adapter)
	if err != nil {
		return nil, err
	}
	if err := m.saveSession(ctx, "reg:"+user.ID.String(), *session); err != nil {
		return nil, err
	}
	return creation, nil
}

func (m *Manager) FinishRegistration(ctx context.Context, r *http.Request, user store.User) (*webauthn.Credential, error) {
	wa, err := m.webAuthn(r)
	if err != nil {
		return nil, err
	}
	adapter, err := m.loadUser(ctx, user)
	if err != nil {
		return nil, err
	}
	session, ok := m.takeSession(ctx, "reg:"+user.ID.String())
	if !ok {
		return nil, errors.New("WebAuthn registration session expired")
	}
	credential, err := wa.FinishRegistration(adapter, session, r)
	if err != nil {
		// The library's message is intentionally terse; DevInfo carries the
		// expected/received origins needed to debug reverse-proxy setups.
		var protocolErr *protocol.Error
		if errors.As(err, &protocolErr) {
			slog.Warn("webauthn registration failed", "error", protocolErr.Details, "debug", protocolErr.DevInfo)
		}
		return nil, err
	}
	raw, _ := json.Marshal(credential)
	_, err = m.db.CreateWebAuthnCredential(ctx, store.CreateWebAuthnCredentialParams{
		UserID: user.ID, CredentialID: credential.ID, Credential: raw,
	})
	if err != nil {
		return nil, err
	}
	return credential, nil
}

func (m *Manager) BeginLogin(ctx context.Context, r *http.Request, user store.User) (*protocol.CredentialAssertion, error) {
	wa, err := m.webAuthn(r)
	if err != nil {
		return nil, err
	}
	adapter, err := m.loadUser(ctx, user)
	if err != nil {
		return nil, err
	}
	if len(adapter.credentials) == 0 {
		return nil, errors.New("no security keys registered")
	}
	assertion, session, err := wa.BeginLogin(adapter)
	if err != nil {
		return nil, err
	}
	if err := m.saveSession(ctx, "login:"+user.ID.String(), *session); err != nil {
		return nil, err
	}
	return assertion, nil
}

func (m *Manager) FinishLogin(ctx context.Context, r *http.Request, user store.User) (*webauthn.Credential, error) {
	wa, err := m.webAuthn(r)
	if err != nil {
		return nil, err
	}
	adapter, err := m.loadUser(ctx, user)
	if err != nil {
		return nil, err
	}
	session, ok := m.takeSession(ctx, "login:"+user.ID.String())
	if !ok {
		return nil, errors.New("WebAuthn login session expired")
	}
	credential, err := wa.FinishLogin(adapter, session, r)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(credential)
	_ = m.db.UpdateWebAuthnCredential(ctx, store.UpdateWebAuthnCredentialParams{
		UserID: user.ID, CredentialID: credential.ID, Credential: raw,
	})
	return credential, nil
}

func (m *Manager) Count(ctx context.Context, userID uuid.UUID) (int, error) {
	rows, err := m.db.ListWebAuthnCredentials(ctx, userID)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func (m *Manager) saveSession(ctx context.Context, key string, session webauthn.SessionData) error {
	expires := session.Expires
	if expires.IsZero() {
		expires = time.Now().Add(5 * time.Minute)
	}
	raw, err := json.Marshal(session)
	if err != nil {
		return err
	}
	_ = m.db.DeleteExpiredWebAuthnSessions(ctx)
	return m.db.UpsertWebAuthnSession(ctx, store.UpsertWebAuthnSessionParams{
		SessionKey: key,
		Session:    raw,
		ExpiresAt:  expires,
	})
}

func (m *Manager) takeSession(ctx context.Context, key string) (webauthn.SessionData, bool) {
	row, err := m.db.TakeWebAuthnSession(ctx, key)
	if err != nil || row.ExpiresAt.Before(time.Now()) {
		return webauthn.SessionData{}, false
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(row.Session, &session); err != nil {
		return webauthn.SessionData{}, false
	}
	return session, true
}
