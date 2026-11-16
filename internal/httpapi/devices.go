package httpapi

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/devices"
	"superbot/internal/store"
)

type deviceDTO struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Platform   string     `json:"platform"`
	Status     string     `json:"status"`
	AllowBot   bool       `json:"allow_bot"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func toDeviceDTO(device store.Device) deviceDTO {
	return deviceDTO{
		ID: device.ID, Name: device.Name, Platform: device.Platform,
		Status: device.Status, AllowBot: device.AllowBot,
		LastSeenAt: device.LastSeenAt, CreatedAt: device.CreatedAt,
	}
}

func (s *Server) deviceRoutes(r chi.Router) {
	r.Get("/", s.handleListDevices)
	r.Post("/", s.handleCreateDevice)
	r.Route("/{deviceID}", func(r chi.Router) {
		r.Delete("/", s.handleDeleteDevice)
		r.Patch("/allow-bot", s.handleSetDeviceAllowBot)
		r.Post("/exec", s.handleExecDevice)
		r.Post("/jobs", s.handleCreateDeviceJob)
		r.Post("/screenshot", s.handleDeviceScreenshot)
		r.Get("/jobs/{jobID}/artifact", s.handleDeviceArtifactForOwner)
	})
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	items, err := s.devices.List(r.Context(), accountFrom(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list devices")
		return
	}
	out := make([]deviceDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toDeviceDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

type createDeviceRequest struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

func (s *Server) handleCreateDevice(w http.ResponseWriter, r *http.Request) {
	var req createDeviceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	created, err := s.devices.Create(r.Context(), accountFrom(r), req.Name, req.Platform)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"device": toDeviceDTO(created.Device),
		"token":  created.Token,
	})
}

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := parseUUIDParam(w, r, "deviceID")
	if !ok {
		return
	}
	if err := s.devices.Delete(r.Context(), accountFrom(r), deviceID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "device not found")
		return
	}
	writeNoContent(w)
}

type allowBotRequest struct {
	AllowBot bool `json:"allow_bot"`
}

func (s *Server) handleSetDeviceAllowBot(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := parseUUIDParam(w, r, "deviceID")
	if !ok {
		return
	}
	var req allowBotRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	device, err := s.devices.SetAllowBot(r.Context(), accountFrom(r), deviceID, req.AllowBot)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "device not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"device": toDeviceDTO(device)})
}

type execDeviceRequest struct {
	Command string `json:"command"`
}

func (s *Server) handleExecDevice(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := parseUUIDParam(w, r, "deviceID")
	if !ok {
		return
	}
	var req execDeviceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	accountID := accountFrom(r)
	if _, err := s.devices.Get(r.Context(), accountID, deviceID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "device not found")
		return
	}
	job, err := s.devices.CreateJob(r.Context(), accountID, deviceID, "shell", req.Command)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	result, err := s.devices.Wait(r.Context(), accountID, job.ID, 0)
	if err != nil {
		writeError(w, http.StatusGatewayTimeout, "device_timeout", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": result.Status,
		"output": result.Output,
		"error":  result.Error,
	})
}

func (s *Server) deviceAgentRoutes(r chi.Router) {
	r.Get("/pubkey", s.handleDevicePublicKey)
	r.Post("/register-key", s.handleDeviceRegisterKey)
	r.Post("/poll", s.handleDevicePoll)
	r.Post("/jobs/{jobID}/result", s.handleDeviceJobResult)
	r.Post("/jobs/{jobID}/artifact", s.handleDeviceArtifact)
}

func (s *Server) handleDeviceArtifact(w http.ResponseWriter, r *http.Request) {
	device, err := s.devices.Authenticate(r.Context(), s.deviceToken(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid device token")
		return
	}
	jobID, ok := parseUUIDParam(w, r, "jobID")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid multipart body")
		return
	}
	nonce := r.FormValue("nonce")
	signature := r.FormValue("signature")
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "file is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 16<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not read file")
		return
	}
	if device.PublicKey == "" || !verifyDeviceSignature(device.PublicKey, artifactSignaturePayload(jobID.String(), nonce, data), signature) {
		writeError(w, http.StatusUnauthorized, "invalid_signature", "invalid device artifact signature")
		return
	}
	if _, err := s.devices.CompleteArtifact(r.Context(), device.ID, jobID, nonce, header.Filename, data); err != nil {
		if errors.Is(err, devices.ErrInvalidNonce) {
			writeError(w, http.StatusBadRequest, "invalid_nonce", "invalid job nonce")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not store artifact")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeviceScreenshot(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := parseUUIDParam(w, r, "deviceID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	if _, err := s.devices.Get(r.Context(), accountID, deviceID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "device not found")
		return
	}
	job, err := s.devices.CreateJob(r.Context(), accountID, deviceID, "screenshot", "")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	result, err := s.devices.Wait(r.Context(), accountID, job.ID, 0)
	if err != nil {
		writeError(w, http.StatusGatewayTimeout, "device_timeout", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": result.Status,
		"url":    "/api/devices/" + deviceID.String() + "/jobs/" + job.ID.String() + "/artifact",
		"error":  result.Error,
	})
}

func (s *Server) handleDeviceArtifactForOwner(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := parseUUIDParam(w, r, "deviceID")
	if !ok {
		return
	}
	jobID, ok := parseUUIDParam(w, r, "jobID")
	if !ok {
		return
	}
	job, err := s.devices.GetJob(r.Context(), accountFrom(r), jobID)
	if err != nil || job.DeviceID != deviceID {
		writeError(w, http.StatusNotFound, "not_found", "artifact not found")
		return
	}
	if job.ArtifactPath == "" {
		writeError(w, http.StatusNotFound, "not_found", "artifact not found")
		return
	}
	http.ServeFile(w, r, job.ArtifactPath)
}

func (s *Server) deviceToken(r *http.Request) string {
	if raw := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(raw, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))
	}
	return strings.TrimSpace(r.Header.Get("X-Device-Token"))
}

func (s *Server) handleDevicePublicKey(w http.ResponseWriter, r *http.Request) {
	pub, ok := s.deviceSignKey.Public().(ed25519.PublicKey)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "signing key unavailable")
		return
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not encode public key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"algorithm":  "ed25519",
		"public_key": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})),
	})
}

func (s *Server) handleDevicePoll(w http.ResponseWriter, r *http.Request) {
	device, err := s.devices.Authenticate(r.Context(), s.deviceToken(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid device token")
		return
	}
	job, err := s.devices.Poll(r.Context(), device.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not poll device job")
		return
	}
	if job == nil {
		writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	payload := job.ID.String() + "|" + job.Kind + "|" + job.Command + "|" + job.Nonce
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(s.deviceSignKey, []byte(payload)))
	writeJSON(w, http.StatusOK, map[string]any{"job": map[string]any{
		"id": job.ID, "kind": job.Kind, "command": job.Command, "created_at": job.CreatedAt,
		"nonce": job.Nonce, "signature": signature,
	}})
}

type deviceJobResultRequest struct {
	Nonce     string `json:"nonce"`
	Output    string `json:"output"`
	Error     string `json:"error"`
	Signature string `json:"signature"`
}

func (s *Server) handleDeviceRegisterKey(w http.ResponseWriter, r *http.Request) {
	device, err := s.devices.Authenticate(r.Context(), s.deviceToken(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid device token")
		return
	}
	var req struct {
		PublicKey string `json:"public_key"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := s.devices.RegisterPublicKey(r.Context(), device.ID, req.PublicKey); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func verifyDeviceSignature(publicKeyPEM, payload, signature string) bool {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return false
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return false
	}
	pub, ok := key.(ed25519.PublicKey)
	if !ok {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	return ed25519.Verify(pub, []byte(payload), sig)
}

