package dockerproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPathAllowlist(t *testing.T) {
	h := newHarness(t)
	h.daemon.addContainer("superbot-bot-abc123", map[string]string{botIDLabel: "x"})
	foreign := h.daemon.addContainer("superbot-postgres", nil)
	labelled := h.daemon.addContainer("renamed", map[string]string{botIDLabel: "y"})

	cases := []struct {
		method, path string
		want         int
	}{
		// Allowed: what internal/sandbox calls.
		{"HEAD", "/_ping", 200},
		{"GET", "/_ping", 200},
		{"GET", "/v1.47/_ping", 200},
		{"GET", "/v1.47/containers/superbot-bot-abc123/json", 200},
		{"GET", "/containers/superbot-bot-abc123/json", 200},
		{"GET", "/v1.47/containers/superbot-bot-missing/json", 404},
		{"POST", "/v1.47/containers/superbot-bot-abc123/start", 204},
		{"POST", "/v1.47/containers/superbot-bot-abc123/stop?t=5", 204},
		{"POST", "/v1.47/containers/superbot-bot-abc123/restart?signal=SIGKILL&t=2", 204},
		{"DELETE", "/v1.47/containers/superbot-bot-abc123?force=1", 204},
		{"POST", "/v1.47/containers/" + labelled + "/start", 204},
		{"DELETE", "/v1.56/containers/" + labelled + "?force=1", 204},
		// Listing is allowed but always narrowed to sandbox containers
		// (see TestContainerListIsForcedToSandboxContainers).
		{"GET", "/v1.47/containers/json?all=1", 200},

		// Denied: not used by the sandbox.
		{"GET", "/version", 403},
		{"GET", "/v1.47/version", 403},
		{"GET", "/v1.47/info", 403},
		{"GET", "/v1.47/events", 403},
		{"GET", "/v1.47/containers/json?size=1", 403},
		{"GET", "/v1.47/images/json", 403},
		{"POST", "/v1.47/images/create?fromImage=alpine", 403},
		{"POST", "/v1.47/build", 403},
		{"POST", "/v1.47/swarm/init", 403},
		{"POST", "/v1.47/plugins/pull", 403},
		{"POST", "/session", 403},
		{"POST", "/v1.47/networks/superbot_sandbox/connect", 403},
		{"POST", "/v1.47/containers/superbot-bot-abc123/kill", 403},
		{"POST", "/v1.47/containers/superbot-bot-abc123/wait", 403},
		{"POST", "/v1.47/containers/superbot-bot-abc123/attach?stream=1", 403},
		{"GET", "/v1.47/containers/superbot-bot-abc123/logs", 403},
		{"GET", "/v1.47/containers/superbot-bot-abc123/archive?path=/", 403},
		{"PUT", "/v1.47/containers/superbot-bot-abc123/archive?path=/", 403},
		{"POST", "/v1.47/containers/superbot-bot-abc123/rename?name=x", 403},
		{"POST", "/v1.47/containers/superbot-bot-abc123/update", 403},
		{"POST", "/v1.47/exec/" + fakeID(1) + "/resize?h=1&w=1", 403},
		{"OPTIONS", "/_ping", 403},

		// Denied: container scoping.
		{"GET", "/v1.47/containers/superbot-postgres/json", 403},
		{"POST", "/v1.47/containers/superbot-postgres/exec", 403},
		{"POST", "/v1.47/containers/" + foreign + "/start", 403},
		{"POST", "/v1.47/containers/" + foreign[:12] + "/stop", 403},
		{"DELETE", "/v1.47/containers/nonexistent", 403},

		// Denied: query, version and path tricks.
		{"POST", "/v1.47/containers/superbot-bot-abc123/start?checkpoint=x", 403},
		{"DELETE", "/v1.47/containers/superbot-bot-abc123?link=1", 403},
		{"POST", "/v1.47/containers/superbot-bot-abc123/stop?t=1&t=2", 403},
		{"POST", "/v1.20/containers/superbot-bot-abc123/start", 403},
		{"GET", "/v1.47/containers/superbot-bot-abc123/../../images/json", 403},
		{"GET", "/v1.47/containers/superbot-bot-abc123%2Fjson", 403},
		{"GET", "/v1.47//containers/superbot-bot-abc123/json", 403},
		{"GET", "/v1.47/containers/superbot-bot-abc123/json/", 403},
		{"POST", "/v1.47/containers/create?name=evil", 403},
	}
	for _, tc := range cases {
		before := len(h.daemon.recorded())
		status, body := h.do(t, tc.method, tc.path, nil)
		if status != tc.want {
			t.Errorf("%s %s: status %d, want %d (%s)", tc.method, tc.path, status, tc.want, body)
			continue
		}
		if tc.want == http.StatusForbidden {
			var msg struct{ Message string }
			if err := json.Unmarshal(body, &msg); err != nil || msg.Message == "" {
				t.Errorf("%s %s: 403 body must be {\"message\":...}, got %q", tc.method, tc.path, body)
			}
			// The only thing a denied request may cause upstream is the
			// proxy's own (unversioned) inspect to resolve a container.
			for _, r := range h.daemon.recorded()[before:] {
				if r.Method != http.MethodGet || strings.HasPrefix(r.Path, "/v1") || !strings.HasSuffix(r.Path, "/json") {
					t.Errorf("%s %s: denied request reached the daemon as %s %s", tc.method, tc.path, r.Method, r.Path)
				}
			}
		}
	}
}

