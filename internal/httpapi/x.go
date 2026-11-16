package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type xPublishRequest struct {
	Text    string `json:"text"`
	ReplyTo string `json:"reply_to"`
}

type xPublishResponse struct {
	Data struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"data"`
	Detail string `json:"detail"`
	Title  string `json:"title"`
}

func (s *Server) xRoutes(r chi.Router) {
	r.Post("/publish", s.handlePublishXPost)
}

func (s *Server) handlePublishXPost(w http.ResponseWriter, r *http.Request) {
	var req xPublishRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" || len([]rune(text)) > 280 {
		writeError(w, http.StatusBadRequest, "invalid_request", "text must be 1-280 characters")
		return
	}
	connector, err := s.connectors.ByProvider(r.Context(), accountFrom(r), "x")
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusServiceUnavailable, "requires_config", "connect an X account in Extensions first")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load X connector")
		return
	}
	token, err := s.connectors.AccessToken(connector)
	if err != nil {
		writeError(w, http.StatusBadRequest, "requires_config", "X connector token is unavailable")
		return
	}
	payload := map[string]any{"text": text}
	if replyTo := strings.TrimSpace(req.ReplyTo); replyTo != "" {
		payload["reply"] = map[string]string{"in_reply_to_tweet_id": replyTo}
	}
	body, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.x.com/2/tweets", bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not build X request")
		return
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		writeError(w, http.StatusBadGateway, "x_failed", err.Error())
		return
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(raw))
		if len(message) > 400 {
			message = message[:400]
		}
		writeError(w, http.StatusBadGateway, "x_failed", message)
		return
	}
	var result xPublishResponse
	if err := json.Unmarshal(raw, &result); err != nil || result.Data.ID == "" {
		writeError(w, http.StatusBadGateway, "x_failed", "X returned an unexpected response")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": result.Data.ID, "url": "https://x.com/i/web/status/" + result.Data.ID})
}
