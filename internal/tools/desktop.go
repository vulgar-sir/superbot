package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"superbot/internal/sandbox"
)

type DesktopTools struct {
	manager *sandbox.Manager
	ownerID uuid.UUID
	botID   uuid.UUID
}

func NewDesktopTools(manager *sandbox.Manager, ownerID, botID uuid.UUID) []Tool {
	tools := &DesktopTools{manager: manager, ownerID: ownerID, botID: botID}
	return []Tool{
		&desktopScreenshotTool{DesktopTools: tools},
		&desktopClickTool{DesktopTools: tools},
		&desktopTypeTool{DesktopTools: tools},
		&desktopKeyTool{DesktopTools: tools},
		&desktopScrollTool{DesktopTools: tools},
		&desktopListWindowsTool{DesktopTools: tools},
		&desktopWindowActionTool{DesktopTools: tools},
	}
}

func (d *DesktopTools) exec(ctx context.Context, cmd []string, timeout time.Duration) (sandbox.ExecResult, error) {
	return d.manager.Exec(ctx, d.ownerID, d.botID, "desktop", cmd, "/workspace", timeout)
}

type desktopScreenshotTool struct{ *DesktopTools }

func (t *desktopScreenshotTool) Name() string { return "desktop_screenshot" }
func (t *desktopScreenshotTool) Description() string {
	return "Capture the current XFCE desktop as a PNG in the bot workspace. Use the live computer panel to view it."
}
func (t *desktopScreenshotTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *desktopScreenshotTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	name := uuid.NewString() + ".png"
	rel := filepath.ToSlash(filepath.Join("artifacts", "desktop", name))
	command := fmt.Sprintf("mkdir -p /workspace/artifacts/desktop && scrot -o /workspace/%s", rel)
	result, err := t.exec(ctx, []string{"sh", "-lc", command}, 60*time.Second)
	if err != nil {
		return Result{}, err
	}
	if result.ExitCode != 0 {
		return formatExecResult(result), nil
	}
	hostPath := filepath.Join(t.manager.WorkspacePath(t.botID), "artifacts", "desktop", name)
	if _, statErr := os.Stat(hostPath); statErr != nil {
		return Result{Content: "scrot reported success but screenshot file is missing: " + hostPath, IsError: true}, nil
	}
	return Result{Content: "screenshot saved: " + hostPath}, nil
}

type desktopClickTool struct{ *DesktopTools }

func (t *desktopClickTool) Name() string { return "desktop_click" }
func (t *desktopClickTool) Description() string {
	return "Click at absolute desktop coordinates with the left mouse button. Use desktop_screenshot to locate targets."
}
func (t *desktopClickTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"}},"required":["x","y"]}`)
}
func (t *desktopClickTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.X < 0 || args.Y < 0 || args.X > 10000 || args.Y > 10000 {
		return Result{}, fmt.Errorf("coordinates out of range")
	}
	result, err := t.exec(ctx, []string{"xdotool", "mousemove", "--sync", fmt.Sprint(args.X), fmt.Sprint(args.Y), "click", "1"}, 30*time.Second)
	if err != nil {
		return Result{}, err
	}
	if result.ExitCode != 0 {
		return formatExecResult(result), nil
	}
	return Result{Content: fmt.Sprintf("clicked at %d,%d", args.X, args.Y)}, nil
}

type desktopTypeTool struct{ *DesktopTools }

func (t *desktopTypeTool) Name() string { return "desktop_type" }
func (t *desktopTypeTool) Description() string {
	return "Type text into the currently focused desktop application or terminal. Focus a window first with desktop_click."
}
func (t *desktopTypeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)
}
func (t *desktopTypeTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Text) == "" {
		return Result{}, fmt.Errorf("text is required")
	}
	if len([]rune(args.Text)) > 4000 {
		return Result{}, fmt.Errorf("text is too long")
	}
	result, err := t.exec(ctx, []string{"xdotool", "type", "--clearmodifiers", "--delay", "20", "--", args.Text}, 60*time.Second)
	if err != nil {
		return Result{}, err
	}
	if result.ExitCode != 0 {
		return formatExecResult(result), nil
	}
	return Result{Content: fmt.Sprintf("typed %d characters", len([]rune(args.Text)))}, nil
}

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9+_-]+$`)

type desktopKeyTool struct{ *DesktopTools }

func (t *desktopKeyTool) Name() string { return "desktop_key" }
func (t *desktopKeyTool) Description() string {
	return "Press a key or key combination on the desktop, e.g. Return, Tab, Escape, ctrl+c, alt+F4."
}
func (t *desktopKeyTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}`)
}
func (t *desktopKeyTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if !keyPattern.MatchString(args.Key) {
		return Result{}, fmt.Errorf("invalid key specification")
	}
	result, err := t.exec(ctx, []string{"xdotool", "key", "--clearmodifiers", args.Key}, 30*time.Second)
	if err != nil {
		return Result{}, err
	}
	if result.ExitCode != 0 {
		return formatExecResult(result), nil
	}
	return Result{Content: "pressed " + args.Key}, nil
}

