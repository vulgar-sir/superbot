package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"superbot/internal/config"
)

func TestVoiceTranscribeWithMockProvider(t *testing.T) {
	var gotModel, gotFile string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseMultipartForm(5 << 20); err != nil {
			http.Error(w, "bad multipart", http.StatusBadRequest)
			return
		}
		gotModel = r.FormValue("model")
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "missing file", http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		gotFile = header.Filename + ":" + string(data)
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "mock transcript"})
	}))
	defer provider.Close()

	server := &Server{cfg: config.Config{
		STTURL: provider.URL + "/v1/audio/transcriptions", STTModel: "whisper-1", STTAPIKey: "test-key",
	}}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "voice.webm")
	_, _ = part.Write([]byte("audio-bytes"))
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	server.handleVoiceTranscribe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "mock transcript") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	if gotModel != "whisper-1" || gotFile != "voice.webm:audio-bytes" {
		t.Fatalf("unexpected provider input: model=%q file=%q", gotModel, gotFile)
	}
}

func TestVoiceSpeakWithMockProvider(t *testing.T) {
	var gotInput, gotVoice string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model string `json:"model"`
			Input string `json:"input"`
			Voice string `json:"voice"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		gotInput = payload.Input
		gotVoice = payload.Voice
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3mock-mp3"))
	}))
	defer provider.Close()

	server := &Server{cfg: config.Config{
		TTSURL: provider.URL + "/v1/audio/speech", TTSModel: "tts-1", TTSVoice: "alloy", TTSAPIKey: "test-key",
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/voice/speak", strings.NewReader(`{"text":"hello","voice":"nova"}`))
	rec := httptest.NewRecorder()
	server.handleVoiceSpeak(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("status %d type %q body %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if gotInput != "hello" || gotVoice != "nova" {
		t.Fatalf("unexpected provider input: input=%q voice=%q", gotInput, gotVoice)
	}
}

func TestVoiceRequiresConfig(t *testing.T) {
	server := &Server{cfg: config.Config{}}
	req := httptest.NewRequest(http.MethodPost, "/api/voice/speak", strings.NewReader(`{"text":"hello"}`))
	rec := httptest.NewRecorder()
	server.handleVoiceSpeak(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "requires_config") {
		t.Fatalf("unexpected response: %d %s", rec.Code, rec.Body.String())
	}
}
