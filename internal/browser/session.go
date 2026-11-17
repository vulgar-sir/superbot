package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
	"github.com/google/uuid"

	"superbot/internal/sandbox"
)

// Session is one browser tab inside the bot's persistent computer container.
// Closing the session closes the tab; cookies and the Chrome profile persist
// in /workspace/.chromium.
type Session struct {
	ctx         context.Context
	cancel      context.CancelFunc
	allocCancel context.CancelFunc
	info        sandbox.Info
	// mu serializes tool calls: several cycles for the same bot can run
	// concurrently, but a browser tab is a single shared resource.
	mu sync.Mutex
}

func NewSession(ctx context.Context, manager *sandbox.Manager, ownerID, botID uuid.UUID, profile string) (*Session, error) {
	info, err := manager.Ensure(ctx, ownerID, botID, profile)
	if err != nil {
		return nil, err
	}
	if info.CDPAddr == "" {
		return nil, fmt.Errorf("computer did not publish a CDP address")
	}
	wsURL, err := browserWebSocketURL(ctx, info)
	if err != nil {
		return nil, err
	}
	// The session outlives the cycle that created it: approvals resume a run
	// in a new cycle, and the page must keep its URL, DOM and element refs.
	base := context.WithoutCancel(ctx)
	// Every connection to the computer's DevTools carries its gate secret,
	// so chromedp gets the full WebSocket address instead of discovering it.
	allocCtx, allocCancel := remote.NewAllocator(base, wsURL, remote.NoModifyURL,
		remote.WithDialHTTPHeader(info.GateHeaders()))
	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	// The first Run binds the browser's lifetime to taskCtx. Do not wrap this
	// call in a short timeout.
	if err := chromedp.Do(taskCtx, chromedp.Navigate("about:blank")); err != nil {
		taskCancel()
		allocCancel()
		return nil, fmt.Errorf("create browser tab: %w", err)
	}
	return &Session{ctx: taskCtx, cancel: taskCancel, allocCancel: allocCancel, info: info}, nil
}

// Alive reports whether the tab still answers CDP calls (the computer may
// have been stopped or restarted behind our back).
func (s *Session) Alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	pingCtx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
	defer cancel()
	_, err := chromedp.Run(pingCtx, chromedp.Title())
	return err == nil
}

func (s *Session) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.allocCancel != nil {
		s.allocCancel()
	}
}

func (s *Session) runCtx(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return context.WithTimeout(s.ctx, timeout)
}

func (s *Session) WorkspacePath() string { return s.info.Workspace }

// browserWebSocketURL asks the computer's DevTools endpoint (through its
// gate) for the browser's WebSocket address and points it at the computer.
func browserWebSocketURL(ctx context.Context, info sandbox.Info) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+info.CDPAddr+"/json/version", nil)
	if err != nil {
		return "", err
	}
	req.Header = info.GateHeaders()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("reach the browser: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("browser endpoint answered %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return rewriteWebSocketHost(body, info.CDPAddr)
}

// rewriteWebSocketHost takes webSocketDebuggerUrl from a /json/version reply
// and replaces its loopback host with addr.
func rewriteWebSocketHost(body []byte, addr string) (string, error) {
	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &version); err != nil || version.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("browser endpoint did not report a WebSocket address")
	}
	parsed, err := url.Parse(version.WebSocketDebuggerURL)
	if err != nil {
		return "", fmt.Errorf("invalid WebSocket address: %w", err)
	}
	parsed.Host = addr
	return parsed.String(), nil
}