func TestContainerIDsAreResolvedAndPinned(t *testing.T) {
	h := newHarness(t)
	id := h.daemon.addContainer("superbot-bot-abc123", map[string]string{botIDLabel: "x"})
	for i := 0; i < 3; i++ {
		if status, body := h.do(t, "POST", "/v1.47/containers/"+id[:12]+"/start", nil); status != 204 {
			t.Fatalf("start by short id: %d %s", status, body)
		}
	}
	if n := h.daemon.count("GET", "/containers/"+id[:12]+"/json"); n != 1 {
		t.Fatalf("resolution must be cached; daemon saw %d inspects", n)
	}
	starts := 0
	for _, r := range h.daemon.recorded() {
		if strings.HasSuffix(r.Path, "/start") {
			starts++
			if r.Path != "/v1.47/containers/"+id+"/start" {
				t.Fatalf("a resolved id must be forwarded in full, got %s", r.Path)
			}
		}
	}
	if starts != 3 {
		t.Fatalf("want 3 forwarded starts, got %d", starts)
	}
}

func TestExecIDsAreTracked(t *testing.T) {
	h := newHarness(t)
	h.daemon.addContainer("superbot-bot-abc123", map[string]string{botIDLabel: "x"})
	status, body := h.do(t, "POST", "/v1.47/containers/superbot-bot-abc123/exec", []byte(`{"AttachStdout":true,"Cmd":["true"]}`))
	if status != http.StatusCreated {
		t.Fatalf("exec create: %d %s", status, body)
	}
	var created struct{ Id string }
	if err := json.Unmarshal(body, &created); err != nil || created.Id == "" {
		t.Fatalf("exec create response not passed through intact: %q", body)
	}
	if status, body := h.do(t, "GET", "/v1.47/exec/"+created.Id+"/json", nil); status != 200 {
		t.Fatalf("inspect of a tracked exec: %d %s", status, body)
	}
	if status, body := h.do(t, "POST", "/v1.47/exec/"+created.Id+"/start", []byte(`{"Detach":true}`)); status != 200 {
		t.Fatalf("detached start of a tracked exec: %d %s", status, body)
	}
	other := fakeID(0xdead)
	for _, req := range [][2]string{{"GET", "/v1.47/exec/" + other + "/json"}, {"POST", "/v1.47/exec/" + other + "/start"}} {
		if status, _ := h.do(t, req[0], req[1], nil); status != http.StatusForbidden {
			t.Errorf("%s %s on an exec not created through the proxy: %d, want 403", req[0], req[1], status)
		}
	}
	// Entries expire.
	h.proxy.mu.Lock()
	h.proxy.now = func() time.Time { return time.Now().Add(execTTL + time.Minute) }
	h.proxy.mu.Unlock()
	if status, _ := h.do(t, "GET", "/v1.47/exec/"+created.Id+"/json", nil); status != http.StatusForbidden {
		t.Errorf("expired exec id still allowed: %d", status)
	}
}

