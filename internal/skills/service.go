package skills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

const (
	maxSkillContent = 256 * 1024
	maxNameLength   = 80
)

type Service struct {
	db      *store.DB
	dataDir string
	logger  *slog.Logger
}

func NewService(db *store.DB, dataDir string, logger *slog.Logger) *Service {
	return &Service{db: db, dataDir: dataDir, logger: logger}
}

// Create writes SKILL.md to disk and records the skill. User-created skills
// are active immediately; bot-proposed skills start as drafts.
func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, name, description, content, createdBy string) (store.Skill, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	content = strings.TrimSpace(content)
	if name == "" {
		return store.Skill{}, fmt.Errorf("skill name is required")
	}
	if len([]rune(name)) > maxNameLength {
		return store.Skill{}, fmt.Errorf("skill name is too long")
	}
	if content == "" {
		return store.Skill{}, fmt.Errorf("skill content is required")
	}
	if len(content) > maxSkillContent {
		return store.Skill{}, fmt.Errorf("skill content is too large (max %d bytes)", maxSkillContent)
	}
	status := "active"
	source := "user"
	if createdBy == "bot" {
		status = "draft"
		source = "bot"
	}
	skill, err := s.db.CreateSkill(ctx, store.CreateSkillParams{
		OwnerID:     ownerID,
		Name:        name,
		Description: description,
		Version:     "1",
		Source:      source,
		Status:      status,
		CreatedBy:   createdBy,
		FilesPath:   "",
	})
	if err != nil {
		return store.Skill{}, err
	}
	dir := filepath.Join(s.dataDir, "skills", skill.ID.String())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		_, _ = s.db.DeleteSkill(ctx, store.DeleteSkillParams{ID: skill.ID, OwnerID: ownerID})
		return store.Skill{}, fmt.Errorf("create skill dir: %w", err)
	}
	filePath := filepath.Join(dir, "SKILL.md")
	body := fmt.Sprintf("---\nname: %s\ndescription: %s\nversion: 1\n---\n\n%s\n", name, description, content)
	if err := os.WriteFile(filePath, []byte(body), 0o644); err != nil {
		_, _ = s.db.DeleteSkill(ctx, store.DeleteSkillParams{ID: skill.ID, OwnerID: ownerID})
		return store.Skill{}, fmt.Errorf("write SKILL.md: %w", err)
	}
	updated, err := s.db.SetSkillFilesPath(ctx, store.SetSkillFilesPathParams{
		ID:        skill.ID,
		OwnerID:   ownerID,
		FilesPath: filePath,
	})
	if err != nil {
		return skill, fmt.Errorf("record skill path: %w", err)
	}
	return updated, nil
}

func (s *Service) List(ctx context.Context, ownerID uuid.UUID) ([]store.Skill, error) {
	return s.db.ListSkills(ctx, ownerID)
}

func (s *Service) Get(ctx context.Context, ownerID, skillID uuid.UUID) (store.Skill, error) {
	skill, err := s.db.GetSkill(ctx, store.GetSkillParams{ID: skillID, OwnerID: ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Skill{}, fmt.Errorf("skill not found")
		}
		return store.Skill{}, err
	}
	return skill, nil
}

func (s *Service) Approve(ctx context.Context, ownerID, skillID uuid.UUID) (store.Skill, error) {
	return s.db.SetSkillStatus(ctx, store.SetSkillStatusParams{
		ID:      skillID,
		OwnerID: ownerID,
		Status:  "active",
	})
}

func (s *Service) Disable(ctx context.Context, ownerID, skillID uuid.UUID) (store.Skill, error) {
	return s.db.SetSkillStatus(ctx, store.SetSkillStatusParams{
		ID:      skillID,
		OwnerID: ownerID,
		Status:  "disabled",
	})
}

func (s *Service) Delete(ctx context.Context, ownerID, skillID uuid.UUID) error {
	n, err := s.db.DeleteSkill(ctx, store.DeleteSkillParams{ID: skillID, OwnerID: ownerID})
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("skill not found")
	}
	_ = os.RemoveAll(filepath.Join(s.dataDir, "skills", skillID.String()))
	return nil
}

func (s *Service) ListForBot(ctx context.Context, botID uuid.UUID) ([]store.Skill, error) {
	return s.db.ListEnabledSkills(ctx, botID)
}

func (s *Service) ReadContent(ctx context.Context, ownerID, botID uuid.UUID, name string) (string, error) {
	items, err := s.ListForBot(ctx, botID)
	if err != nil {
		return "", err
	}
	builtins, err := s.ListBuiltins(ctx, ownerID)
	if err == nil {
		seen := make(map[uuid.UUID]bool, len(items))
		for _, item := range items {
			seen[item.ID] = true
		}
		for _, item := range builtins {
			if !seen[item.ID] {
				items = append(items, item)
			}
		}
	}
	for _, skill := range items {
		if strings.EqualFold(skill.Name, strings.TrimSpace(name)) {
			data, err := os.ReadFile(skill.FilesPath)
			if err != nil {
				return "", fmt.Errorf("read skill file: %w", err)
			}
			if len(data) > 32*1024 {
				data = data[:32*1024]
			}
			return string(data), nil
		}
	}
	return "", fmt.Errorf("skill %q is not enabled for this bot", name)
}

func (s *Service) SetBotSkills(ctx context.Context, ownerID, botID uuid.UUID, skillIDs []uuid.UUID) error {
	if _, err := s.db.GetBot(ctx, store.GetBotParams{ID: botID, OwnerID: ownerID}); err != nil {
		return fmt.Errorf("bot not found")
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.db.WithTx(tx)
	if err := q.ClearBotSkills(ctx, botID); err != nil {
		return err
	}
	seen := make(map[uuid.UUID]bool)
	for _, skillID := range skillIDs {
		if seen[skillID] {
			continue
		}
		seen[skillID] = true
		if _, err := q.GetSkill(ctx, store.GetSkillParams{ID: skillID, OwnerID: ownerID}); err != nil {
			return fmt.Errorf("skill not found")
		}
		if err := q.UpsertBotSkill(ctx, store.UpsertBotSkillParams{BotID: botID, SkillID: skillID}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
