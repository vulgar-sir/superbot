package dockerproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// TestSandboxCallSequenceThroughProxy drives the real moby client (as the
// worker uses it with DOCKER_HOST=tcp://docker-proxy:2375) through every
// Docker call internal/sandbox makes, for each profile, and checks that the
// daemon receives exactly the create request the manager built.
func TestSandboxCallSequenceThroughProxy(t *testing.T) {
	h := newHarness(t)
	cli, err := client.New(client.WithHost("tcp://" + strings.TrimPrefix(h.server.URL, "http://")))
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Manager.Ping.
	if _, err := cli.Ping(ctx, client.PingOptions{}); err != nil {
		t.Fatalf("ping: %v", err)
	}
	for _, tc := range []struct{ profile, network string }{
		{"none", "superbot_sandbox"}, {"browser", "superbot_sandbox"}, {"desktop", "superbot_sandbox"},
		{"browser", ""}, // local development: bridge + loopback ports
	} {
		name := tc.profile + "/" + tc.network
		h.daemon.mu.Lock()
		h.daemon.containers = nil
		h.daemon.mu.Unlock()
		opts := managerSpec(tc.profile, tc.network)

		// Ensure: inspect by name (missing), create, start by id, inspect.
		if _, err := cli.ContainerInspect(ctx, opts.Name, client.ContainerInspectOptions{}); err == nil {
			t.Fatalf("%s: inspect of a missing container succeeded", name)
		}
		created, err := cli.ContainerCreate(ctx, opts)
		if err != nil {
			t.Fatalf("%s: create: %v", name, err)
		}
		reqs := h.daemon.recorded()
		var got container.CreateRequest
		if err := json.Unmarshal(reqs[len(reqs)-1].Body, &got); err != nil {
			t.Fatalf("%s: daemon got invalid create body: %v", name, err)
		}
		want := createBody(t, opts)
		if !bytes.Equal(createBody(t, client.ContainerCreateOptions{Config: got.Config, HostConfig: got.HostConfig}), want) {
			t.Fatalf("%s: daemon got a different create request than the manager built", name)
		}
		if _, err := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
			t.Fatalf("%s: start by id: %v", name, err)
		}
		if _, err := cli.ContainerInspect(ctx, opts.Name, client.ContainerInspectOptions{}); err != nil {
			t.Fatalf("%s: inspect by name: %v", name, err)
		}

		// execInContainer: create, attach (hijacked stream), inspect.
		exec, err := cli.ExecCreate(ctx, opts.Name, client.ExecCreateOptions{
			AttachStdout: true, AttachStderr: true,
			Cmd:        []string{"setsid", "-w", "sh", "-c", "script", "marker", "5s", "echo", "hi"},
			WorkingDir: "/workspace",
			Env:        []string{"HOME=/tmp", "SUPERBOT_EXEC_MARK=marker"},
		})
		if err != nil {
			t.Fatalf("%s: exec create: %v", name, err)
		}
		attach, err := cli.ExecAttach(ctx, exec.ID, client.ExecAttachOptions{})
		if err != nil {
			t.Fatalf("%s: exec attach: %v", name, err)
		}
		var stdout, stderr bytes.Buffer
		if _, err := stdcopy.StdCopy(&stdout, &stderr, attach.Reader); err != nil {
			t.Fatalf("%s: read exec output: %v", name, err)
		}
		attach.Close()
		if stdout.String() != "hi\n" {
			t.Fatalf("%s: exec stdout %q", name, stdout.String())
		}
		if info, err := cli.ExecInspect(ctx, exec.ID, client.ExecInspectOptions{}); err != nil || info.Running {
			t.Fatalf("%s: exec inspect: %+v %v", name, info, err)
		}

		// killExec: kill exec (no attach), detached start, inspect, and the
		// restart fallback.
		kill, err := cli.ExecCreate(ctx, opts.Name, client.ExecCreateOptions{Cmd: []string{"sh", "-c", "kill", "sh", "SUPERBOT_EXEC_MARK=marker"}})
		if err != nil {
			t.Fatalf("%s: kill exec create: %v", name, err)
		}
		if _, err := cli.ExecStart(ctx, kill.ID, client.ExecStartOptions{Detach: true}); err != nil {
			t.Fatalf("%s: kill exec start: %v", name, err)
		}
		if _, err := cli.ExecInspect(ctx, kill.ID, client.ExecInspectOptions{}); err != nil {
			t.Fatalf("%s: kill exec inspect: %v", name, err)
		}
		stopAfter := 2
		if _, err := cli.ContainerRestart(ctx, opts.Name, client.ContainerRestartOptions{Signal: "SIGKILL", Timeout: &stopAfter}); err != nil {
			t.Fatalf("%s: restart: %v", name, err)
		}

		// Stop / recreate: stop and force-remove by id.
		timeout := 5
		if _, err := cli.ContainerStop(ctx, created.ID, client.ContainerStopOptions{Timeout: &timeout}); err != nil {
			t.Fatalf("%s: stop: %v", name, err)
		}
		if _, err := cli.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true}); err != nil {
			t.Fatalf("%s: remove: %v", name, err)
		}
	}
	if logs := h.logs.String(); strings.Contains(logs, "denied") {
		t.Fatalf("the sandbox call sequence was partly denied:\n%s", logs)
	}

	// A refusal reaches the client as a readable error.
	bad := managerSpec("none", "superbot_sandbox")
	bad.HostConfig.Privileged = true
	if _, err := cli.ContainerCreate(ctx, bad); err == nil || !strings.Contains(err.Error(), "privileged containers are not allowed") {
		t.Fatalf("privileged create through the client: %v", err)
	}
	if _, err := cli.ImageList(ctx, client.ImageListOptions{}); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("image list through the client: %v", err)
	}
}
