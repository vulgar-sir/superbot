package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"superbot/internal/sandbox"
)

type ShellTool struct {
	sandbox *sandbox.Manager
	ownerID uuid.UUID
	botID   uuid.UUID
	profile string
}

func NewShellTool(manager *sandbox.Manager, ownerID, botID uuid.UUID, profile string) *ShellTool {
	return &ShellTool{sandbox: manager, ownerID: ownerID, botID: botID, profile: profile}
}

func (t *ShellTool) Name() string { return "shell" }
func (t *ShellTool) Description() string {
	return "Run a shell command inside the bot's isolated Linux sandbox. The working directory is /workspace, which maps to the bot workspace. Network is disabled by default."
}
func (t *ShellTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"timeout_seconds":{"type":"integer","description":"default 120, max 300"}},"required":["command"]}`)
}

type shellArgs struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (t *ShellTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args shellArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	args.Command = strings.TrimSpace(args.Command)
	if args.Command == "" {
		return Result{}, fmt.Errorf("command is required")
	}
	if len(args.Command) > 16*1024 {
		return Result{}, fmt.Errorf("command is too long (max 16KB)")
	}
	timeout := clampTimeout(args.TimeoutSeconds)
	result, err := t.sandbox.Exec(ctx, t.ownerID, t.botID, t.profile, []string{"sh", "-lc", args.Command}, "/workspace", timeout)
	if err != nil {
		return Result{}, err
	}
	return formatExecResult(result), nil
}

type PythonTool struct {
	sandbox *sandbox.Manager
	ownerID uuid.UUID
	botID   uuid.UUID
	profile string
}

func NewPythonTool(manager *sandbox.Manager, ownerID, botID uuid.UUID, profile string) *PythonTool {
	return &PythonTool{sandbox: manager, ownerID: ownerID, botID: botID, profile: profile}
}

func (t *PythonTool) Name() string { return "python" }
func (t *PythonTool) Description() string {
	return "Execute Python 3 code inside the bot's Linux sandbox. Use this for calculations, data processing and quick scripts. The working directory is /workspace."
}
func (t *PythonTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"code":{"type":"string"},"timeout_seconds":{"type":"integer","description":"default 120, max 300"}},"required":["code"]}`)
}

type pythonArgs struct {
	Code           string `json:"code"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (t *PythonTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args pythonArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Code) == "" {
		return Result{}, fmt.Errorf("code is required")
	}
	if len(args.Code) > 256*1024 {
		return Result{}, fmt.Errorf("code is too large (max 256KB)")
	}
	workspace := t.sandbox.WorkspacePath(t.botID)
	tmpDir := filepath.Join(workspace, ".superbot", "tmp")
	if err := os.MkdirAll(tmpDir, 0o777); err != nil {
		return Result{}, fmt.Errorf("create python temp dir: %w", err)
	}
	name := uuid.NewString() + ".py"
	filePath := filepath.Join(tmpDir, name)
	if err := os.WriteFile(filePath, []byte(args.Code), 0o666); err != nil {
		return Result{}, fmt.Errorf("write python file: %w", err)
	}
	_ = os.Chmod(filePath, 0o666)
	rel := filepath.ToSlash(filepath.Join(".superbot", "tmp", name))

	timeout := clampTimeout(args.TimeoutSeconds)
	result, err := t.sandbox.Exec(ctx, t.ownerID, t.botID, t.profile, []string{"python3", rel}, "/workspace", timeout)
	if err != nil {
		return Result{}, err
	}
	return formatExecResult(result), nil
}

func clampTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return 120 * time.Second
	}
	if seconds > 300 {
		return 300 * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func formatExecResult(result sandbox.ExecResult) Result {
	var b strings.Builder
	if strings.TrimSpace(result.Stdout) != "" {
		b.WriteString(result.Stdout)
	}
	if strings.TrimSpace(result.Stderr) != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[stderr]\n")
		b.WriteString(result.Stderr)
	}
	if result.ExitCode != 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[exit code: %d]", result.ExitCode)
	}
	if b.Len() == 0 {
		b.WriteString("(no output)")
	}
	return Result{Content: b.String(), IsError: result.ExitCode != 0}
}
