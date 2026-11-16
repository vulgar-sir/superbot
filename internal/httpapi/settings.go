package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/i18n"
	"superbot/internal/store"
)

type notificationPrefs struct {
	Desktop   bool    `json:"desktop"`
	Sound     string  `json:"sound"`
	Volume    float64 `json:"volume"`
	Messages  bool    `json:"messages"`
	Approvals bool    `json:"approvals"`
	Agents    bool    `json:"agents"`
}

func defaultNotificationPrefs() notificationPrefs {
	return notificationPrefs{
		Desktop:   true,
		Sound:     "default",
		Volume:    0.6,
		Messages:  true,
		Approvals: true,
		Agents:    true,
	}
}

func notificationSettingsKey(accountID uuid.UUID) string {
	return "notification_prefs:" + accountID.String()
}

func normalizeNotificationPrefs(prefs notificationPrefs) notificationPrefs {
	switch prefs.Sound {
	case "default", "chime", "soft", "none":
	default:
		prefs.Sound = "default"
	}
	if prefs.Volume < 0 {
		prefs.Volume = 0
	}
	if prefs.Volume > 1 {
		prefs.Volume = 1
	}
	return prefs
}

func (s *Server) settingsRoutes(r chi.Router) {
	r.Get("/notifications", s.handleGetNotificationPrefs)
	r.Put("/notifications", s.handlePutNotificationPrefs)
	r.Get("/data-sharing", s.handleGetDataSharing)
	r.Put("/data-sharing", s.handlePutDataSharing)
	r.Get("/timezone", s.handleGetTimezone)
	r.Put("/timezone", s.handlePutTimezone)
	r.Get("/language", s.handleGetLanguage)
	r.Put("/language", s.handlePutLanguage)
}

type languagePrefs struct {
	Language string `json:"language"`
}

func languageKey(accountID uuid.UUID) string {
	return "language:" + accountID.String()
}

func (s *Server) handleGetLanguage(w http.ResponseWriter, r *http.Request) {
	prefs := languagePrefs{Language: i18n.Chinese}
	raw, err := s.db.GetSetting(r.Context(), languageKey(accountFrom(r)))
	if err == nil {
		_ = json.Unmarshal(raw, &prefs.Language)
	}
	prefs.Language = i18n.Normalize(prefs.Language)
	writeJSON(w, http.StatusOK, prefs)
}

func (s *Server) handlePutLanguage(w http.ResponseWriter, r *http.Request) {
	var req languagePrefs
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	raw, _ := json.Marshal(i18n.Normalize(req.Language))
	if err := s.db.UpsertSetting(r.Context(), store.UpsertSettingParams{
		Key: languageKey(accountFrom(r)), Value: raw,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save language")
		return
	}
	writeJSON(w, http.StatusOK, languagePrefs{Language: i18n.Normalize(req.Language)})
}

type timezonePrefs struct {
	Timezone string `json:"timezone"`
}

func timezoneKey(accountID uuid.UUID) string {
	return "timezone:" + accountID.String()
}

func (s *Server) handleGetTimezone(w http.ResponseWriter, r *http.Request) {
	prefs := timezonePrefs{}
	raw, err := s.db.GetSetting(r.Context(), timezoneKey(accountFrom(r)))
	if err == nil {
		_ = json.Unmarshal(raw, &prefs)
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (s *Server) handlePutTimezone(w http.ResponseWriter, r *http.Request) {
	var prefs timezonePrefs
	if err := decodeJSON(w, r, &prefs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	prefs.Timezone = strings.TrimSpace(prefs.Timezone)
	if prefs.Timezone != "" {
		if _, err := time.LoadLocation(prefs.Timezone); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid timezone")
			return
		}
	}
	raw, err := json.Marshal(prefs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not encode timezone")
		return
	}
	if err := s.db.UpsertSetting(r.Context(), store.UpsertSettingParams{
		Key: timezoneKey(accountFrom(r)), Value: raw,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save timezone")
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

type dataSharingPrefs struct {
	Enabled bool `json:"enabled"`
}

func dataSharingKey(accountID uuid.UUID) string {
	return "data_sharing:" + accountID.String()
}

func (s *Server) handleGetDataSharing(w http.ResponseWriter, r *http.Request) {
	prefs := dataSharingPrefs{}
	raw, err := s.db.GetSetting(r.Context(), dataSharingKey(accountFrom(r)))
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "internal", "could not load data sharing settings")
			return
		}
	} else {
		_ = json.Unmarshal(raw, &prefs)
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (s *Server) handlePutDataSharing(w http.ResponseWriter, r *http.Request) {
	var prefs dataSharingPrefs
	if err := decodeJSON(w, r, &prefs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	raw, err := json.Marshal(prefs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not encode data sharing settings")
		return
	}
	if err := s.db.UpsertSetting(r.Context(), store.UpsertSettingParams{
		Key:   dataSharingKey(accountFrom(r)),
		Value: raw,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save data sharing settings")
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (s *Server) dataSharingEnabled(ctx context.Context, accountID uuid.UUID) bool {
	raw, err := s.db.GetSetting(ctx, dataSharingKey(accountID))
	if err != nil {
		return false
	}
	var prefs dataSharingPrefs
	if json.Unmarshal(raw, &prefs) != nil {
		return false
	}
	return prefs.Enabled
}

func (s *Server) handleGetNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	prefs := defaultNotificationPrefs()
	raw, err := s.db.GetSetting(r.Context(), notificationSettingsKey(accountFrom(r)))
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "internal", "could not load notification settings")
			return
		}
	} else if err := json.Unmarshal(raw, &prefs); err != nil {
		prefs = defaultNotificationPrefs()
	}
	writeJSON(w, http.StatusOK, map[string]any{"preferences": normalizeNotificationPrefs(prefs)})
}

func (s *Server) handlePutNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	var prefs notificationPrefs
	if err := decodeJSON(w, r, &prefs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	prefs = normalizeNotificationPrefs(prefs)
	raw, err := json.Marshal(prefs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not encode notification settings")
		return
	}
	err = s.db.UpsertSetting(r.Context(), store.UpsertSettingParams{
		Key:   notificationSettingsKey(accountFrom(r)),
		Value: raw,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save notification settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"preferences": prefs})
}
