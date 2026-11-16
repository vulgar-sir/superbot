package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type collected struct {
	text      string
	reasoning string
	deltas    int
	calls     []ToolCall
	usage     *Usage
	finish    string
	done      bool
	err       error
}

// collect drains a stream, failing the test if it does not end promptly.
func collect(t *testing.T, s Stream) collected {
	t.Helper()
	var out collected
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				return out
			}
			switch ev.Type {
			case EventTextDelta:
				out.text += ev.Text
				out.deltas++
			case EventReasoningDelta:
				out.reasoning += ev.Text
			case EventToolCalls:
				out.calls = append(out.calls, ev.ToolCalls...)
			case EventUsage:
				out.usage = ev.Usage
			case EventDone:
				out.done = true
				out.finish = ev.FinishReason
			case EventError:
				out.err = ev.Err
			}
		case <-deadline:
			t.Fatal("stream did not finish in time")
		}
	}
}

// sleepRecorder replaces the transport's backoff sleep so retries are instant.
type sleepRecorder struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (r *sleepRecorder) sleep(ctx context.Context, d time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.delays = append(r.delays, d)
	return ctx.Err()
}

type sseResponse struct {
	status int
	header map[string]string
	body   string
}

// sseServer serves the scripted responses in order (repeating the last one)
// and records every request.
type sseServer struct {
	*httptest.Server
	mu      sync.Mutex
	bodies  [][]byte
	headers []http.Header
	paths   []string
}

func newSSEServer(t *testing.T, responses ...sseResponse) *sseServer {
	t.Helper()
	s := &sseServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		n := len(s.bodies)
		s.bodies = append(s.bodies, body)
		s.headers = append(s.headers, r.Header.Clone())
		s.paths = append(s.paths, r.URL.Path)
		s.mu.Unlock()
		resp := responses[min(n, len(responses)-1)]
		for k, v := range resp.header {
			w.Header().Set(k, v)
		}
		if resp.status == 0 || resp.status == http.StatusOK {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.WriteHeader(resp.status)
		}
		_, _ = io.WriteString(w, resp.body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *sseServer) attempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func (s *sseServer) body(i int) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[i]
}

func (s *sseServer) request(i int) (http.Header, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.headers[i], s.paths[i]
}

// stallingServer sends prefix, flushes, then holds the connection open.
func stallingServer(t *testing.T, prefix string) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, prefix)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})
	return server
}
