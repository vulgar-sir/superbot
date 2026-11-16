package devices

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

var (
	ErrNotFound     = errors.New("device not found")
	ErrForbidden    = errors.New("device permission denied")
	ErrInvalidNonce = errors.New("invalid job nonce or job already completed")
)

const (
	maxOutputBytes = 256 * 1024
	defaultTimeout = 120 * time.Second
)

type Service struct {
	db      *store.DB
	dataDir string
}

func NewService(db *store.DB, dataDir string) *Service {
	return &Service{db: db, dataDir: dataDir}
}

func (s *Service) DataDir() string { return s.dataDir }

type Created struct {
	Device store.Device
	Token  string
}

func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, name, platform string) (Created, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Created{}, errors.New("device name is required")
	}
	token, err := randomToken()
	if err != nil {
		return Created{}, err
	}
	device, err := s.db.CreateDevice(ctx, store.CreateDeviceParams{
		OwnerID:   ownerID,
		Name:      name,
		TokenHash: hashToken(token),
		Platform:  strings.TrimSpace(platform),
	})
	if err != nil {
		return Created{}, fmt.Errorf("create device: %w", err)
	}
	return Created{Device: device, Token: token}, nil
}

func (s *Service) List(ctx context.Context, ownerID uuid.UUID) ([]store.Device, error) {
	return s.db.ListDevicesByOwner(ctx, ownerID)
}

func (s *Service) Get(ctx context.Context, ownerID, deviceID uuid.UUID) (store.Device, error) {
	device, err := s.db.GetDevice(ctx, store.GetDeviceParams{ID: deviceID, OwnerID: ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Device{}, ErrNotFound
		}
		return store.Device{}, err
	}
	return device, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (store.Device, error) {
	if strings.TrimSpace(token) == "" {
		return store.Device{}, ErrForbidden
	}
	device, err := s.db.GetDeviceByTokenHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Device{}, ErrForbidden
		}
		return store.Device{}, err
	}
	return device, nil
}

func (s *Service) Delete(ctx context.Context, ownerID, deviceID uuid.UUID) error {
	n, err := s.db.DeleteDevice(ctx, store.DeleteDeviceParams{ID: deviceID, OwnerID: ownerID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) RegisterPublicKey(ctx context.Context, deviceID uuid.UUID, pemText string) error {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemText)))
	if block == nil {
		return errors.New("invalid public key PEM")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return errors.New("invalid public key")
	}
	if _, ok := key.(ed25519.PublicKey); !ok {
		return errors.New("public key must be Ed25519")
	}
	return s.db.SetDevicePublicKey(ctx, store.SetDevicePublicKeyParams{ID: deviceID, PublicKey: pemText})
}

func (s *Service) SetAllowBot(ctx context.Context, ownerID, deviceID uuid.UUID, allow bool) (store.Device, error) {
	device, err := s.db.SetDeviceAllowBot(ctx, store.SetDeviceAllowBotParams{
		ID: deviceID, OwnerID: ownerID, AllowBot: allow,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Device{}, ErrNotFound
		}
		return store.Device{}, err
	}
	return device, nil
}

func (s *Service) Poll(ctx context.Context, deviceID uuid.UUID) (*store.DeviceJob, error) {
	job, err := s.db.ClaimDeviceJob(ctx, deviceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_ = s.db.TouchDevice(ctx, deviceID)
			return nil, nil
		}
		return nil, err
	}
	_ = s.db.TouchDevice(ctx, deviceID)
	return &job, nil
}

func (s *Service) Complete(ctx context.Context, deviceID, jobID uuid.UUID, nonce, output, jobErr string) error {
	if len(output) > maxOutputBytes {
		output = output[:maxOutputBytes] + "\n…[truncated]"
	}
	if jobErr != "" {
		n, err := s.db.FailDeviceJob(ctx, store.FailDeviceJobParams{
			ID: jobID, DeviceID: deviceID, Output: output, Error: jobErr, Nonce: nonce,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrInvalidNonce
		}
		return nil
	}
	n, err := s.db.CompleteDeviceJob(ctx, store.CompleteDeviceJobParams{
		ID: jobID, DeviceID: deviceID, Output: output, Error: "", Nonce: nonce,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInvalidNonce
	}
	return nil
}

// CompleteArtifact stores a screenshot/file uploaded by the device and marks
// the job done. The file lives under data/devices/<device>/artifacts.
func (s *Service) CompleteArtifact(ctx context.Context, deviceID, jobID uuid.UUID, nonce, filename string, data []byte) (string, error) {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "" || filename == "." {
		filename = "artifact.bin"
	}
	dir := filepath.Join(s.dataDir, "devices", deviceID.String(), "artifacts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, jobID.String()+"-"+filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	n, err := s.db.CompleteDeviceJobArtifact(ctx, store.CompleteDeviceJobArtifactParams{
		ID:           jobID,
		DeviceID:     deviceID,
		Output:       "artifact uploaded",
		ArtifactPath: path,
		Nonce:        nonce,
	})
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if n == 0 {
		_ = os.Remove(path)
		return "", ErrInvalidNonce
	}
	return path, nil
}

func (s *Service) GetJob(ctx context.Context, ownerID, jobID uuid.UUID) (store.DeviceJob, error) {
	return s.db.GetDeviceJob(ctx, store.GetDeviceJobParams{ID: jobID, OwnerID: ownerID})
}

func (s *Service) CreateJob(ctx context.Context, ownerID, deviceID uuid.UUID, kind, command string) (store.DeviceJob, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "shell"
	}
	switch kind {
	case "shell", "screenshot", "clipboard_read", "clipboard_write", "fs_read", "fs_write", "app_list", "app_open", "location", "audio_list", "http_request":
	default:
		return store.DeviceJob{}, errors.New("unsupported device job kind")
	}
	command = strings.TrimSpace(command)
	if (kind == "shell" || kind == "clipboard_write" || kind == "fs_read" || kind == "fs_write" || kind == "app_open" || kind == "http_request") && command == "" {
		return store.DeviceJob{}, errors.New("command is required")
	}
	nonce, err := randomToken()
	if err != nil {
		return store.DeviceJob{}, err
	}
	return s.db.CreateDeviceJob(ctx, store.CreateDeviceJobParams{
		OwnerID: ownerID, DeviceID: deviceID, Kind: kind, Command: command, Nonce: nonce,
	})
}

// Wait blocks until a device job finishes, for synchronous Bot tool calls.
func (s *Service) Wait(ctx context.Context, ownerID, jobID uuid.UUID, timeout time.Duration) (store.DeviceJob, error) {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		job, err := s.db.GetDeviceJob(ctx, store.GetDeviceJobParams{ID: jobID, OwnerID: ownerID})
		if err != nil {
			return store.DeviceJob{}, err
		}
		if job.Status == "done" || job.Status == "failed" {
			return job, nil
		}
		if time.Now().After(deadline) {
			return job, errors.New("device job timed out")
		}
		select {
		case <-ctx.Done():
			return job, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (s *Service) FindByName(ctx context.Context, ownerID uuid.UUID, name string) (store.Device, error) {
	items, err := s.List(ctx, ownerID)
	if err != nil {
		return store.Device{}, err
	}
	for _, item := range items {
		if strings.EqualFold(item.Name, strings.TrimSpace(name)) {
			return item, nil
		}
	}
	return store.Device{}, ErrNotFound
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
