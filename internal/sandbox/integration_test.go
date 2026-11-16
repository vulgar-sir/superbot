//go:build integration

package sandbox

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func TestDockerSandboxExec(t *testing.T) {
	if os.Getenv("SUPERBOT_TEST_DOCKER") == "" {
		t.Skip("SUPERBOT_TEST_DOCKER is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer cli.Close()
	image := strings.TrimSpace(os.Getenv("SUPERBOT_SANDBOX_IMAGE"))
	if image == "" {
		image = "superbot/computer:latest"
	}
	name := "superbot-integration-" + uuid.NewString()
	created, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{
			Image: image,
			Cmd:   []string{"sh", "-c", "echo docker-integration-ok"},
		},
		HostConfig: &container.HostConfig{
			NetworkMode: container.NetworkMode("none"),
		},
	})
	if err != nil {
		t.Fatalf("create container: %v", err)
	}
	defer func() {
		_, _ = cli.ContainerRemove(context.Background(), created.ID, client.ContainerRemoveOptions{Force: true})
	}()
	if _, err := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		t.Fatalf("start container: %v", err)
	}
	wait := cli.ContainerWait(ctx, created.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case err := <-wait.Error:
		if err != nil {
			t.Fatalf("wait container: %v", err)
		}
	case status := <-wait.Result:
		if status.StatusCode != 0 {
			t.Fatalf("container exit = %d", status.StatusCode)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for container")
	}
	logs, err := cli.ContainerLogs(ctx, created.ID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		t.Fatalf("container logs: %v", err)
	}
	defer logs.Close()
	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, logs); err != nil {
		t.Fatalf("read logs: %v", err)
	}
	if !strings.Contains(stdout.String(), "docker-integration-ok") {
		t.Fatalf("unexpected output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