func TestExecCreateBodyIsChecked(t *testing.T) {
	h := newHarness(t)
	h.daemon.addContainer("superbot-bot-abc123", map[string]string{botIDLabel: "x"})
	for _, body := range []string{`{"Privileged":true,"Cmd":["sh"]}`, `{"User":"root","Cmd":["sh"]}`} {
		if status, _ := h.do(t, "POST", "/v1.47/containers/superbot-bot-abc123/exec", []byte(body)); status != http.StatusForbidden {
			t.Errorf("exec %s: status %d, want 403", body, status)
		}
	}
	if n := h.daemon.count("POST", "/exec"); n != 0 {
		t.Fatalf("refused exec reached the daemon %d times", n)
	}
}

func TestCreateIsValidatedAndCanonicalized(t *testing.T) {
	h := newHarness(t)
	opts := managerSpec("desktop", "superbot_sandbox")
	good := string(createBody(t, opts))
	withUnknown := strings.Replace(good, `"HostConfig":{`, `"HostConfig":{"FutureField":true,`, 1)
	status, body := h.do(t, "POST", "/v1.47/containers/create?name="+opts.Name, []byte(withUnknown))
	if status != http.StatusCreated {
		t.Fatalf("manager create refused: %d %s", status, body)
	}
	reqs := h.daemon.recorded()
	last := reqs[len(reqs)-1]
	if last.Query != "name="+opts.Name {
		t.Fatalf("query not forwarded canonically: %q", last.Query)
	}
	if strings.Contains(string(last.Body), "FutureField") {
		t.Fatal("unknown fields reached the daemon")
	}
	if last.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content type: %q", last.Header.Get("Content-Type"))
	}

	bad := strings.Replace(good, `"Privileged":false`, `"Privileged":true`, 1)
	if status, _ := h.do(t, "POST", "/v1.47/containers/create?name="+opts.Name, []byte(bad)); status != http.StatusForbidden {
		t.Fatalf("privileged create: %d, want 403", status)
	}
	huge := strings.Replace(good, `"Env":[`, `"Env":["PAD=`+strings.Repeat("x", maxCreateBody)+`",`, 1)
	if status, _ := h.do(t, "POST", "/v1.47/containers/create?name="+opts.Name, []byte(huge)); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized create: %d, want 413", status)
	}
}

func TestDenialsAreLoggedWithoutEnv(t *testing.T) {
	h := newHarness(t)
	opts := managerSpec("none", "superbot_sandbox")
	opts.Config.Env = append(opts.Config.Env, "OPENAI_API_KEY=sk-very-secret")
	opts.HostConfig.Privileged = true
	status, body := h.do(t, "POST", "/v1.47/containers/create?name="+opts.Name, createBody(t, opts))
	if status != http.StatusForbidden || strings.Contains(string(body), "sk-very-secret") {
		t.Fatalf("status %d body %s", status, body)
	}
	logs := h.logs.String()
	if !strings.Contains(logs, "docker proxy denied request") || !strings.Contains(logs, "privileged containers are not allowed") ||
		!strings.Contains(logs, "/v1.47/containers/create") {
		t.Fatalf("denial not logged with method, path and reason:\n%s", logs)
	}
	if strings.Contains(logs, "sk-very-secret") {
		t.Fatal("env values from the create body were logged")
	}
}

func TestBodiesRefusedWhereNoneIsExpected(t *testing.T) {
	h := newHarness(t)
	h.daemon.addContainer("superbot-bot-abc123", map[string]string{botIDLabel: "x"})
	if status, _ := h.do(t, "POST", "/v1.47/containers/superbot-bot-abc123/start", []byte(`{"Privileged":true}`)); status != http.StatusForbidden {
		t.Fatalf("start with a body: %d, want 403", status)
	}
}

func TestUpgradeHeaderOnlyForExecStart(t *testing.T) {
	h := newHarness(t)
	h.daemon.addContainer("superbot-bot-abc123", map[string]string{botIDLabel: "x"})
	req, _ := http.NewRequest("POST", h.server.URL+"/v1.47/containers/superbot-bot-abc123/start", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "tcp")
	res, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	reqs := h.daemon.recorded()
	if up := reqs[len(reqs)-1].Header.Get("Upgrade"); up != "" {
		t.Fatalf("upgrade header forwarded on container start: %q", up)
	}
}

