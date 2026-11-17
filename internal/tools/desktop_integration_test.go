//go:build integration

package tools

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"superbot/internal/config"
	"superbot/internal/sandbox"
)

// TestIntegrationDesktopTools drives the real desktop tools against a desktop
// computer (SUPERBOT_DESKTOP_IMAGE) the way a text-only model would: start a
// program, find things on screen by their text, click and type there, save,
// and check the result on disk.
func TestIntegrationDesktopTools(t *testing.T) {
	if os.Getenv("SUPERBOT_TEST_DOCKER") == "" {
		t.Skip("SUPERBOT_TEST_DOCKER is not set")
	}
	image := strings.TrimSpace(os.Getenv("SUPERBOT_DESKTOP_IMAGE"))
	if image == "" {
		image = "superbot/computer-desktop:latest"
	}
	ctx := context.Background()
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	opts := sandbox.ContainerSpec{Image: image, DesktopImage: image, BotID: uuid.New(), Profile: "desktop", GateToken: "t"}.CreateOptions()
	opts.Name = "sbtest-desktop-" + uuid.NewString()[:8]
	opts.HostConfig.Binds = nil
	opts.HostConfig.PortBindings = nil
	opts.HostConfig.NetworkMode = container.NetworkMode("none")
	opts.HostConfig.RestartPolicy = container.RestartPolicy{}
	opts.Config.ExposedPorts = nil
	created, err := cli.ContainerCreate(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.ContainerRemove(context.Background(), created.ID, client.ContainerRemoveOptions{Force: true})
	if _, err := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		t.Fatal(err)
	}
	manager, err := sandbox.New(config.Config{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	desktop := &DesktopTools{manager: manager, container: opts.Name}
	registry := NewRegistry()
	for _, tool := range desktop.tools() {
		registry.Register(tool)
	}
	call := func(name string, args any) Result {
		t.Helper()
		raw, _ := json.Marshal(args)
		started := time.Now()
		result := registry.Execute(ctx, name, raw)
		t.Logf("%s %s (%s)\n%s", name, raw, time.Since(started).Round(time.Millisecond), result.Content)
		if result.IsError {
			t.Fatalf("%s failed: %s", name, result.Content)
		}
		return result
	}
	// The desktop session needs a few seconds after the container starts;
	// Chromium's window is the last to appear.
	deadline := time.Now().Add(60 * time.Second)
	for {
		capture, err := desktop.screen(ctx, screenOptions{Windows: true})
		if err == nil && strings.Contains(formatWindows(capture), "Chromium") {
			time.Sleep(2 * time.Second)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("desktop never came up: %v", err)
		}
		time.Sleep(2 * time.Second)
	}

	launched := call("desktop_launch", map[string]any{"command": "mousepad /workspace/notes.txt", "wait_seconds": 4})
	if !strings.Contains(launched.Content, "running") || !strings.Contains(launched.Content, "notes.txt") {
		t.Fatalf("mousepad did not open a window")
	}
	call("desktop_window_action", map[string]any{"title": "notes.txt", "action": "maximize"})
	time.Sleep(time.Second)
	screen := call("desktop_screenshot", map[string]any{})
	if !strings.Contains(screen.Content, "* \"") || !strings.Contains(screen.Content, "File") {
		t.Fatalf("screenshot text lacks the active window or its menu")
	}
	found := call("desktop_read_screen", map[string]any{"find": "File"})
	point := regexp.MustCompile(`\((\d+),(\d+)\) File`).FindStringSubmatch(found.Content)
	if point == nil {
		t.Fatalf("find did not return a click point")
	}
	x, _ := strconv.Atoi(point[1])
	y, _ := strconv.Atoi(point[2])
	// Open the File menu by its text and close it again, then type into the
	// document below the menu bar.
	call("desktop_click", map[string]any{"x": x, "y": y})
	menu := call("desktop_read_screen", map[string]any{"find": "Save"})
	if !strings.Contains(menu.Content, "Found") {
		t.Fatalf("the File menu did not open (no Save item)")
	}
	call("desktop_key", map[string]any{"key": "Escape"})
	call("desktop_click", map[string]any{"x": 700, "y": 450})
	call("desktop_type", map[string]any{"text": "hello desktop", "enter": true})
	call("desktop_type", map[string]any{"text": "你好，桌面", "paste": true})
	call("desktop_key", map[string]any{"key": "ctrl+s"})
	time.Sleep(2 * time.Second)
	result, err := manager.ExecIn(ctx, opts.Name, []string{"cat", "/workspace/notes.txt"}, "/workspace", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Stdout; !strings.Contains(got, "hello desktop\n") || !strings.Contains(got, "你好，桌面") {
		t.Fatalf("saved file = %q", got)
	}
	read := call("desktop_read_screen", map[string]any{"region": map[string]int{"x": 0, "y": 30, "width": 900, "height": 300}})
	if !strings.Contains(strings.ReplaceAll(read.Content, " ", ""), "hellodesktop") {
		t.Errorf("region OCR did not read the typed text")
	}
	call("desktop_drag", map[string]any{"from_x": 60, "from_y": y + 60, "to_x": 300, "to_y": y + 60})
	call("desktop_click", map[string]any{"x": 400, "y": 400, "button": "right"})
	call("desktop_key", map[string]any{"key": "Escape"})
	call("desktop_scroll", map[string]any{"x": 700, "y": 450, "direction": "down", "clicks": 2})
	call("desktop_move", map[string]any{"x": 700, "y": 450})
	call("desktop_move", map[string]any{"x": 700, "y": 450}) // same spot: must not hang
	windows := call("desktop_list_windows", map[string]any{})
	if !strings.Contains(windows.Content, "notes.txt") {
		t.Fatalf("window list lacks mousepad")
	}
}
