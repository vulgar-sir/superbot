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

	"superbot/internal/llm"
	"superbot/internal/sandbox"
)

// DesktopTools operate the XFCE desktop of a bot's desktop computer. The
// model may be text-only, so every look at the screen comes back as text:
// the windows and the words on screen with the pixel coordinates to click
// (tesseract OCR). Screenshots additionally carry the image, which the
// engine forwards to models that accept images.
type DesktopTools struct {
	manager *sandbox.Manager
	ownerID uuid.UUID
	botID   uuid.UUID
	// container, when set, names a running container to use instead of the
	// bot's computer (integration tests).
	container string
}

func NewDesktopTools(manager *sandbox.Manager, ownerID, botID uuid.UUID) []Tool {
	return (&DesktopTools{manager: manager, ownerID: ownerID, botID: botID}).tools()
}

func (tools *DesktopTools) tools() []Tool {
	return []Tool{
		&desktopScreenshotTool{DesktopTools: tools},
		&desktopReadScreenTool{DesktopTools: tools},
		&desktopClickTool{DesktopTools: tools},
		&desktopDragTool{DesktopTools: tools},
		&desktopMoveTool{DesktopTools: tools},
		&desktopTypeTool{DesktopTools: tools},
		&desktopKeyTool{DesktopTools: tools},
		&desktopScrollTool{DesktopTools: tools},
		&desktopListWindowsTool{DesktopTools: tools},
		&desktopWindowActionTool{DesktopTools: tools},
		&desktopLaunchTool{DesktopTools: tools},
	}
}

func (d *DesktopTools) exec(ctx context.Context, cmd []string, timeout time.Duration) (sandbox.ExecResult, error) {
	if d.container != "" {
		return d.manager.ExecIn(ctx, d.container, cmd, "/workspace", timeout)
	}
	return d.manager.Exec(ctx, d.ownerID, d.botID, "desktop", cmd, "/workspace", timeout)
}

// run executes an xdotool (or similar) command and turns a failure into a
// tool error the model can read.
func (d *DesktopTools) run(ctx context.Context, cmd []string, ok string) (Result, error) {
	result, err := d.exec(ctx, cmd, 30*time.Second)
	if err != nil {
		return Result{}, err
	}
	if result.ExitCode != 0 {
		return formatExecResult(result), nil
	}
	return Result{Content: ok}, nil
}

// screen runs desktop_screen.py in the computer.
func (d *DesktopTools) screen(ctx context.Context, opts screenOptions) (screenCapture, error) {
	encoded, _ := json.Marshal(opts)
	result, err := d.exec(ctx, []string{"python3", "-c", desktopScreenScript, string(encoded)}, 120*time.Second)
	if err != nil {
		return screenCapture{}, err
	}
	if result.ExitCode != 0 {
		return screenCapture{}, fmt.Errorf("reading the screen failed: %s", strings.TrimSpace(result.Stderr+" "+result.Stdout))
	}
	capture, err := parseScreenCapture(result.Stdout)
	if err != nil {
		return screenCapture{}, err
	}
	if opts.Capture && capture.Width == 0 {
		return screenCapture{}, fmt.Errorf("could not capture the screen: %s", strings.Join(capture.Errors, "; "))
	}
	return capture, nil
}

func checkPoint(x, y int) error {
	if x < 0 || y < 0 || x > 10000 || y > 10000 {
		return fmt.Errorf("coordinates out of range")
	}
	return nil
}

type desktopScreenshotTool struct{ *DesktopTools }

