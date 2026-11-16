package httpapi

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"superbot/internal/tunnel"
)

func (s *Server) handleDeviceTunnel(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	device, err := s.devices.Authenticate(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid device token")
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	session := s.tunnelHub.Register(device.ID, conn)
	defer s.tunnelHub.Unregister(device.ID, session)
	ctx := r.Context()
	for {
		_, payload, err := conn.Read(ctx)
		if err != nil {
			return
		}
		session.Handle(ctx, payload)
	}
}

func (s *Server) TunnelHub() *tunnel.Hub { return s.tunnelHub }

func (s *Server) ProxyHandler() http.Handler {
	return http.HandlerFunc(s.handleEgressProxy)
}

func (s *Server) handleEgressProxy(w http.ResponseWriter, r *http.Request) {
	if s.cfg.TunnelToken == "" {
		writeError(w, http.StatusServiceUnavailable, "requires_config", "egress tunnel is not configured")
		return
	}
	username, password, ok := parseProxyAuth(r.Header.Get("Proxy-Authorization"))
	if !ok || password != s.cfg.TunnelToken {
		w.Header().Set("Proxy-Authenticate", `Basic realm="superbot"`)
		writeError(w, http.StatusProxyAuthRequired, "unauthorized", "proxy authentication required")
		return
	}
	deviceID, err := uuid.Parse(strings.TrimSpace(username))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "proxy username must be a device id")
		return
	}
	device, err := s.tunnelHub.Get(deviceID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "device_offline", "device tunnel is offline")
		return
	}
	if r.Method == http.MethodConnect {
		s.proxyConnect(w, r, device)
		return
	}
	s.proxyPlainHTTP(w, r, device)
}

func (s *Server) proxyConnect(w http.ResponseWriter, r *http.Request, device *tunnel.Device) {
	target := r.Host
	if !strings.Contains(target, ":") {
		target += ":443"
	}
	s.proxyTunnel(w, device, target, nil)
}

func (s *Server) proxyPlainHTTP(w http.ResponseWriter, r *http.Request, device *tunnel.Device) {
	target := r.URL.Host
	if target == "" {
		target = r.Host
	}
	if !strings.Contains(target, ":") {
		target += ":80"
	}
	r.RequestURI = ""
	r.URL.Scheme = ""
	r.URL.Host = ""
	r.Header.Del("Proxy-Authorization")
	var requestBuffer strings.Builder
	if err := r.Write(&requestBuffer); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not forward request")
		return
	}
	s.proxyTunnel(w, device, target, []byte(requestBuffer.String()))
}

func (s *Server) proxyTunnel(w http.ResponseWriter, device *tunnel.Device, target string, initial []byte) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "hijacking is not supported")
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	openCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, err := device.Open(openCtx, target)
	if err != nil {
		_, _ = buffered.WriteString("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 18\r\nConnection: close\r\n\r\ndevice dial failed")
		_ = buffered.Flush()
		return
	}
	if initial == nil {
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		if err := buffered.Flush(); err != nil {
			device.CloseConn(context.Background(), session.ID)
			return
		}
	} else if err := device.SendData(context.Background(), session.ID, initial); err != nil {
		device.CloseConn(context.Background(), session.ID)
		return
	}
	go func() {
		for {
			select {
			case data := <-session.Data:
				if _, err := client.Write(data); err != nil {
					device.CloseConn(context.Background(), session.ID)
					return
				}
			case <-session.Closed():
				return
			}
		}
	}()
	buffer := make([]byte, 32*1024)
	for {
		count, readErr := buffered.Read(buffer)
		if count > 0 {
			if err := device.SendData(context.Background(), session.ID, buffer[:count]); err != nil {
				break
			}
		}
		if readErr != nil {
			break
		}
	}
	device.CloseConn(context.Background(), session.ID)
}

func parseProxyAuth(header string) (string, string, bool) {
	value := strings.TrimSpace(header)
	if !strings.HasPrefix(strings.ToLower(value), "basic ") {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value[6:]))
	if err != nil {
		return "", "", false
	}
	username, password, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return "", "", false
	}
	return username, password, true
}