func TestExecUpgradePassthrough(t *testing.T) {
	h := newHarness(t)
	h.daemon.execMode = "echo"
	h.daemon.addContainer("superbot-bot-abc123", map[string]string{botIDLabel: "x"})
	status, body := h.do(t, "POST", "/v1.47/containers/superbot-bot-abc123/exec", []byte(`{"AttachStdin":true,"AttachStdout":true,"Cmd":["cat"]}`))
	if status != http.StatusCreated {
		t.Fatalf("exec create: %d %s", status, body)
	}
	var created struct{ Id string }
	_ = json.Unmarshal(body, &created)

	addr := strings.TrimPrefix(h.server.URL, "http://")
	conn, reader, res := rawUpgrade(t, addr, "/v1.47/exec/"+created.Id+"/start", `{"Detach":false,"Tty":false}`)
	if res.StatusCode != http.StatusSwitchingProtocols || !strings.EqualFold(res.Header.Get("Upgrade"), "tcp") {
		t.Fatalf("want 101 with Upgrade: tcp, got %d %v", res.StatusCode, res.Header)
	}
	for _, msg := range []string{"hello", " docker", " stream"} {
		if _, err := io.WriteString(conn, msg); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, len(msg))
		if _, err := io.ReadFull(reader, buf); err != nil || string(buf) != msg {
			t.Fatalf("echo through the hijacked stream: %q, %v", buf, err)
		}
	}
	// Half-close: the daemon sees EOF on stdin, finishes, and the client
	// then reads EOF.
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if rest, err := io.ReadAll(reader); err != nil || len(rest) != 0 {
		t.Fatalf("after half-close: %q, %v", rest, err)
	}

	// Other upgrade protocols are refused.
	_, _, res = rawUpgradeProto(t, addr, "/v1.47/exec/"+created.Id+"/start", "h2c")
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("upgrade to h2c: %d, want 403", res.StatusCode)
	}
}

func rawUpgradeProto(t *testing.T, addr, path, proto string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	_, _ = io.WriteString(conn, "POST "+path+" HTTP/1.1\r\nHost: docker\r\nConnection: Upgrade\r\nUpgrade: "+proto+"\r\nContent-Length: 0\r\n\r\n")
	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	return conn, reader, res
}

func TestStreamingResponsesAreFlushed(t *testing.T) {
	h := newHarness(t)
	h.daemon.streamGate = make(chan struct{})
	h.daemon.addContainer("superbot-bot-abc123", map[string]string{botIDLabel: "x"})
	_, body := h.do(t, "POST", "/v1.47/containers/superbot-bot-abc123/exec", []byte(`{"AttachStdout":true,"Cmd":["true"]}`))
	var created struct{ Id string }
	_ = json.Unmarshal(body, &created)

	req, _ := http.NewRequest("POST", h.server.URL+"/v1.47/exec/"+created.Id+"/start", strings.NewReader(`{"Detach":false}`))
	res, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	first := make(chan string, 1)
	go func() {
		buf := make([]byte, len("first"))
		_, _ = io.ReadFull(res.Body, buf)
		first <- string(buf)
	}()
	select {
	case got := <-first:
		if got != "first" {
			t.Fatalf("first chunk: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first chunk was buffered by the proxy instead of flushed")
	}
	close(h.daemon.streamGate)
	rest, _ := io.ReadAll(res.Body)
	if string(rest) != "second" {
		t.Fatalf("second chunk: %q", rest)
	}
}

func TestUpstreamUnavailable(t *testing.T) {
	cfg := testConfig(newFakeDaemon(t).socket + ".missing")
	proxy, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(proxy)
	defer srv.Close()
	res, err := http.Get(srv.URL + "/_ping")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var msg struct{ Message string }
	_ = json.NewDecoder(res.Body).Decode(&msg)
	if res.StatusCode != http.StatusBadGateway || msg.Message == "" {
		t.Fatalf("status %d message %q", res.StatusCode, msg.Message)
	}
}

func TestListenAndServeStopsOnCancel(t *testing.T) {
	daemon := newFakeDaemon(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	cfg := testConfig(daemon.socket)
	cfg.Listen = addr
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ListenAndServe(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil))) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err := http.Get("http://" + addr + "/_ping")
		if err == nil {
			res.Body.Close()
			if res.StatusCode != 200 {
				t.Fatalf("ping: %d", res.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("proxy never served: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ListenAndServe returned %v after cancel", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ListenAndServe did not stop on cancel")
	}
}
