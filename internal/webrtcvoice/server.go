package webrtcvoice

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

type Config struct {
	UDPServerPort int
	PublicIP      string
	STTURL        string
	STTAPIKey     string
	STTModel      string
}

type Server struct {
	api    *webrtc.API
	cfg    Config
	logger *slog.Logger
	mu     sync.Mutex
	peers  map[*webrtc.PeerConnection]struct{}
	sendMu sync.Mutex
}

func New(cfg Config, logger *slog.Logger) (*Server, error) {
	if cfg.UDPServerPort <= 0 || strings.TrimSpace(cfg.PublicIP) == "" {
		return nil, errors.New("webrtc voice is not configured")
	}
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: cfg.UDPServerPort})
	if err != nil {
		return nil, fmt.Errorf("listen webrtc udp: %w", err)
	}
	mux := webrtc.NewICEUDPMux(nil, udpConn)
	settings := webrtc.SettingEngine{}
	settings.SetICEUDPMux(mux)
	settings.SetNAT1To1IPs([]string{strings.TrimSpace(cfg.PublicIP)}, webrtc.ICECandidateTypeHost)
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	media := &webrtc.MediaEngine{}
	if err := media.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	api := webrtc.NewAPI(webrtc.WithSettingEngine(settings), webrtc.WithMediaEngine(media))
	return &Server{api: api, cfg: cfg, logger: logger, peers: make(map[*webrtc.PeerConnection]struct{})}, nil
}

func (s *Server) HandleOffer(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SDP string `json:"sdp"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&request); err != nil || strings.TrimSpace(request.SDP) == "" {
		http.Error(w, "sdp is required", http.StatusBadRequest)
		return
	}
	pc, err := s.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pc.OnDataChannel(func(channel *webrtc.DataChannel) {
		s.handleDataChannel(pc, channel)
	})
	s.mu.Lock()
	s.peers[pc] = struct{}{}
	s.mu.Unlock()
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		s.logger.Info("webrtc connection state", "state", state.String())
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			s.mu.Lock()
			delete(s.peers, pc)
			s.mu.Unlock()
			_ = pc.Close()
		}
	})
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: request.SDP}); err != nil {
		_ = pc.Close()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		_ = pc.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		_ = pc.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	select {
	case <-gatherComplete:
	case <-time.After(10 * time.Second):
	}
	local := pc.LocalDescription()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"type": "answer", "sdp": local.SDP})
}

func (s *Server) handleDataChannel(pc *webrtc.PeerConnection, channel *webrtc.DataChannel) {
	s.logger.Info("webrtc data channel opened", "label", channel.Label())
	var mu sync.Mutex
	buffer := make([]byte, 0, 64*1024)
	flush := func() {
		mu.Lock()
		if len(buffer) < 3200 {
			mu.Unlock()
			return
		}
		chunk := append([]byte(nil), buffer...)
		buffer = buffer[:0]
		mu.Unlock()
		s.logger.Info("webrtc audio flush", "bytes", len(chunk))
		go s.transcribe(pc, channel, chunk)
	}
	channel.OnMessage(func(message webrtc.DataChannelMessage) {
		var payload struct {
			Type       string `json:"type"`
			Data       string `json:"data"`
			SampleRate int    `json:"sample_rate"`
		}
		if !message.IsString || json.Unmarshal(message.Data, &payload) != nil {
			return
		}
		if payload.Type != "audio" {
			return
		}
		audio, err := base64.StdEncoding.DecodeString(payload.Data)
		if err != nil || len(audio) == 0 {
			return
		}
		mu.Lock()
		if len(buffer)+len(audio) <= 256*1024 {
			buffer = append(buffer, audio...)
		}
		shouldFlush := len(buffer) >= 32*1024
		mu.Unlock()
		if shouldFlush {
			flush()
		}
	})
	channel.OnClose(func() {
		flush()
		_ = pc.Close()
	})
}

func (s *Server) transcribe(pc *webrtc.PeerConnection, channel *webrtc.DataChannel, pcm []byte) {
	if s.cfg.STTURL == "" {
		s.sendJSON(channel, map[string]any{"type": "error", "message": "STT is not configured"})
		return
	}
	wav := pcmToWAV(pcm, 16000)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "webrtc.wav")
	if err != nil {
		return
	}
	if _, err := part.Write(wav); err != nil {
		return
	}
	_ = writer.WriteField("model", s.cfg.STTModel)
	_ = writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.STTURL, &body)
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if s.cfg.STTAPIKey != "" {
		request.Header.Set("Authorization", "Bearer "+s.cfg.STTAPIKey)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		s.sendJSON(channel, map[string]any{"type": "error", "message": err.Error()})
		return
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		s.sendJSON(channel, map[string]any{"type": "error", "message": string(raw)})
		return
	}
	var result struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &result) == nil && strings.TrimSpace(result.Text) != "" {
		s.logger.Info("webrtc transcript", "text", strings.TrimSpace(result.Text))
		s.sendJSON(channel, map[string]any{"type": "transcript", "text": strings.TrimSpace(result.Text)})
	} else {
		s.logger.Warn("webrtc stt empty", "status", response.StatusCode, "body", string(raw))
	}
}

func (s *Server) sendJSON(channel *webrtc.DataChannel, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if err := channel.SendText(string(payload)); err != nil {
		s.logger.Warn("webrtc send failed", "error", err)
	}
}

func pcmToWAV(pcm []byte, sampleRate int) []byte {
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+len(pcm)))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(len(pcm)))
	return append(header, pcm...)
}
