package dockerproxy

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDaemon imitates the parts of the Docker Engine API the sandbox uses.
// It listens on a unix socket, like the real daemon.
type fakeDaemon struct {
	socket string

	mu         sync.Mutex
	requests   []recordedRequest
	containers []fakeContainer
	seq        int

	// execMode selects what an exec start does: "" writes one stdout
	// frame ("hi\n") after the upgrade, "echo" echoes the raw stream.
	execMode string
	// streamGate, when set, makes a non-upgraded exec start write "first",
	// flush, wait for the gate, then write "second".
	streamGate chan struct{}
}

type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   []byte
}

type fakeContainer struct {
	ID     string
	Name   string
	Labels map[string]string
}

var fakeVersionRE = regexp.MustCompile(`^/v[0-9]+\.[0-9]+`)

func newFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	// Unix socket paths are limited to ~108 bytes; t.TempDir() can exceed
	// that on Windows, so use a short directory.
	dir, err := os.MkdirTemp("", "dpx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	f := &fakeDaemon{socket: filepath.Join(dir, "d.sock")}
	ln, err := net.Listen("unix", f.socket)
	if err != nil {
		t.Fatalf("listen on unix socket: %v", err)
	}
	srv := &http.Server{Handler: f}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return f
}

// fakeID returns a 64-hex id; distinct n give distinct prefixes.
func fakeID(n int) string {
	sum := sha256.Sum256([]byte(strconv.Itoa(n)))
	return hex.EncodeToString(sum[:])
}

func (f *fakeDaemon) addContainer(name string, labels map[string]string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	id := fakeID(0xc000 + f.seq)
	f.containers = append(f.containers, fakeContainer{ID: id, Name: name, Labels: labels})
	return id
}

func (f *fakeDaemon) lookup(ref string) (fakeContainer, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.containers {
		if c.ID == ref || c.Name == ref {
			return c, true
		}
	}
	for _, c := range f.containers {
		if len(ref) >= 4 && strings.HasPrefix(c.ID, ref) {
			return c, true
		}
	}
	return fakeContainer{}, false
}

func (f *fakeDaemon) recorded() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedRequest(nil), f.requests...)
}

func (f *fakeDaemon) count(method, pathSuffix string) int {
	n := 0
	for _, r := range f.recorded() {
		if r.Method == method && strings.HasSuffix(r.Path, pathSuffix) {
			n++
		}
	}
	return n
}

func (f *fakeDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, recordedRequest{
		Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Header: r.Header.Clone(), Body: body,
	})
	f.mu.Unlock()

	p := fakeVersionRE.ReplaceAllString(r.URL.Path, "")
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	notFound := func() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"No such object"}`)
	}
	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case p == "/_ping":
		w.Header().Set("API-Version", "1.47")
		w.Header().Set("OSType", "linux")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, "OK")
		}
	case p == "/containers/create" && r.Method == http.MethodPost:
		var req struct{ Labels map[string]string }
		_ = json.Unmarshal(body, &req)
		id := f.addContainer(r.URL.Query().Get("name"), req.Labels)
		writeJSON(http.StatusCreated, map[string]any{"Id": id, "Warnings": []string{}})
	case len(segs) == 3 && segs[0] == "containers" && segs[2] == "json":
		c, ok := f.lookup(segs[1])
		if !ok {
			notFound()
			return
		}
		writeJSON(http.StatusOK, map[string]any{
			"Id": c.ID, "Name": "/" + c.Name,
			"State":  map[string]any{"Running": true, "Status": "running"},
			"Config": map[string]any{"Labels": c.Labels},
		})
	case len(segs) == 3 && segs[0] == "containers" && segs[2] == "exec":
		if _, ok := f.lookup(segs[1]); !ok {
			notFound()
			return
		}
		f.mu.Lock()
		f.seq++
		id := fakeID(0xe000 + f.seq)
		f.mu.Unlock()
		writeJSON(http.StatusCreated, map[string]string{"Id": id})
	case len(segs) == 3 && segs[0] == "containers":
		if _, ok := f.lookup(segs[1]); !ok {
			notFound()
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case len(segs) == 2 && segs[0] == "containers" && r.Method == http.MethodDelete:
		if _, ok := f.lookup(segs[1]); !ok {
			notFound()
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case len(segs) == 3 && segs[0] == "exec" && segs[2] == "start":
		f.execStart(w, r, body)
	case len(segs) == 3 && segs[0] == "exec" && segs[2] == "json":
		writeJSON(http.StatusOK, map[string]any{"ID": segs[1], "Running": false, "ExitCode": 0})
	default:
		writeJSON(http.StatusOK, map[string]string{"reached": p})
	}
}

func (f *fakeDaemon) execStart(w http.ResponseWriter, r *http.Request, body []byte) {
	var req struct{ Detach bool }
	_ = json.Unmarshal(body, &req)
	if req.Detach {
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "tcp") {
		conn, brw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = brw.WriteString("HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.multiplexed-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
		_ = brw.Flush()
		if f.execMode == "echo" {
			_, _ = io.Copy(conn, brw.Reader)
			return
		}
		_, _ = conn.Write(stdFrame(1, "hi\n"))
		return
	}
	// Not upgraded: stream the output as the response body.
	w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "first")
	http.NewResponseController(w).Flush()
	if f.streamGate != nil {
		select {
		case <-f.streamGate:
		case <-time.After(10 * time.Second):
		}
	}
	_, _ = io.WriteString(w, "second")
}

// stdFrame encodes one frame of Docker's multiplexed stream.
func stdFrame(stream byte, payload string) []byte {
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	return append(header, payload...)
}

// syncBuffer collects log output from concurrent handlers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

const testBindRoot = "/srv/superbot/data"

func testConfig(socket string) Config {
	return Config{
		Socket:   socket,
		Listen:   "127.0.0.1:0",
		Images:   []string{"superbot/computer:latest", "superbot/computer-desktop:latest"},
		Networks: []string{"superbot_sandbox"},
		BindRoot: testBindRoot,
	}
}

// harness is a proxy served over TCP in front of a fake daemon.
type harness struct {
	daemon *fakeDaemon
	proxy  *Proxy
	server *httptest.Server
	logs   *syncBuffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	daemon := newFakeDaemon(t)
	logs := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	proxy, err := New(testConfig(daemon.socket), logger)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)
	return &harness{daemon: daemon, proxy: proxy, server: server, logs: logs}
}

func (h *harness) do(t *testing.T, method, path string, body []byte) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, data
}

// rawUpgrade sends an upgrade request over a raw connection and returns the
// connection positioned after the response headers.
func rawUpgrade(t *testing.T, addr, path, body string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: docker\r\nContent-Type: application/json\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Length: %d\r\n\r\n%s", path, len(body), body)
	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("read upgrade response: %v", err)
	}
	return conn, reader, res
}