func (t *desktopScreenshotTool) Name() string { return "desktop_screenshot" }
func (t *desktopScreenshotTool) Description() string {
	return "Look at the desktop. Returns the open windows (active one marked *) and all text on screen with the (x,y) pixel " +
		"coordinates to click it (OCR), so you can operate the desktop even without seeing images; models that accept images " +
		"also get the screenshot. Set save=true to keep the PNG in /workspace/artifacts/desktop/ (e.g. to send it to the user with send_file)."
}
func (t *desktopScreenshotTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"save":{"type":"boolean","description":"keep the screenshot as a PNG file in the workspace"}}}`)
}
func (t *desktopScreenshotTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Save bool `json:"save"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return Result{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	id := uuid.NewString()
	jpegRel := filepath.ToSlash(filepath.Join(".superbot", "screens", id+".jpg"))
	opts := screenOptions{Capture: true, OCR: true, Windows: true, Scale: 1.5, JPEG: "/workspace/" + jpegRel}
	savedRel := ""
	if args.Save {
		savedRel = filepath.ToSlash(filepath.Join("artifacts", "desktop", time.Now().Format("20060102-150405")+"-"+id[:8]+".png"))
		opts.Save = "/workspace/" + savedRel
	}
	capture, err := t.screen(ctx, opts)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	content := formatScreen(capture, "", true)
	if savedRel != "" {
		content += "\n\nScreenshot saved: /workspace/" + savedRel
	}
	result := Result{Content: content}
	jpegHost := filepath.Join(t.manager.WorkspacePath(t.botID), filepath.FromSlash(jpegRel))
	if data, err := os.ReadFile(jpegHost); err == nil {
		result.Images = []llm.Image{{MIMEType: "image/jpeg", Data: data}}
	}
	_ = os.Remove(jpegHost)
	return result, nil
}

type desktopReadScreenTool struct{ *DesktopTools }

