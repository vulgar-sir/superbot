package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/imagegen"
	"superbot/internal/store"
)

const maxAvatarBytes = 4 << 20

var avatarMimeExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

func (s *Server) avatarFilePath(botID uuid.UUID) string {
	return filepath.Join(s.cfg.DataDir, "avatars", botID.String())
}

// loadBotForAvatar resolves the bot and the caller's role on it. It writes the
// error response itself and returns ok=false when the request must stop.
func (s *Server) loadBotForAvatar(w http.ResponseWriter, r *http.Request) (store.Bot, string, bool) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return store.Bot{}, "", false
	}
	accountID := accountFrom(r)
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "bot not found")
			return store.Bot{}, "", false
		}
		s.logger.Error("load bot for avatar failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not load bot")
		return store.Bot{}, "", false
	}
	accountRole, _ := s.accountRole(r.Context(), accountID, userFrom(r).ID)
	role, allowed := s.botAccess(r.Context(), accountRole, userFrom(r).ID, bot)
	if !allowed {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return store.Bot{}, "", false
	}
	return bot, role, true
}

func (s *Server) handleGetBotAvatar(w http.ResponseWriter, r *http.Request) {
	bot, _, ok := s.loadBotForAvatar(w, r)
	if !ok {
		return
	}
	path := s.avatarFilePath(bot.ID)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "not_found", "avatar not found")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}

func (s *Server) handleUploadBotAvatar(w http.ResponseWriter, r *http.Request) {
	bot, role, ok := s.loadBotForAvatar(w, r)
	if !ok {
		return
	}
	if role != "editor" {
		writeError(w, http.StatusForbidden, "forbidden", "you cannot edit this bot")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBytes+1<<20)
	if err := r.ParseMultipartForm(maxAvatarBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "image is too large or malformed")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "file is required")
		return
	}
	defer file.Close()

	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not read image")
		return
	}
	head = head[:n]
	if _, allowed := avatarMimeExtensions[http.DetectContentType(head)]; !allowed {
		writeError(w, http.StatusBadRequest, "unsupported_media", "avatar must be a PNG, JPEG, WebP or GIF image")
		return
	}
	if err := s.writeAvatarFile(bot.ID, io.MultiReader(bytes.NewReader(head), file)); err != nil {
		s.logger.Error("save avatar failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not save avatar")
		return
	}
	s.finishAvatarUpdate(w, r, bot)
}

func (s *Server) handleGenerateBotAvatar(w http.ResponseWriter, r *http.Request) {
	bot, role, ok := s.loadBotForAvatar(w, r)
	if !ok {
		return
	}
	if role != "editor" {
		writeError(w, http.StatusForbidden, "forbidden", "you cannot edit this bot")
		return
	}
	if !s.imagegen.Configured() {
		writeError(w, http.StatusConflict, "requires_config", "图像生成未配置：需要 SUPERBOT_IMAGE_API_KEY")
		return
	}
	var input struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "prompt is required")
		return
	}
	images, err := s.imagegen.Generate(r.Context(), imagegen.Request{
		Prompt: "Bot avatar, centered, simple background: " + prompt,
		Size:   "1024x1024",
	})
	if err != nil {
		s.logger.Error("generate avatar failed", "error", err)
		writeError(w, http.StatusBadGateway, "image_generation_failed", err.Error())
		return
	}
	data, err := downloadImage(r, images[0].URL)
	if err != nil {
		s.logger.Error("download generated avatar failed", "error", err)
		writeError(w, http.StatusBadGateway, "image_generation_failed", "could not download generated image")
		return
	}
	if err := s.writeAvatarBytes(bot.ID, data); err != nil {
		s.logger.Error("save generated avatar failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not save avatar")
		return
	}
	s.finishAvatarUpdate(w, r, bot)
}

func (s *Server) handleDeleteBotAvatar(w http.ResponseWriter, r *http.Request) {
	bot, role, ok := s.loadBotForAvatar(w, r)
	if !ok {
		return
	}
	if role != "editor" {
		writeError(w, http.StatusForbidden, "forbidden", "you cannot edit this bot")
		return
	}
	_ = os.Remove(s.avatarFilePath(bot.ID))
	updated, err := s.db.SetBotAvatar(r.Context(), store.SetBotAvatarParams{
		ID: bot.ID, OwnerID: accountFrom(r), Avatar: "",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not clear avatar")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bot": toBotDTO(updated)})
}

func (s *Server) finishAvatarUpdate(w http.ResponseWriter, r *http.Request, bot store.Bot) {
	version := time.Now().UTC().UnixNano()
	updated, err := s.db.SetBotAvatar(r.Context(), store.SetBotAvatarParams{
		ID:      bot.ID,
		OwnerID: accountFrom(r),
		Avatar:  "/api/bots/" + bot.ID.String() + "/avatar?v=" + strconv.FormatInt(version, 10),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update avatar")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bot": toBotDTO(updated)})
}

func (s *Server) writeAvatarFile(botID uuid.UUID, reader io.Reader) error {
	directory := filepath.Join(s.cfg.DataDir, "avatars")
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, botID.String()+"-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, io.LimitReader(reader, maxAvatarBytes)); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, s.avatarFilePath(botID))
}

func (s *Server) writeAvatarBytes(botID uuid.UUID, data []byte) error {
	return s.writeAvatarFile(botID, bytes.NewReader(data))
}

func downloadImage(r *http.Request, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := (&http.Client{Timeout: 60 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, errors.New("image download: " + response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 10<<20))
}
