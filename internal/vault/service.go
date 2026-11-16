package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/secrets"
	"superbot/internal/store"
)

type Service struct {
	db  *store.DB
	box *secrets.Box
}

func NewService(db *store.DB, box *secrets.Box) *Service {
	return &Service{db: db, box: box}
}

type Input struct {
	Kind     string
	Name     string
	Username string
	Password string
	URL      string
	Notes    string
}

func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, input Input) (store.VaultItem, error) {
	input.Kind = normalizeKind(input.Kind)
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return store.VaultItem{}, errors.New("name is required")
	}
	var passwordEnc, notesEnc []byte
	var err error
	if input.Password != "" {
		passwordEnc, err = s.box.EncryptString(input.Password)
		if err != nil {
			return store.VaultItem{}, fmt.Errorf("encrypt password: %w", err)
		}
	}
	if input.Notes != "" {
		notesEnc, err = s.box.EncryptString(input.Notes)
		if err != nil {
			return store.VaultItem{}, fmt.Errorf("encrypt notes: %w", err)
		}
	}
	return s.db.CreateVaultItem(ctx, store.CreateVaultItemParams{
		OwnerID:     ownerID,
		Kind:        input.Kind,
		Name:        input.Name,
		Username:    strings.TrimSpace(input.Username),
		PasswordEnc: passwordEnc,
		Url:         strings.TrimSpace(input.URL),
		NotesEnc:    notesEnc,
		Metadata:    json.RawMessage(`{}`),
	})
}

func (s *Service) Update(ctx context.Context, ownerID, itemID uuid.UUID, input Input) (store.VaultItem, error) {
	existing, err := s.db.GetVaultItem(ctx, store.GetVaultItemParams{ID: itemID, OwnerID: ownerID})
	if err != nil {
		return store.VaultItem{}, errors.New("vault item not found")
	}
	input.Kind = normalizeKind(input.Kind)
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return store.VaultItem{}, errors.New("name is required")
	}
	passwordEnc := existing.PasswordEnc
	notesEnc := existing.NotesEnc
	if input.Password != "" {
		passwordEnc, err = s.box.EncryptString(input.Password)
		if err != nil {
			return store.VaultItem{}, err
		}
	}
	if input.Notes != "" {
		notesEnc, err = s.box.EncryptString(input.Notes)
		if err != nil {
			return store.VaultItem{}, err
		}
	}
	return s.db.UpdateVaultItem(ctx, store.UpdateVaultItemParams{
		ID:          itemID,
		OwnerID:     ownerID,
		Kind:        input.Kind,
		Name:        input.Name,
		Username:    strings.TrimSpace(input.Username),
		PasswordEnc: passwordEnc,
		Url:         strings.TrimSpace(input.URL),
		NotesEnc:    notesEnc,
	})
}

func (s *Service) List(ctx context.Context, ownerID uuid.UUID) ([]store.VaultItem, error) {
	return s.db.ListVaultItems(ctx, ownerID)
}

func (s *Service) Reveal(ctx context.Context, ownerID, itemID uuid.UUID) (store.VaultItem, string, string, error) {
	item, err := s.db.GetVaultItem(ctx, store.GetVaultItemParams{ID: itemID, OwnerID: ownerID})
	if err != nil {
		return store.VaultItem{}, "", "", errors.New("vault item not found")
	}
	password, notes := "", ""
	if len(item.PasswordEnc) > 0 {
		password, err = s.box.DecryptString(item.PasswordEnc)
		if err != nil {
			return store.VaultItem{}, "", "", fmt.Errorf("decrypt password: %w", err)
		}
	}
	if len(item.NotesEnc) > 0 {
		notes, err = s.box.DecryptString(item.NotesEnc)
		if err != nil {
			return store.VaultItem{}, "", "", fmt.Errorf("decrypt notes: %w", err)
		}
	}
	return item, password, notes, nil
}

func (s *Service) Delete(ctx context.Context, ownerID, itemID uuid.UUID) error {
	n, err := s.db.DeleteVaultItem(ctx, store.DeleteVaultItemParams{ID: itemID, OwnerID: ownerID})
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("vault item not found")
	}
	return nil
}

func normalizeKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case "payment", "contact":
		return strings.TrimSpace(kind)
	default:
		return "login"
	}
}