func (t *desktopReadScreenTool) Name() string { return "desktop_read_screen" }
func (t *desktopReadScreenTool) Description() string {
	return "Read the text on the desktop with click coordinates (OCR), cheaper than desktop_screenshot. " +
		"find: return only where this text appears (e.g. a button label) with the exact point to click. " +
		"region: read only part of the screen (more accurate for small text)."
}
func (t *desktopReadScreenTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"find":{"type":"string","description":"text to locate, e.g. 保存 or OK"},` +
		`"region":{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"},"width":{"type":"integer"},"height":{"type":"integer"}},"required":["x","y","width","height"]}}}`)
}
func (t *desktopReadScreenTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Find   string `json:"find"`
		Region *struct {
			X      int `json:"x"`
			Y      int `json:"y"`
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"region"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return Result{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	if len([]rune(args.Find)) > 200 {
		return Result{}, fmt.Errorf("find is too long")
	}
	opts := screenOptions{Capture: true, OCR: true, Windows: args.Region == nil, Scale: 1.5}
	if args.Region != nil {
		r := args.Region
		if err := checkPoint(r.X, r.Y); err != nil {
			return Result{}, err
		}
		if r.Width <= 0 || r.Height <= 0 || r.Width > 10000 || r.Height > 10000 {
			return Result{}, fmt.Errorf("region width and height must be positive")
		}
		opts.Region = []int{r.X, r.Y, r.Width, r.Height}
		// Small regions are enlarged more; OCR reads small UI fonts better.
		if r.Width*r.Height < 400*300 {
			opts.Scale = 2.5
		} else {
			opts.Scale = 2
		}
	}
	capture, err := t.screen(ctx, opts)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	return Result{Content: formatScreen(capture, args.Find, args.Region == nil && args.Find == "")}, nil
}

var mouseButtons = map[string]string{"": "1", "left": "1", "middle": "2", "right": "3"}

var modifierKeys = map[string]string{"ctrl": "ctrl", "control": "ctrl", "shift": "shift", "alt": "alt", "super": "super", "meta": "super"}

func modifierArgs(modifiers []string) ([]string, []string, error) {
	var down, up []string
	seen := map[string]bool{}
	for _, raw := range modifiers {
		key, ok := modifierKeys[strings.ToLower(strings.TrimSpace(raw))]
		if !ok {
			return nil, nil, fmt.Errorf("modifier must be ctrl, shift, alt or super")
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		down = append(down, "keydown", key)
		up = append([]string{"keyup", key}, up...)
	}
	return down, up, nil
}

type desktopClickTool struct{ *DesktopTools }

func (t *desktopClickTool) Name() string { return "desktop_click" }
func (t *desktopClickTool) Description() string {
	return "Click at desktop coordinates (take them from desktop_screenshot / desktop_read_screen). " +
		"button: left (default), right (context menu) or middle; clicks: 2 for a double-click (open files, select words); " +
		"modifiers: keys held while clicking, e.g. [\"ctrl\"]. " +
		"then: more points to click afterwards, in order, with the same button (e.g. a row of calculator keys or a menu path) — " +
		"one call instead of one per click; read the screen again afterwards to check the result."
}
func (t *desktopClickTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"},` +
		`"button":{"type":"string","enum":["left","right","middle"]},"clicks":{"type":"integer","description":"1-3, default 1"},` +
		`"modifiers":{"type":"array","items":{"type":"string","enum":["ctrl","shift","alt","super"]}},` +
		`"then":{"type":"array","maxItems":20,"items":{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"}},"required":["x","y"]}}},` +
		`"required":["x","y"]}`)
}
func (t *desktopClickTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		X         int          `json:"x"`
		Y         int          `json:"y"`
		Button    string       `json:"button"`
		Clicks    int          `json:"clicks"`
		Modifiers []string     `json:"modifiers"`
		Then      []clickPoint `json:"then"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	points := append([]clickPoint{{X: args.X, Y: args.Y}}, args.Then...)
	command, summary, err := clickSequence(points, args.Button, args.Clicks, args.Modifiers)
	if err != nil {
		return Result{}, err
	}
	return t.run(ctx, command, summary)
}

type clickPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// clickSequence clicks the points in order in one xdotool invocation; the
// pause between them lets the application react to each click.
func clickSequence(points []clickPoint, button string, clicks int, modifiers []string) ([]string, string, error) {
	if len(points) == 0 || len(points) > maxClickSequence+1 {
		return nil, "", fmt.Errorf("then takes at most %d points", maxClickSequence)
	}
	var command []string
	var summaries []string
	for i, point := range points {
		next, summary, err := clickCommand(point.X, point.Y, button, clicks, modifiers)
		if err != nil {
			return nil, "", err
		}
		if i == 0 {
			command = next
		} else {
			command = append(append(command, "sleep", "0.15"), next[1:]...)
		}
		summaries = append(summaries, summary)
	}
	return command, strings.Join(summaries, "; "), nil
}

// maxClickSequence bounds the extra points of one desktop_click call.
const maxClickSequence = 20

// clickCommand builds the xdotool command for a click.
func clickCommand(x, y int, button string, clicks int, modifiers []string) ([]string, string, error) {
	if err := checkPoint(x, y); err != nil {
		return nil, "", err
	}
	code, ok := mouseButtons[strings.ToLower(button)]
	if !ok {
		return nil, "", fmt.Errorf("button must be left, right or middle")
	}
	if clicks <= 0 {
		clicks = 1
	}
	if clicks > 3 {
		return nil, "", fmt.Errorf("clicks must be 1, 2 or 3")
	}
	down, up, err := modifierArgs(modifiers)
	if err != nil {
		return nil, "", err
	}
	command := []string{"xdotool", "mousemove", fmt.Sprint(x), fmt.Sprint(y), "sleep", "0.05"}
	command = append(command, down...)
	command = append(command, "click", "--repeat", fmt.Sprint(clicks), "--delay", "80", code)
	command = append(command, up...)
	name := map[string]string{"1": "left", "2": "middle", "3": "right"}[code]
	what := name + " click"
	switch clicks {
	case 2:
		what = name + " double-click"
	case 3:
		what = name + " triple-click"
	}
	if len(modifiers) > 0 {
		what = strings.Join(modifiers, "+") + "+" + what
	}
	return command, fmt.Sprintf("%s at %d,%d", what, x, y), nil
}

type desktopDragTool struct{ *DesktopTools }

func (t *desktopDragTool) Name() string { return "desktop_drag" }
func (t *desktopDragTool) Description() string {
	return "Drag with the mouse button held from (from_x,from_y) to (to_x,to_y): move files, select text, resize, drag sliders."
}
func (t *desktopDragTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"from_x":{"type":"integer"},"from_y":{"type":"integer"},` +
		`"to_x":{"type":"integer"},"to_y":{"type":"integer"},"button":{"type":"string","enum":["left","right","middle"]}},` +
		`"required":["from_x","from_y","to_x","to_y"]}`)
}
func (t *desktopDragTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		FromX  int    `json:"from_x"`
		FromY  int    `json:"from_y"`
		ToX    int    `json:"to_x"`
		ToY    int    `json:"to_y"`
		Button string `json:"button"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	command, err := dragCommand(args.FromX, args.FromY, args.ToX, args.ToY, args.Button)
	if err != nil {
		return Result{}, err
	}
	return t.run(ctx, command, fmt.Sprintf("dragged from %d,%d to %d,%d", args.FromX, args.FromY, args.ToX, args.ToY))
}

// dragCommand presses, moves in steps (applications ignore a jump straight
// to the target) and releases.
func dragCommand(fromX, fromY, toX, toY int, button string) ([]string, error) {
	if err := checkPoint(fromX, fromY); err != nil {
		return nil, err
	}
	if err := checkPoint(toX, toY); err != nil {
		return nil, err
	}
	code, ok := mouseButtons[strings.ToLower(button)]
	if !ok {
		return nil, fmt.Errorf("button must be left, right or middle")
	}
	command := []string{"xdotool", "mousemove", fmt.Sprint(fromX), fmt.Sprint(fromY), "sleep", "0.05", "mousedown", code, "sleep", "0.15"}
	const steps = 5
	for i := 1; i <= steps; i++ {
		x := fromX + (toX-fromX)*i/steps
		y := fromY + (toY-fromY)*i/steps
		command = append(command, "mousemove", fmt.Sprint(x), fmt.Sprint(y), "sleep", "0.05")
	}
	command = append(command, "sleep", "0.1", "mouseup", code)
	return command, nil
}

type desktopMoveTool struct{ *DesktopTools }

func (t *desktopMoveTool) Name() string { return "desktop_move" }
func (t *desktopMoveTool) Description() string {
	return "Move the mouse to (x,y) without clicking, e.g. to hover over a menu or show a tooltip."
}
func (t *desktopMoveTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"}},"required":["x","y"]}`)
}
func (t *desktopMoveTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if err := checkPoint(args.X, args.Y); err != nil {
		return Result{}, err
	}
	return t.run(ctx, []string{"xdotool", "mousemove", fmt.Sprint(args.X), fmt.Sprint(args.Y)},
		fmt.Sprintf("mouse moved to %d,%d", args.X, args.Y))
}

// pasteScript puts $1 on the clipboard and pastes it into the focused
// window; terminals paste with ctrl+shift+v.
const pasteScript = `printf %s "$1" | xclip -selection clipboard -i >/dev/null 2>&1 || exit 1
class=$(xprop -id "$(xdotool getactivewindow)" WM_CLASS 2>/dev/null | tr 'A-Z' 'a-z')
case "$class" in *terminal*|*xterm*|*konsole*) key=ctrl+shift+v ;; *) key=ctrl+v ;; esac
sleep 0.1
xdotool key --clearmodifiers "$key"`

type desktopTypeTool struct{ *DesktopTools }

func (t *desktopTypeTool) Name() string { return "desktop_type" }
func (t *desktopTypeTool) Description() string {
	return "Type text (Chinese works) into the focused window; click the input field first. " +
		"Long text is pasted through the clipboard. enter=true presses Enter afterwards."
}
func (t *desktopTypeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"enter":{"type":"boolean"},` +
		`"paste":{"type":"boolean","description":"paste via the clipboard instead of typing key by key"}},"required":["text"]}`)
}
func (t *desktopTypeTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Text  string `json:"text"`
		Enter bool   `json:"enter"`
		Paste bool   `json:"paste"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Text == "" {
		return Result{}, fmt.Errorf("text is required")
	}
	runes := len([]rune(args.Text))
	if runes > 20000 {
		return Result{}, fmt.Errorf("text is too long (max 20000 characters)")
	}
	var command []string
	method := "typed"
	if args.Paste || runes > 300 {
		method = "pasted"
		command = []string{"sh", "-c", pasteScript, "sh", args.Text}
	} else {
		command = []string{"xdotool", "type", "--clearmodifiers", "--delay", "15", "--", args.Text}
	}
	result, err := t.exec(ctx, command, 120*time.Second)
	if err != nil {
		return Result{}, err
	}
	if result.ExitCode != 0 {
		return formatExecResult(result), nil
	}
	if args.Enter {
		if result, err := t.exec(ctx, []string{"xdotool", "key", "--clearmodifiers", "Return"}, 30*time.Second); err != nil {
			return Result{}, err
		} else if result.ExitCode != 0 {
			return formatExecResult(result), nil
		}
		return Result{Content: fmt.Sprintf("%s %d characters and pressed Enter", method, runes)}, nil
	}
	return Result{Content: fmt.Sprintf("%s %d characters", method, runes)}, nil
}

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9+_-]+$`)

type desktopKeyTool struct{ *DesktopTools }

func (t *desktopKeyTool) Name() string { return "desktop_key" }
func (t *desktopKeyTool) Description() string {
	return "Press a key or key combination on the desktop, e.g. Return, Tab, Escape, BackSpace, Delete, Page_Down, F5, " +
		"ctrl+c, ctrl+shift+t, alt+F4, super. Several keys in order: separate with spaces (\"ctrl+a Delete\"). repeat presses it again."
}
func (t *desktopKeyTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"},"repeat":{"type":"integer","description":"1-50, default 1"}},"required":["key"]}`)
}
func (t *desktopKeyTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Key    string `json:"key"`
		Repeat int    `json:"repeat"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	command, err := keyCommand(args.Key, args.Repeat)
	if err != nil {
		return Result{}, err
	}
	return t.run(ctx, command, "pressed "+strings.Join(strings.Fields(args.Key), " "))
}

func keyCommand(spec string, repeat int) ([]string, error) {
	keys := strings.Fields(spec)
	if len(keys) == 0 || len(keys) > 20 {
		return nil, fmt.Errorf("give 1 to 20 keys")
	}
	for _, key := range keys {
		if !keyPattern.MatchString(key) {
			return nil, fmt.Errorf("invalid key specification %q", key)
		}
	}
	if repeat <= 0 {
		repeat = 1
	}
	if repeat > 50 {
		return nil, fmt.Errorf("repeat must be at most 50")
	}
	command := []string{"xdotool", "key", "--clearmodifiers", "--delay", "40"}
	// Repeat by listing the keys again: Debian's xdotool has no --repeat
	// for key.
	for i := 0; i < repeat; i++ {
		command = append(command, keys...)
	}
	return command, nil
}

type desktopScrollTool struct{ *DesktopTools }

func (t *desktopScrollTool) Name() string { return "desktop_scroll" }
func (t *desktopScrollTool) Description() string {
	return "Scroll the mouse wheel at a desktop position. Direction up/down/left/right, clicks 1-20 (default 3)."
}
func (t *desktopScrollTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"},"direction":{"type":"string","enum":["up","down","left","right"]},"clicks":{"type":"integer"}},"required":["x","y","direction"]}`)
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
	if err := checkPoint(args.X, args.Y); err != nil {
		return Result{}, err
	}
	button, ok := map[string]string{"up": "4", "down": "5", "left": "6", "right": "7"}[args.Direction]
	if !ok {
		return Result{}, fmt.Errorf("direction must be up, down, left or right")
	}
	if args.Clicks <= 0 {
		args.Clicks = 3
	}
	if args.Clicks > 20 {
		args.Clicks = 20
	}
	command := []string{"xdotool", "mousemove", fmt.Sprint(args.X), fmt.Sprint(args.Y), "sleep", "0.05",
		"click", "--repeat", fmt.Sprint(args.Clicks), "--delay", "30", button}
	return t.run(ctx, command, fmt.Sprintf("scrolled %s %d at %d,%d", args.Direction, args.Clicks, args.X, args.Y))
}

