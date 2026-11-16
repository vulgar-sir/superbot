package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"superbot/internal/imagegen"
	"superbot/internal/store"
)

type imageGenerateTool struct {
	service *imagegen.Service
	db      *store.DB
	ownerID uuid.UUID
	botID   uuid.UUID
	dataDir string
}

func NewImageGenerateTool(service *imagegen.Service, db *store.DB, ownerID, botID uuid.UUID, dataDir string) Tool {
	return &imageGenerateTool{service: service, db: db, ownerID: ownerID, botID: botID, dataDir: dataDir}
}

func (t *imageGenerateTool) Name() string { return "generate_image" }
func (t *imageGenerateTool) Description() string {
	return "根据文字描述生成图片，并保存到 Bot 资料库。可传入参考图 URL（图生图）。生成是异步的，通常需要几十秒。"
}
func (t *imageGenerateTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"prompt":{"type":"string"},"size":{"type":"string"},"quality":{"type":"string","enum":["low","medium","high"]},"background":{"type":"string","enum":["auto","opaque","transparent"]},"reference_urls":{"type":"array","items":{"type":"string"}}},"required":["prompt"]}`)
}

func (t *imageGenerateTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	if !t.service.Configured() {
		return Result{Content: "图像生成服务未配置：需要 SUPERBOT_IMAGE_API_KEY", IsError: true}, nil
	}
	var input struct {
		Prompt        string   `json:"prompt"`
		Size          string   `json:"size"`
		Quality       string   `json:"quality"`
		Background    string   `json:"background"`
		ReferenceURLs []string `json:"reference_urls"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	images, err := t.service.Generate(ctx, imagegen.Request{
		Prompt: input.Prompt, Size: input.Size, Quality: input.Quality,
		Background: input.Background, ReferenceURLs: input.ReferenceURLs,
	})
	if err != nil {
		if errors.Is(err, imagegen.ErrNotConfigured) {
			return Result{Content: "图像生成服务未配置", IsError: true}, nil
		}
		return Result{Content: "生成图片失败: " + err.Error(), IsError: true}, nil
	}
	out := make([]map[string]string, 0, len(images))
	for _, image := range images {
		item, err := t.saveImage(ctx, input.Prompt, image)
		if err != nil {
			out = append(out, map[string]string{"url": image.URL, "note": err.Error()})
			continue
		}
		out = append(out, map[string]string{
			"library_item_id": item.ID.String(),
			"url":             "/api/library/" + item.ID.String() + "/file",
		})
	}
	encoded, _ := json.Marshal(map[string]any{"images": out})
	return Result{Content: string(encoded)}, nil
}

func (t *imageGenerateTool) saveImage(ctx context.Context, prompt string, image imagegen.Image) (store.LibraryItem, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, image.URL, nil)
	if err != nil {
		return store.LibraryItem{}, err
	}
	response, err := (&http.Client{Timeout: 60 * time.Second}).Do(request)
	if err != nil {
		return store.LibraryItem{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return store.LibraryItem{}, fmt.Errorf("download image: %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 25<<20))
	if err != nil || len(data) == 0 {
		return store.LibraryItem{}, errors.New("empty image")
	}
	extension := strings.ToLower(filepath.Ext(image.FileName))
	if extension == "" {
		extension = ".png"
	}
	directory := filepath.Join(t.dataDir, "workspaces", t.botID.String(), "generated")
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return store.LibraryItem{}, err
	}
	filePath := filepath.Join(directory, uuid.NewString()+extension)
	if err := os.WriteFile(filePath, data, 0o640); err != nil {
		return store.LibraryItem{}, err
	}
	mimeType := http.DetectContentType(data)
	title := strings.TrimSpace(prompt)
	if len([]rune(title)) > 80 {
		title = string([]rune(title)[:80])
	}
	metadata, _ := json.Marshal(map[string]string{"source": "imagegen", "original_url": image.URL})
	item, err := t.db.CreateLibraryItem(ctx, store.CreateLibraryItemParams{
		OwnerID: t.ownerID, BotID: t.botID, Kind: "media", Title: title,
		Description: "AI 生成图片", Content: "", MimeType: mimeType,
		FilePath: filePath, Metadata: metadata,
	})
	if err != nil {
		_ = os.Remove(filePath)
		return store.LibraryItem{}, err
	}
	return item, nil
}
