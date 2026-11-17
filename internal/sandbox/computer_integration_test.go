//go:build integration

package sandbox

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// These tests run the real computer image (SUPERBOT_SANDBOX_IMAGE) shaped
// exactly as the worker creates it, under throwaway names
// (sbtest-computer-*), on SUPERBOT_TEST_COMPUTER_NETWORK (default: the
// default bridge). The test process must be able to reach containers on
// that network; deploy/run-integration-tests.sh runs it on the same one.

const testGateToken = "integration-gate-token-0123456789"

func startComputer(t *testing.T) (*Manager, string, string) {
	t.Helper()
	if os.Getenv("SUPERBOT_TEST_DOCKER") == "" {
		t.Skip("SUPERBOT_TEST_DOCKER is not set")
	}
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() { cli.Close() })
	image := strings.TrimSpace(os.Getenv("SUPERBOT_SANDBOX_IMAGE"))
	if image == "" {
		image = "superbot/computer:latest"
	}
	opts := ContainerSpec{Image: image, BotID: uuid.New(), Profile: "browser", GateToken: testGateToken}.CreateOptions()
	opts.Name = "sbtest-computer-" + uuid.NewString()[:8]
	// No bind mounts (the test may itself run in a container) and no
	// published ports: the test talks to the container over the bridge.
	opts.HostConfig.Binds = nil
	opts.HostConfig.PortBindings = nil
	network := strings.TrimSpace(os.Getenv("SUPERBOT_TEST_COMPUTER_NETWORK"))
	if network == "" {
		network = "bridge"
	}
	opts.HostConfig.NetworkMode = container.NetworkMode(network)
	opts.HostConfig.RestartPolicy = container.RestartPolicy{}
	opts.Config.ExposedPorts = nil
	ctx := context.Background()
	created, err := cli.ContainerCreate(ctx, opts)
	if err != nil {
		t.Fatalf("create computer: %v", err)
	}
	t.Cleanup(func() {
		_, _ = cli.ContainerRemove(context.Background(), created.ID, client.ContainerRemoveOptions{Force: true})
	})
	if _, err := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		t.Fatalf("start computer: %v", err)
	}
	info, err := cli.ContainerInspect(ctx, created.ID, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("inspect computer: %v", err)
	}
	ip := ""
	if settings := info.Container.NetworkSettings; settings != nil {
		if endpoint := settings.Networks[network]; endpoint != nil && endpoint.IPAddress.IsValid() {
			ip = endpoint.IPAddress.String()
		}
	}
	if ip == "" {
		t.Fatal("computer has no bridge address")
	}
	m := &Manager{cli: cli, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := m.waitForCDP(ctx, ip+":9223", testGateToken, 60*time.Second); err != nil {
		t.Fatalf("computer did not come up behind its gate: %v", err)
	}
	return m, opts.Name, ip
}

func gateStatus(t *testing.T, rawURL, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, rawURL, nil)
	if token != "" {
		req.Header.Set(GateHeader, token)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(body)
}

