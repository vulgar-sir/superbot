package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"superbot/internal/store"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidSession     = errors.New("invalid or expired session")
	ErrCurrentPassword    = errors.New("current password is incorrect")
	ErrPasswordTooShort   = errors.New("new password is too short")
	ErrPasswordTooLong    = errors.New("new password is too long")
	ErrPasswordUnchanged  = errors.New("new password must differ from the current password")
)

const (
	PasswordMinRunes = 10
	PasswordMaxRunes = 128
)

// Service owns password verification and database-backed sessions.
// Session tokens are only ever stored as SHA-256 hashes.
type Service struct {
	queries         *store.Queries
	sessionLifetime time.Duration
}

func NewService(queries *store.Queries, sessionLifetime time.Duration) *Service {
	return &Service{queries: queries, sessionLifetime: sessionLifetime}
}

// EnsureAdmin creates the first user when the users table is empty. This is
// how the single-admin deployment bootstraps without a registration flow.
func (s *Service) EnsureAdmin(ctx context.Context, email, password, displayName string) error {
	count, err := s.queries.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}
	if displayName == "" {
		displayName = "Admin"
	}
	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	user, err := s.queries.CreateUser(ctx, store.CreateUserParams{
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
	})
	if err != nil {
		// Another API instance may have seeded the admin concurrently.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil
		}
		return fmt.Errorf("create admin user: %w", err)
	}
	if err := s.queries.SetUserRole(ctx, store.SetUserRoleParams{ID: user.ID, Role: "admin"}); err != nil {
		return fmt.Errorf("set admin role: %w", err)
	}
	if err := s.EnsurePersonalAccount(ctx, user); err != nil {
		return err
	}
	return nil
}

// EnsurePersonalAccount guarantees the user has the personal account whose ID
// equals the user ID. Session creation uses that account as the initial scope.
func (s *Service) EnsurePersonalAccount(ctx context.Context, user store.User) error {
	if _, err := s.queries.GetAccountByID(ctx, user.ID); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load personal account: %w", err)
	}
	name := strings.TrimSpace(user.DisplayName)
	if name == "" {
		name = user.Email
	}
	_, err := s.queries.CreatePersonalAccount(ctx, store.CreatePersonalAccountParams{
		ID: user.ID, Name: name, CreatedByUserID: user.ID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			return fmt.Errorf("create personal account: %w", err)
		}
	}
	if err := s.queries.AddAccountMember(ctx, store.AddAccountMemberParams{
		AccountID: user.ID, UserID: user.ID, Role: "owner",
	}); err != nil {
		return fmt.Errorf("add account owner: %w", err)
	}
	return nil
}

// Login verifies credentials and returns a new session token.
func (s *Service) Login(ctx context.Context, email, password string) (store.User, string, time.Time, error) {
	user, err := s.queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.User{}, "", time.Time{}, ErrInvalidCredentials
		}
		return store.User{}, "", time.Time{}, fmt.Errorf("get user: %w", err)
	}
	ok, err := VerifyPassword(user.PasswordHash, password)
	if err != nil {
		return store.User{}, "", time.Time{}, fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return store.User{}, "", time.Time{}, ErrInvalidCredentials
	}
	if err := s.EnsurePersonalAccount(ctx, user); err != nil {
		return store.User{}, "", time.Time{}, err
	}
	token, expires, err := s.createSession(ctx, user.ID)
	if err != nil {
		return store.User{}, "", time.Time{}, err
	}
	return user, token, expires, nil
}

// ChangePassword verifies the current password, stores a fresh hash and
// revokes every existing session before issuing a replacement session for the
// caller. That way a stolen cookie cannot survive a password rotation.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) (string, time.Time, error) {
	user, err := s.queries.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", time.Time{}, ErrInvalidSession
		}
		return "", time.Time{}, fmt.Errorf("get user: %w", err)
	}
	ok, err := VerifyPassword(user.PasswordHash, currentPassword)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("verify current password: %w", err)
	}
	if !ok {
		return "", time.Time{}, ErrCurrentPassword
	}
	if currentPassword == newPassword {
		return "", time.Time{}, ErrPasswordUnchanged
	}
	if err := ValidateNewPassword(newPassword); err != nil {
		return "", time.Time{}, err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("hash new password: %w", err)
	}
	if err := s.queries.UpdateUserPassword(ctx, store.UpdateUserPasswordParams{
		ID:           user.ID,
		PasswordHash: hash,
	}); err != nil {
		return "", time.Time{}, fmt.Errorf("update password: %w", err)
	}
	if err := s.queries.DeleteUserSessions(ctx, user.ID); err != nil {
		return "", time.Time{}, fmt.Errorf("revoke sessions: %w", err)
	}
	if err := s.EnsurePersonalAccount(ctx, user); err != nil {
		return "", time.Time{}, err
	}
	token, expires, err := s.createSession(ctx, user.ID)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create replacement session: %w", err)
	}
	return token, expires, nil
}

// ValidateNewPassword enforces a length-only policy. Length is the property
// that matters most for a self-hosted single-admin password, and it avoids
// rejecting long passphrases or non-ASCII passwords.
func ValidateNewPassword(password string) error {
	length := utf8.RuneCountInString(password)
	if strings.TrimSpace(password) == "" || length < PasswordMinRunes {
		return ErrPasswordTooShort
	}
	if length > PasswordMaxRunes {
		return ErrPasswordTooLong
	}
	return nil
}

// CreateSessionForUser issues a session after a non-password authentication
// ceremony (currently WebAuthn/passkey login).
func (s *Service) CreateSessionForUser(ctx context.Context, userID uuid.UUID) (string, time.Time, error) {
	return s.createSession(ctx, userID)
}

func (s *Service) createSession(ctx context.Context, userID uuid.UUID) (string, time.Time, error) {
	token, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().Add(s.sessionLifetime)
	_, err = s.queries.CreateSession(ctx, store.CreateSessionParams{
		UserID:          userID,
		ActiveAccountID: userID,
		TokenHash:       hashToken(token),
		ExpiresAt:       expires,
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return token, expires, nil
}

// SessionUser resolves a raw session token to its user and active account.
func (s *Service) SessionUser(ctx context.Context, token string) (store.User, uuid.UUID, error) {
	if token == "" {
		return store.User{}, uuid.Nil, ErrInvalidSession
	}
	row, err := s.queries.GetSessionUser(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.User{}, uuid.Nil, ErrInvalidSession
		}
		return store.User{}, uuid.Nil, fmt.Errorf("get session: %w", err)
	}
	return row.User, row.ActiveAccountID, nil
}

func (s *Service) SwitchAccount(ctx context.Context, token string, accountID uuid.UUID) error {
	user, _, err := s.SessionUser(ctx, token)
	if err != nil {
		return err
	}
	if _, err := s.queries.GetAccountMembership(ctx, store.GetAccountMembershipParams{
		AccountID: accountID,
		UserID:    user.ID,
	}); err != nil {
		return errors.New("not a member of this account")
	}
	n, err := s.queries.UpdateSessionAccount(ctx, store.UpdateSessionAccountParams{
		TokenHash:       hashToken(token),
		ActiveAccountID: accountID,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInvalidSession
	}
	return nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.queries.DeleteSession(ctx, hashToken(token))
}

func (s *Service) DeleteExpiredSessions(ctx context.Context) error {
	return s.queries.DeleteExpiredSessions(ctx)
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