type desktopListWindowsTool struct{ *DesktopTools }

func (t *desktopListWindowsTool) Name() string { return "desktop_list_windows" }
func (t *desktopListWindowsTool) Description() string {
	return "List the open desktop windows with title, position and size; the active window is marked *."
}
func (t *desktopListWindowsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *desktopListWindowsTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	capture, err := t.screen(ctx, screenOptions{Windows: true})
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	return Result{Content: "Windows (* = active):\n" + formatWindows(capture)}, nil
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
	// xdotool --name takes a regular expression; the model gives a plain
	// title fragment.
	pattern := regexp.QuoteMeta(title)
	var command []string
	switch args.Action {
	case "focus":
		command = []string{"xdotool", "search", "--onlyvisible", "--name", pattern, "windowactivate", "%@"}
	case "minimize":
		command = []string{"xdotool", "search", "--onlyvisible", "--name", pattern, "windowminimize", "%@"}
	case "close":
		command = []string{"xdotool", "search", "--onlyvisible", "--name", pattern, "windowclose", "%@"}
	case "maximize":
		command = []string{"sh", "-c", `wmctrl -r "$1" -b add,maximized_vert,maximized_horz && wmctrl -a "$1"`, "sh", title}
	default:
		return Result{}, fmt.Errorf("action must be focus, minimize, maximize or close")
	}
	return t.run(ctx, command, args.Action+" ok: "+title)
}

