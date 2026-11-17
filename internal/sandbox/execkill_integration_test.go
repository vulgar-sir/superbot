//go:build integration

package sandbox

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// startKillTarget starts a container shaped like a bot's shell sandbox.
func startKillTarget(t *testing.T) (*Manager, string) {
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
	name := "sbtest-killtest-" + uuid.NewString()[:8]
	ctx := context.Background()
	created, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{
			Image: image, User: "1000:1000", Cmd: []string{"sleep", "infinity"}, WorkingDir: "/tmp",
		},
		HostConfig: &container.HostConfig{
			NetworkMode: container.NetworkMode("none"),
			CapDrop:     []string{"ALL"},
			SecurityOpt: []string{"no-new-privileges"},
			Tmpfs:       map[string]string{"/tmp": "rw,size=64m,mode=1777"},
			Resources:   container.Resources{PidsLimit: int64Ptr(64)},
		},
	})
	if err != nil {
		t.Fatalf("create container: %v", err)
	}
	t.Cleanup(func() {
		_, _ = cli.ContainerRemove(context.Background(), created.ID, client.ContainerRemoveOptions{Force: true})
	})
	if _, err := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		t.Fatalf("start container: %v", err)
	}
	return &Manager{cli: cli, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, name
}

func countSleepers(t *testing.T, m *Manager, name, pattern string) int {
	t.Helper()
	result, err := m.execInContainer(context.Background(), name,
		[]string{"sh", "-c", "ps -eo args | grep -c '^" + pattern + "' || true"}, "/tmp", 10*time.Second)
	if err != nil {
		t.Fatalf("count processes: %v", err)
	}
	var n int
	for _, r := range strings.TrimSpace(result.Stdout) {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
		}
	}
	return n
}

func TestIntegrationExecCancelKillsWholeTree(t *testing.T) {
	m, name := startKillTarget(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(1500 * time.Millisecond)
		cancel()
	}()
	// A foreground command, a background job, a double-forked orphan, and a
	// process that starts its own session with an empty environment.
	script := "sleep 301 & (sleep 302 &) ; env -i setsid sleep 303 & sleep 304"
	started := time.Now()
	if _, err := m.execInContainer(ctx, name, []string{"sh", "-c", script}, "/tmp", 120*time.Second); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if waited := time.Since(started); waited > 20*time.Second {
		t.Fatalf("cancelled exec took %s to return", waited)
	}
	time.Sleep(500 * time.Millisecond)
	if left := countSleepers(t, m, name, "sleep 30"); left != 0 {
		t.Fatalf("%d processes of the cancelled exec are still running", left)
	}
}

func TestIntegrationExecCompletedKeepsBackgroundJobs(t *testing.T) {
	m, name := startKillTarget(t)
	// A command that finishes normally may leave a daemon behind on purpose.
	result, err := m.execInContainer(context.Background(), name,
		[]string{"sh", "-c", "nohup sleep 401 >/dev/null 2>&1 & echo started"}, "/tmp", 30*time.Second)
	if err != nil || !strings.Contains(result.Stdout, "started") || result.ExitCode != 0 {
		t.Fatalf("exec: %+v %v", result, err)
	}
	if left := countSleepers(t, m, name, "sleep 401"); left != 1 {
		t.Fatalf("a completed command's daemon must keep running, found %d", left)
	}
	exit, err := m.execInContainer(context.Background(), name, []string{"sh", "-c", "exit 9"}, "/tmp", 30*time.Second)
	if err != nil || exit.ExitCode != 9 {
		t.Fatalf("exit status must pass through the wrapper: %+v %v", exit, err)
	}
}

func TestIntegrationExecCancelAtProcessLimitRestarts(t *testing.T) {
	m, name := startKillTarget(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(2500 * time.Millisecond)
		cancel()
	}()
	// Fill the container's process limit so not even the kill can start.
	script := "for i in $(seq 1 100); do sleep 501 & done; wait"
	if _, err := m.execInContainer(ctx, name, []string{"sh", "-c", script}, "/tmp", 120*time.Second); err != nil {
		t.Fatalf("exec: %v", err)
	}
	deadline := time.Now().Add(40 * time.Second)
	for {
		left := -1
		func() {
			defer func() { _ = recover() }()
			left = countSleepers(t, m, name, "sleep 501")
		}()
		if left == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("processes still running after the fallback restart: %d", left)
		}
		time.Sleep(time.Second)
	}
}
