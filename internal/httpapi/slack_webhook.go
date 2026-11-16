package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/i18n"
	"superbot/internal/store"
)

type slackEnvelope struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	TeamID    string `json:"team_id"`
	Event     struct {
		Type        string `json:"type"`
		Subtype     string `json:"subtype"`
		Text        string `json:"text"`
		User        string `json:"user"`
		Channel     string `json:"channel"`
		ChannelType string `json:"channel_type"`
		TS          string `json:"ts"`
		BotID       string `json:"bot_id"`
	} `json:"event"`
}

func truncateSlackText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func (s *Server) handleSlackWebhook(w http.ResponseWriter, r *http.Request) {
	connectorID, err := uuid.Parse(chi.URLParam(r, "connectorID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid connector id")
		return
	}
	connector, err := s.db.GetConnectorByID(r.Context(), connectorID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "connector not found")
		return
	}
	var cfg struct {
		SigningSecret string `json:"signing_secret"`
	}
	_ = json.Unmarshal(connector.Config, &cfg)
	if strings.TrimSpace(cfg.SigningSecret) == "" {
		writeError(w, http.StatusServiceUnavailable, "requires_config", "Slack signing_secret is not configured")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not read body")
		return
	}
	timestamp := r.Header.Get("X-Slack-Request-Timestamp")
	signature := r.Header.Get("X-Slack-Signature")
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(ts, 0)).Abs() > 5*time.Minute {
		writeError(w, http.StatusUnauthorized, "invalid_signature", "stale or invalid timestamp")
		return
	}
	mac := hmac.New(sha256.New, []byte(cfg.SigningSecret))
	_, _ = mac.Write([]byte("v0:" + timestamp + ":" + string(body)))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		writeError(w, http.StatusUnauthorized, "invalid_signature", "invalid Slack signature")
		return
	}
	var envelope slackEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid Slack payload")
		return
	}
	if envelope.Type == "url_verification" {
		writeJSON(w, http.StatusOK, map[string]string{"challenge": envelope.Challenge})
		return
	}
	if envelope.Type != "event_callback" || envelope.Event.Type != "message" ||
		envelope.Event.Subtype != "" || envelope.Event.BotID != "" || strings.TrimSpace(envelope.Event.Text) == "" {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	meta, _ := json.Marshal(map[string]any{
		"connector_id": connector.ID.String(),
		"channel":      envelope.Event.Channel,
		"user":         envelope.Event.User,
		"ts":           envelope.Event.TS,
		"team_id":      envelope.TeamID,
	})
	language := i18n.AccountLanguage(r.Context(), s.db, connector.OwnerID)
	_, err = s.db.CreateNotification(r.Context(), store.CreateNotificationParams{
		OwnerID:      connector.OwnerID,
		Kind:         "slack",
		Title:        i18n.Text(language, "Slack 消息", "Slack message"),
		Body:         truncateSlackText(envelope.Event.Text, 500),
		ResourceType: "connector",
		ResourceID:   connector.ID.String(),
		Metadata:     meta,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not store Slack event")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