func TestIntegrationComputerGate(t *testing.T) {
	_, _, ip := startComputer(t)
	for _, endpoint := range []string{"http://" + ip + ":9223/json/version", "http://" + ip + ":6080/vnc.html"} {
		if code, _ := gateStatus(t, endpoint, ""); code != http.StatusForbidden {
			t.Errorf("%s without the secret: %d, want 403", endpoint, code)
		}
		if code, _ := gateStatus(t, endpoint, "wrong"); code != http.StatusForbidden {
			t.Errorf("%s with a wrong secret: %d, want 403", endpoint, code)
		}
		if code, _ := gateStatus(t, endpoint, testGateToken); code != http.StatusOK {
			t.Errorf("%s with the secret: %d, want 200", endpoint, code)
		}
	}
	// Chromium, VNC and websockify themselves are not on the network.
	for _, port := range []string{"9222", "5900", "6081"} {
		if conn, err := net.DialTimeout("tcp", ip+":"+port, 3*time.Second); err == nil {
			conn.Close()
			t.Errorf("port %s is reachable from outside the computer", port)
		}
	}

	// A full DevTools session through the gate, as the browser tools open it.
	_, body := gateStatus(t, "http://"+ip+":9223/json/version", testGateToken)
	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal([]byte(body), &version); err != nil {
		t.Fatalf("version reply: %v", err)
	}
	wsURL, _ := url.Parse(version.WebSocketDebuggerURL)
	wsURL.Host = ip + ":9223"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, _, err := websocket.Dial(ctx, wsURL.String(), nil); err == nil {
		t.Error("a DevTools WebSocket without the secret was accepted")
	}
	conn, _, err := websocket.Dial(ctx, wsURL.String(), &websocket.DialOptions{HTTPHeader: Info{GateToken: testGateToken}.GateHeaders()})
	if err != nil {
		t.Fatalf("dial DevTools through the gate: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"id":1,"method":"Browser.getVersion"}`)); err != nil {
		t.Fatal(err)
	}
	_, reply, err := conn.Read(ctx)
	if err != nil || !strings.Contains(string(reply), `"product"`) {
		t.Fatalf("Browser.getVersion = %s, %v", reply, err)
	}
}

func TestIntegrationComputerEnvironment(t *testing.T) {
	m, name, _ := startComputer(t)
	result, err := m.execInContainer(context.Background(), name, []string{"sh", "-lc",
		`echo "home=$HOME"; for c in userapt appimage-install node npm tesseract proot xdotool wmctrl; do command -v $c >/dev/null || echo "missing $c"; done
		 tesseract --list-langs 2>/dev/null | grep -qx chi_sim || echo "missing chi_sim"
		 pip install --user --dry-run --no-deps --no-index six 2>&1 | grep -q externally-managed && echo "pip refuses"
		 touch "$HOME/persist-check" && echo wrote-home`}, "/workspace", 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	out := result.Stdout + result.Stderr
	if !strings.Contains(out, "home=/workspace/.home") || !strings.Contains(out, "wrote-home") {
		t.Fatalf("home not set up: %s", out)
	}
	if strings.Contains(out, "missing") || strings.Contains(out, "pip refuses") {
		t.Fatalf("image is missing tools: %s", out)
	}
}

func TestIntegrationComputerJobs(t *testing.T) {
	m, name, _ := startComputer(t)
	ctx := context.Background()
	reply, err := m.jobsCallIn(ctx, name, startJobArgs(`for i in 1 2 3; do echo "tick $i"; sleep 1; done; echo 中文完成; exit 3`, "ticks", time.Minute)...)
	if err != nil {
		t.Fatal(err)
	}
	job, err := startedJob(reply)
	if err != nil || job.State != "running" {
		t.Fatalf("start = %+v, %v", job, err)
	}
	if n, err := m.runningJobsIn(ctx, name); err != nil || n != 1 {
		t.Fatalf("running jobs = %d, %v", n, err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		args, _ := statusArgs(job.ID, 10)
		reply, err := m.jobsCallIn(ctx, name, args...)
		if err != nil {
			t.Fatal(err)
		}
		jobs, err := listedJobs(reply)
		if err != nil {
			t.Fatal(err)
		}
		if jobs[0].State == "exited" {
			if jobs[0].ExitCode == nil || *jobs[0].ExitCode != 3 || !strings.Contains(jobs[0].LogTail, "tick 3") || !strings.Contains(jobs[0].LogTail, "中文完成") {
				t.Fatalf("finished job = %+v", jobs[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never finished: %+v", jobs[0])
		}
		time.Sleep(time.Second)
	}
	if n, _ := m.runningJobsIn(ctx, name); n != 0 {
		t.Fatalf("running jobs after the end = %d", n)
	}

	// A long job survives an unrelated exec that is cancelled (whose
	// cleanup kills orphans started after it), and stops on request.
	reply, _ = m.jobsCallIn(ctx, name, startJobArgs(`trap 'echo stopping; exit 0' TERM; while :; do sleep 1; done`, "server", time.Hour)...)
	long, err := startedJob(reply)
	if err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	_, _ = m.execInContainer(cancelCtx, name, []string{"sleep", "60"}, "/workspace", 60*time.Second)
	cancel()
	if n, _ := m.runningJobsIn(ctx, name); n != 1 {
		t.Fatalf("the job did not survive a cancelled exec: %d running", n)
	}
	reply, err = m.jobsCallIn(ctx, name, "stop", long.ID)
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := stoppedJob(reply)
	if err != nil || stopped.State != "stopped" || !strings.Contains(stopped.LogTail, "stopping") {
		t.Fatalf("stopped = %+v, %v", stopped, err)
	}
	// Nothing is left behind: no job processes and no zombies.
	result, _ := m.execInContainer(ctx, name, []string{"sh", "-c", "ps -eo stat,args | grep -E '^Z|sleep 1' | grep -v grep || true"}, "/workspace", 10*time.Second)
	if strings.TrimSpace(result.Stdout) != "" {
		t.Fatalf("left behind:\n%s", result.Stdout)
	}
}

func TestIntegrationUserapt(t *testing.T) {
	if os.Getenv("SUPERBOT_TEST_NETWORK") == "" {
		t.Skip("SUPERBOT_TEST_NETWORK is not set (needs the Debian mirror)")
	}
	m, name, _ := startComputer(t)
	result, err := m.execInContainer(context.Background(), name, []string{"sh", "-lc",
		"userapt install tree >/tmp/ua.log 2>&1 || cat /tmp/ua.log; tree --version; userapt list"}, "/workspace", 280*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Stdout, "tree v") || !strings.Contains(result.Stdout, "tree ") {
		t.Fatalf("userapt install tree: %s %s", result.Stdout, result.Stderr)
	}
}
