package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"superbot/internal/llm"
	"superbot/internal/store"
)

type analyzeCaptureRequest struct {
	Duration    int      `json:"duration"`
	Description string   `json:"description"`
	Frames      []string `json:"frames"`
}

// handleAnalyzeCapture turns keyframes captured during a screen recording into
// a Markdown Skill draft using the account's vision-capable primary Bot model.
func (s *Server) handleAnalyzeCapture(w http.ResponseWriter, r *http.Request) {
	var req analyzeCaptureRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(req.Frames) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "at least one frame is required")
		return
	}
	if len(req.Frames) > 8 {
		req.Frames = req.Frames[:8]
	}
	account, err := s.db.GetAccountByID(r.Context(), accountFrom(r))
	if err != nil || account.PrimaryBotID == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "account has no primary bot")
		return
	}
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: *account.PrimaryBotID, OwnerID: accountFrom(r)})
	if err != nil || bot.ProviderID == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "primary bot has no provider")
		return
	}
	modelRow, err := s.db.GetModelByName(r.Context(), store.GetModelByNameParams{
		ProviderID: *bot.ProviderID, Name: bot.Model,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "model not found")
		return
	}
	if !modelRow.SupportsVision {
		writeError(w, http.StatusBadRequest, "requires_config", "current model does not support vision")
		return
	}
	providerRow, err := s.db.GetProvider(r.Context(), store.GetProviderParams{ID: *bot.ProviderID, OwnerID: accountFrom(r)})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "provider not found")
		return
	}
	provider, err := s.factory.Build(providerRow, s.cfg.LLMTimeout)
	if err != nil {
		writeError(w, http.StatusBadRequest, "provider_failed", err.Error())
		return
	}
	prompt := "用户说明：" + strings.TrimSpace(req.Description) +
		"\n录制时长：" + strconv.Itoa(req.Duration) + " 秒。\n请根据这些关键帧生成 Skill 草稿。"
	parts := []llm.Part{{Type: llm.PartText, Text: prompt}}
	for _, frame := range req.Frames {
		comma := strings.Index(frame, ",")
		if comma < 0 {
			continue
		}
		header := frame[:comma]
		if !strings.HasPrefix(header, "data:image/") {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(frame[comma+1:])
		if err != nil || len(data) == 0 || len(data) > 4<<20 {
			continue
		}
		mimeType := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
		parts = append(parts, llm.Part{
			Type:  llm.PartImage,
			Image: &llm.Image{MIMEType: mimeType, Data: data},
		})
	}
	if len(parts) == 1 {
		writeError(w, http.StatusBadRequest, "invalid_request", "no valid image frames")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.LLMTimeout)
	defer cancel()
	stream, err := provider.Stream(ctx, llm.Request{
		Model: bot.Model,
		Messages: []llm.Message{
			llm.TextMessage(llm.RoleSystem, "你是任务录制助手。根据录屏关键帧和用户说明，用中文输出 Markdown 格式的 Skill 草稿，包含：目标、前置条件、按顺序的步骤（写出看到的页面/按钮）、注意点与待确认项。只依据关键帧，不要编造未出现的内容。"),
			{Role: llm.RoleUser, Parts: parts},
		},
		MaxTokens: 1600,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "analyze_failed", err.Error())
		return
	}
	defer stream.Close()
	var content strings.Builder
	for event := range stream.Events() {
		switch event.Type {
		case llm.EventTextDelta:
			content.WriteString(event.Text)
		case llm.EventError:
			writeError(w, http.StatusBadGateway, "analyze_failed", event.Err.Error())
			return
		}
	}
	text := strings.TrimSpace(content.String())
	if text == "" {
		writeError(w, http.StatusBadGateway, "analyze_failed", "model returned no content")
		return
	}
	encoded, _ := json.Marshal(map[string]any{"content": text, "frames": len(parts) - 1})
	writeJSON(w, http.StatusOK, json.RawMessage(encoded))
}