func resultSignaturePayload(jobID, nonce, output, errText string) string {
	outHash := sha256.Sum256([]byte(output))
	errHash := sha256.Sum256([]byte(errText))
	return jobID + "|" + nonce + "|" + hex.EncodeToString(outHash[:]) + "|" + hex.EncodeToString(errHash[:])
}

func artifactSignaturePayload(jobID, nonce string, data []byte) string {
	hash := sha256.Sum256(data)
	return jobID + "|" + nonce + "|" + hex.EncodeToString(hash[:])
}

func (s *Server) handleDeviceJobResult(w http.ResponseWriter, r *http.Request) {
	device, err := s.devices.Authenticate(r.Context(), s.deviceToken(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid device token")
		return
	}
	jobID, ok := parseUUIDParam(w, r, "jobID")
	if !ok {
		return
	}
	var req deviceJobResultRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if device.PublicKey == "" || !verifyDeviceSignature(device.PublicKey, resultSignaturePayload(jobID.String(), req.Nonce, req.Output, req.Error), req.Signature) {
		writeError(w, http.StatusUnauthorized, "invalid_signature", "invalid device result signature")
		return
	}
	if err := s.devices.Complete(r.Context(), device.ID, jobID, req.Nonce, req.Output, req.Error); err != nil {
		if errors.Is(err, devices.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not save job result")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCreateDeviceJob(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := parseUUIDParam(w, r, "deviceID")
	if !ok {
		return
	}
	var req struct {
		Kind    string `json:"kind"`
		Command string `json:"command"`
		Async   bool   `json:"async"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	accountID := accountFrom(r)
	if _, err := s.devices.Get(r.Context(), accountID, deviceID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "device not found")
		return
	}
	job, err := s.devices.CreateJob(r.Context(), accountID, deviceID, req.Kind, req.Command)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Async {
		writeJSON(w, http.StatusAccepted, map[string]any{"job_id": job.ID, "status": job.Status})
		return
	}
	result, err := s.devices.Wait(r.Context(), accountID, job.ID, 0)
	if err != nil {
		writeError(w, http.StatusGatewayTimeout, "device_timeout", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": result.Status,
		"output": result.Output,
		"error":  result.Error,
	})
}