// launchScript starts a program detached from the exec (its own session,
// output to a log), so the tool returns while the program keeps running,
// and reports whether it is still running after a moment. The exec marker
// is dropped so a later cancelled exec can never take the program down.
const launchScript = `mkdir -p /tmp/.sblaunch
log=/tmp/.sblaunch/$(date +%s%N).log
[ -z "${DBUS_SESSION_BUS_ADDRESS:-}" ] && [ -f /tmp/dbus.env ] && . /tmp/dbus.env && export DBUS_SESSION_BUS_ADDRESS
env -u SUPERBOT_EXEC_MARK setsid sh -lc "$1" >"$log" 2>&1 </dev/null &
pid=$!
sleep "$2"
if kill -0 "$pid" 2>/dev/null; then echo "running (pid $pid)"; else wait "$pid"; echo "exited with code $?"; fi
echo "--- output:"
tail -c 1500 "$log"`

type desktopLaunchTool struct{ *DesktopTools }

func (t *desktopLaunchTool) Name() string { return "desktop_launch" }
func (t *desktopLaunchTool) Description() string {
	return "Start a GUI program on the desktop and leave it running (e.g. \"mousepad notes.txt\", \"xfce4-terminal\", \"thunar /workspace\", " +
		"\"chromium https://example.com\", or a program installed with userapt/appimage-install). Do not start GUI programs with shell: " +
		"shell waits for the program to exit. Returns whether it started and the windows now open."
}
func (t *desktopLaunchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"wait_seconds":{"type":"integer","description":"how long to wait for the window, 1-20, default 3"}},"required":["command"]}`)
}
func (t *desktopLaunchTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Command     string `json:"command"`
		WaitSeconds int    `json:"wait_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	args.Command = strings.TrimSpace(args.Command)
	if args.Command == "" {
		return Result{}, fmt.Errorf("command is required")
	}
	if len(args.Command) > 4096 {
		return Result{}, fmt.Errorf("command is too long")
	}
	wait := args.WaitSeconds
	if wait <= 0 {
		wait = 3
	}
	wait = min(wait, 20)
	before, _ := t.screen(ctx, screenOptions{Windows: true})
	result, err := t.exec(ctx, []string{"sh", "-c", launchScript, "sh", args.Command, fmt.Sprint(wait)}, time.Duration(wait+30)*time.Second)
	if err != nil {
		return Result{}, err
	}
	content := strings.TrimSpace(result.Stdout)
	isError := result.ExitCode != 0 || strings.HasPrefix(content, "exited with code ") && !strings.HasPrefix(content, "exited with code 0")
	after, err := t.screen(ctx, screenOptions{Windows: true})
	if err == nil {
		// Bring the program's window to the front: another window that
		// appeared meanwhile (or Chromium starting up) may have the focus.
		if opened := newWindows(before, after); len(opened) > 0 {
			if activated, err := t.exec(ctx, []string{"xdotool", "windowactivate", opened[len(opened)-1].ID}, 15*time.Second); err == nil && activated.ExitCode == 0 {
				after.Active = opened[len(opened)-1].ID
			}
		}
		content += "\n\nWindows (* = active):\n" + formatWindows(after)
	}
	return Result{Content: "Started: " + args.Command + "\n" + content, IsError: isError}, nil
}

// newWindows lists the normal windows in after that were not in before.
func newWindows(before, after screenCapture) []screenWindow {
	seen := map[string]bool{}
	for _, w := range before.Windows {
		seen[strings.ToLower(w.ID)] = true
	}
	var opened []screenWindow
	for _, w := range after.Windows {
		if w.Desktop >= 0 && !seen[strings.ToLower(w.ID)] {
			opened = append(opened, w)
		}
	}
	return opened
}
