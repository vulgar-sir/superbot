package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (s *Server) voiceRoutes(r chi.Router) {
	r.Get("/status", s.handleVoiceStatus)
	r.Post("/transcribe", s.handleVoiceTranscribe)
	r.Post("/speak", s.handleVoiceSpeak)
	r.Post("/webrtc/offer", s.handleVoiceWebRTCOffer)
}

func (s *Server) handleVoiceStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"stt": map[string]any{
			"configured": s.cfg.STTURL != "",
			"model":      s.cfg.STTModel,
		},
		"tts": map[string]any{
			"configured": s.cfg.TTSURL != "",
			"model":      s.cfg.TTSModel,
			"voice":      s.cfg.TTSVoice,
		},
		"webrtc": map[string]any{
			"configured": s.webrtcVoice != nil,
			"udp_port":   s.cfg.WebRTCUDPPort,
		},
	})
}

func (s *Server) handleVoiceWebRTCOffer(w http.ResponseWriter, r *http.Request) {
	if s.webrtcVoice == nil {
		writeError(w, http.StatusServiceUnavailable, "requires_config", "WebRTC voice is not configured")
		return
	}
	s.webrtcVoice.HandleOffer(w, r)
}

func (s *Server) handleVoiceTranscribe(w http.ResponseWriter, r *http.Request) {
	if s.cfg.STTURL == "" {
		writeError(w, http.StatusServiceUnavailable, "requires_config", "STT provider is not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 25<<20)
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid multipart body")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "file is required")
		return
	}
	defer file.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", header.Filename)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not build STT request")
		return
	}
	if _, err := io.Copy(part, io.LimitReader(file, 25<<20)); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not read audio")
		return
	}
	_ = writer.WriteField("model", s.cfg.STTModel)
	_ = writer.Close()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.cfg.STTURL, &body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not build STT request")
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if s.cfg.STTAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.STTAPIKey)
	}
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "stt_failed", err.Error())
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeError(w, http.StatusBadGateway, "stt_failed", string(data))
		return
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &result); err != nil || strings.TrimSpace(result.Text) == "" {
		writeError(w, http.StatusBadGateway, "stt_failed", "provider returned no transcript")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": result.Text})
}

func (s *Server) handleVoiceSpeak(w http.ResponseWriter, r *http.Request) {
	if s.cfg.TTSURL == "" {
		writeError(w, http.StatusServiceUnavailable, "requires_config", "TTS provider is not configured")
		return
	}
	var req struct {
		Text  string `json:"text"`
		Voice string `json:"voice"`
	}
	if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "text is required")
		return
	}
	voice := strings.TrimSpace(req.Voice)
	if voice == "" {
		voice = s.cfg.TTSVoice
	}
	payload, _ := json.Marshal(map[string]string{
		"model": s.cfg.TTSModel, "input": req.Text, "voice": voice, "response_format": "mp3",
	})
	outbound, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.cfg.TTSURL, bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not build TTS request")
		return
	}
	outbound.Header.Set("Content-Type", "application/json")
	if s.cfg.TTSAPIKey != "" {
		outbound.Header.Set("Authorization", "Bearer "+s.cfg.TTSAPIKey)
	}
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(outbound)
	if err != nil {
		writeError(w, http.StatusBadGateway, "tts_failed", err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		writeError(w, http.StatusBadGateway, "tts_failed", string(data))
		return
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "audio/mpeg"
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 25<<20))
}