type desktopScrollTool struct{ *DesktopTools }

func (t *desktopScrollTool) Name() string { return "desktop_scroll" }
func (t *desktopScrollTool) Description() string {
	return "Scroll the mouse wheel at a desktop position. Direction up/down, clicks 1-10."
}
func (t *desktopScrollTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"},"direction":{"type":"string","enum":["up","down"]},"clicks":{"type":"integer"}},"required":["x","y","direction"]}`)
}
func (t *desktopScrollTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		X         int    `json:"x"`
		Y         int    `json:"y"`
		Direction string `json:"direction"`
		Clicks    int    `json:"clicks"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.X < 0 || args.Y < 0 || args.X > 10000 || args.Y > 10000 {
		return Result{}, fmt.Errorf("coordinates out of range")
	}
	if args.Direction != "up" && args.Direction != "down" {
		return Result{}, fmt.Errorf("direction must be up or down")
	}
	if args.Clicks <= 0 {
		args.Clicks = 3
	}
	if args.Clicks > 20 {
		args.Clicks = 20
	}
	button := "5"
	if args.Direction == "up" {
		button = "4"
	}
	command := []string{"xdotool", "mousemove", "--sync", fmt.Sprint(args.X), fmt.Sprint(args.Y)}
	for i := 0; i < args.Clicks; i++ {
		command = append(command, "click", button)
	}
	result, err := t.exec(ctx, command, 30*time.Second)
	if err != nil {
		return Result{}, err
	}
	if result.ExitCode != 0 {
		return formatExecResult(result), nil
	}
	return Result{Content: fmt.Sprintf("scrolled %s %d at %d,%d", args.Direction, args.Clicks, args.X, args.Y)}, nil
}

type desktopListWindowsTool struct{ *DesktopTools }

func (t *desktopListWindowsTool) Name() string { return "desktop_list_windows" }
func (t *desktopListWindowsTool) Description() string {
	return "List visible desktop windows with their titles."
}
func (t *desktopListWindowsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *desktopListWindowsTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	result, err := t.exec(ctx, []string{"sh", "-c", "xdotool search --onlyvisible --name '.*' getwindowname %@ 2>/dev/null | sort -u"}, 30*time.Second)
	if err != nil {
		return Result{}, err
	}
	if result.ExitCode != 0 {
		return formatExecResult(result), nil
	}
	return Result{Content: result.Stdout}, nil
}

type desktopWindowActionTool struct{ *DesktopTools }

func (t *desktopWindowActionTool) Name() string { return "desktop_window_action" }
func (t *desktopWindowActionTool) Description() string {
	return "Act on a window by title substring: focus, minimize, maximize or close."
}
func (t *desktopWindowActionTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"action":{"type":"string","enum":["focus","minimize","maximize","close"]}},"required":["title","action"]}`)
}
func (t *desktopWindowActionTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Title  string `json:"title"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	title := strings.TrimSpace(args.Title)
	if title == "" {
		return Result{}, fmt.Errorf("title is required")
	}
	var command []string
	switch args.Action {
	case "focus":
		command = []string{"xdotool", "search", "--onlyvisible", "--name", title, "windowactivate", "%@"}
	case "minimize":
		command = []string{"xdotool", "search", "--onlyvisible", "--name", title, "windowminimize", "%@"}
	case "close":
		command = []string{"xdotool", "search", "--onlyvisible", "--name", title, "windowclose", "%@"}
	case "maximize":
		if result, err := t.exec(ctx, []string{"xdotool", "search", "--onlyvisible", "--name", title, "windowsize", "%@", "100%", "100%"}, 30*time.Second); err != nil {
			return Result{}, err
		} else if result.ExitCode != 0 {
			return formatExecResult(result), nil
		}
		command = []string{"xdotool", "search", "--onlyvisible", "--name", title, "windowmove", "%@", "0", "0"}
	default:
		return Result{}, fmt.Errorf("action must be focus, minimize, maximize or close")
	}
	result, err := t.exec(ctx, command, 30*time.Second)
	if err != nil {
		return Result{}, err
	}
	if result.ExitCode != 0 {
		return formatExecResult(result), nil
	}
	return Result{Content: args.Action + " ok: " + title}, nil
}
