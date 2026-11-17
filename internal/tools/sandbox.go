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
	network := "It has network access."
	if t.profile != "browser" && t.profile != "desktop" {
		network = "It has no network access (enable a computer with computer_enable for that)."
	}
	return "Run a shell command (sh -lc) in the bot's own Linux computer, without root; the working directory is /workspace " +
		"(the bot workspace) and HOME is /workspace/.home, both persistent. " + network + " " +
		"A command runs at most timeout_seconds (default 120, max 300) and is then killed. For anything longer or that keeps " +
		"running (downloads, builds, servers) set background=true: it starts a background job and returns its id at once; " +
		"check it with shell_jobs. Install software without root: `userapt install PKG` (Debian packages), " +
		"`appimage-install FILE_OR_URL`, `pip install PKG` (goes to the home), `npm install -g PKG`. " +
		"Start GUI programs with desktop_launch, not here."
}
func (t *ShellTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},` +
		`"timeout_seconds":{"type":"integer","description":"foreground limit: default 120, max 300"},` +
		`"background":{"type":"boolean","description":"run as a background job and return at once"},` +
		`"name":{"type":"string","description":"short label for a background job"},` +
		`"max_minutes":{"type":"integer","description":"background job time limit in minutes: default 120, max 720"}},"required":["command"]}`)
}

type shellArgs struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Background     bool   `json:"background"`
	Name           string `json:"name"`
	MaxMinutes     int    `json:"max_minutes"`
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
	if args.Background {
		return t.startJob(ctx, args)
	}
	timeout := clampTimeout(args.TimeoutSeconds)
	result, err := t.sandbox.Exec(ctx, t.ownerID, t.botID, t.profile, []string{"sh", "-lc", args.Command}, "/workspace", timeout)
	if err != nil {
		return Result{}, err
	}
	formatted := formatExecResult(result)
	if result.ExitCode == 124 || result.ExitCode == -1 {
		formatted.Content += fmt.Sprintf("\n[the command was stopped after %d seconds; for long-running work use background=true]", int(timeout/time.Second))
	}
	return formatted, nil
}

func (t *ShellTool) startJob(ctx context.Context, args shellArgs) (Result, error) {
	name := strings.TrimSpace(args.Name)
	if len([]rune(name)) > 60 {
		name = string([]rune(name)[:60])
	}
	limit := time.Duration(args.MaxMinutes) * time.Minute
	job, err := t.sandbox.StartJob(ctx, t.ownerID, t.botID, t.profile, args.Command, name, limit)
	if err != nil {
		return Result{Content: "could not start the background job: " + err.Error(), IsError: true}, nil
	}
	if job.State != "running" {
		failed := job.ExitCode != nil && *job.ExitCode != 0
		return Result{Content: "Background job " + job.ID + " ended right away.\n" + formatJob(job), IsError: failed}, nil
	}
	return Result{Content: fmt.Sprintf("Started background job %s (time limit %d minutes). It keeps running after this call; "+
		"check progress and output with shell_jobs {\"job_id\": %q}; stop it with shell_jobs {\"action\": \"stop\", \"job_id\": %q}. "+
		"Full output: %s/%s/log", job.ID, job.Timeout/60, job.ID, job.ID, sandbox.JobsDir, job.ID)}, nil
}

// ShellJobsTool shows and stops background jobs started with
// shell background=true.
type ShellJobsTool struct {
	sandbox *sandbox.Manager
	ownerID uuid.UUID
	botID   uuid.UUID
	profile string
}

func NewShellJobsTool(manager *sandbox.Manager, ownerID, botID uuid.UUID, profile string) *ShellJobsTool {
	return &ShellJobsTool{sandbox: manager, ownerID: ownerID, botID: botID, profile: profile}
}

func (t *ShellJobsTool) Name() string { return "shell_jobs" }
func (t *ShellJobsTool) Description() string {
	return "Background jobs started with shell background=true. Without job_id: list recent jobs. With job_id: its state, " +
		"exit code and the last lines of its output (tail_lines, default 40). action=stop stops it. " +
		"Jobs end when the computer restarts (state lost)."
}
func (t *ShellJobsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["status","stop"]},` +
		`"job_id":{"type":"string"},"tail_lines":{"type":"integer","description":"1-400, default 40"}}}`)
}

func (t *ShellJobsTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Action    string `json:"action"`
		JobID     string `json:"job_id"`
		TailLines int    `json:"tail_lines"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return Result{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	args.JobID = strings.TrimSpace(args.JobID)
	switch args.Action {
	case "stop":
		if args.JobID == "" {
			return Result{}, fmt.Errorf("job_id is required to stop a job")
		}
		job, err := t.sandbox.StopJob(ctx, t.ownerID, t.botID, t.profile, args.JobID)
		if err != nil {
			return Result{Content: err.Error() + ": " + args.JobID, IsError: true}, nil
		}
		return Result{Content: formatJob(job)}, nil
	case "", "status":
	default:
		return Result{}, fmt.Errorf("action must be status or stop")
	}
	lines := args.TailLines
	if lines <= 0 {
		lines = 40
	}
	lines = min(lines, 400)
	jobs, err := t.sandbox.Jobs(ctx, t.ownerID, t.botID, t.profile, args.JobID, lines)
	if err != nil {
		return Result{Content: err.Error() + ": " + args.JobID, IsError: true}, nil
	}
	if len(jobs) == 0 {
		return Result{Content: "No background jobs yet. Start one with shell {\"command\": \"...\", \"background\": true}."}, nil
	}
	if args.JobID != "" {
		return Result{Content: formatJob(jobs[0])}, nil
	}
	lines2 := make([]string, 0, len(jobs))
	for _, job := range jobs {
		lines2 = append(lines2, formatJobLine(job))
	}
	return Result{Content: strings.Join(lines2, "\n")}, nil
}

func jobTime(seconds float64) string {
	if seconds <= 0 {
		return "-"
	}
	return time.Unix(int64(seconds), 0).In(time.FixedZone("CST", 8*3600)).Format("01-02 15:04:05")
}

func formatJobLine(job sandbox.Job) string {
	state := job.State
	if job.ExitCode != nil && state != "running" {
		state += fmt.Sprintf(" (exit %d)", *job.ExitCode)
	}
	label := job.Name
	if label == "" {
		label = job.Command
		if len([]rune(label)) > 80 {
			label = string([]rune(label)[:80]) + "…"
		}
	}
	return fmt.Sprintf("%s  %s  started %s  %s", job.ID, state, jobTime(job.StartedAt), label)
}

func formatJob(job sandbox.Job) string {
	var b strings.Builder
	b.WriteString(formatJobLine(job))
	fmt.Fprintf(&b, "\ncommand: %s", job.Command)
	if job.EndedAt > 0 {
		fmt.Fprintf(&b, "\nended: %s", jobTime(job.EndedAt))
	}
	switch job.State {
	case "timed_out":
		fmt.Fprintf(&b, "\nstopped at its time limit of %d minutes", job.Timeout/60)
	case "lost":
		b.WriteString("\nthe computer was restarted or put to sleep while the job ran, so the job ended; start it again if needed")
	}
	if strings.TrimSpace(job.LogTail) != "" {
		b.WriteString("\n--- output (last lines):\n")
		b.WriteString(job.LogTail)
	} else {
		b.WriteString("\n(no output yet)")
	}
	return b.String()
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
