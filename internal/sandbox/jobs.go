package sandbox

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/moby/moby/client"
)

// Background jobs let a bot start work that outlives one tool call — a long
// download, a build, a server it is developing — and check on it later. A
// computer with a running job is not put to sleep for being idle, nor
// evicted to make room for another; every job has a time limit, so a
// forgotten one cannot keep the computer awake for ever.

//go:embed jobs.py
var jobsScript string

const (
	// DefaultJobTimeout and MaxJobTimeout bound how long a job may run.
	DefaultJobTimeout = 2 * time.Hour
	MaxJobTimeout     = 12 * time.Hour
	// MaxRunningJobs is how many jobs one computer runs at once.
	MaxRunningJobs = 4
	jobCallTimeout = 30 * time.Second
)

// Job is a background job as jobs.py reports it.
type Job struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Command   string  `json:"command"`
	State     string  `json:"state"` // running, exited, stopped, timed_out, lost
	StartedAt float64 `json:"started_at"`
	EndedAt   float64 `json:"ended_at"`
	Timeout   int     `json:"timeout"`
	ExitCode  *int    `json:"exit_code"`
	LogTail   string  `json:"log_tail"`
}

// ErrTooManyJobs is returned when the computer already runs MaxRunningJobs.
type ErrTooManyJobs struct{ Running []string }

func (e ErrTooManyJobs) Error() string {
	return fmt.Sprintf("this computer already runs %d background jobs (%s); stop one first", len(e.Running), strings.Join(e.Running, ", "))
}

// ErrJobNotFound is returned for an unknown job id.
var ErrJobNotFound = fmt.Errorf("no such background job")

var jobIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,39}$`)

// ValidJobID reports whether id can name a job.
func ValidJobID(id string) bool { return jobIDPattern.MatchString(id) }

func newJobID() string {
	return "job-" + time.Now().Format("0102-150405") + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:4]
}

type jobsReply struct {
	Error   string   `json:"error"`
	Running []string `json:"running"`
	Job     *Job     `json:"job"`
	Jobs    []Job    `json:"jobs"`
}

func parseJobsReply(stdout string) (jobsReply, error) {
	var reply jobsReply
	trimmed := strings.TrimSpace(stdout)
	if i := strings.LastIndex(trimmed, "\n{"); i >= 0 {
		trimmed = trimmed[i+1:]
	}
	if err := json.Unmarshal([]byte(trimmed), &reply); err != nil {
		return jobsReply{}, fmt.Errorf("unreadable job data: %w", err)
	}
	return reply, nil
}

// jobsCall runs jobs.py in botID's computer, starting it if needed.
func (m *Manager) jobsCall(ctx context.Context, ownerID, botID uuid.UUID, profile string, args ...string) (jobsReply, error) {
	info, err := m.Ensure(ctx, ownerID, botID, profile)
	if err != nil {
		return jobsReply{}, err
	}
	return m.jobsCallIn(ctx, info.Name, args...)
}

// jobsCallIn runs jobs.py in the named, running container.
func (m *Manager) jobsCallIn(ctx context.Context, name string, args ...string) (jobsReply, error) {
	cmd := append([]string{"python3", "-c", jobsScript}, args...)
	result, err := m.execInContainer(ctx, name, cmd, "/workspace", jobCallTimeout)
	if err != nil {
		return jobsReply{}, err
	}
	if result.ExitCode != 0 {
		return jobsReply{}, fmt.Errorf("job control failed (exit %d): %s", result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return parseJobsReply(result.Stdout)
}

func startJobArgs(command, name string, timeout time.Duration) []string {
	if timeout <= 0 {
		timeout = DefaultJobTimeout
	}
	timeout = min(timeout, MaxJobTimeout)
	return []string{"start", newJobID(), fmt.Sprint(int(timeout / time.Second)), fmt.Sprint(MaxRunningJobs), name, command}
}

func startedJob(reply jobsReply) (Job, error) {
	if reply.Error == "too_many" {
		return Job{}, ErrTooManyJobs{Running: reply.Running}
	}
	if reply.Error != "" || reply.Job == nil {
		return Job{}, fmt.Errorf("could not start the job: %s", reply.Error)
	}
	return *reply.Job, nil
}

// StartJob starts command as a background job and returns at once.
func (m *Manager) StartJob(ctx context.Context, ownerID, botID uuid.UUID, profile, command, name string, timeout time.Duration) (Job, error) {
	reply, err := m.jobsCall(ctx, ownerID, botID, profile, startJobArgs(command, name, timeout)...)
	if err != nil {
		return Job{}, err
	}
	return startedJob(reply)
}

func statusArgs(id string, tailLines int) ([]string, error) {
	if id == "" {
		return []string{"status"}, nil
	}
	if !ValidJobID(id) {
		return nil, ErrJobNotFound
	}
	return []string{"status", id, fmt.Sprint(tailLines)}, nil
}

func listedJobs(reply jobsReply) ([]Job, error) {
	if reply.Error == "not_found" {
		return nil, ErrJobNotFound
	}
	if reply.Error != "" {
		return nil, fmt.Errorf("job control failed: %s", reply.Error)
	}
	return reply.Jobs, nil
}

// Jobs returns one job (id set) with its last tailLines of output, or the
// most recent jobs.
func (m *Manager) Jobs(ctx context.Context, ownerID, botID uuid.UUID, profile, id string, tailLines int) ([]Job, error) {
	args, err := statusArgs(id, tailLines)
	if err != nil {
		return nil, err
	}
	reply, err := m.jobsCall(ctx, ownerID, botID, profile, args...)
	if err != nil {
		return nil, err
	}
	return listedJobs(reply)
}

func stoppedJob(reply jobsReply) (Job, error) {
	if reply.Error == "not_found" || reply.Job == nil {
		return Job{}, ErrJobNotFound
	}
	return *reply.Job, nil
}

// StopJob stops a job: SIGTERM to its process group, SIGKILL after a grace
// period.
func (m *Manager) StopJob(ctx context.Context, ownerID, botID uuid.UUID, profile, id string) (Job, error) {
	if !ValidJobID(id) {
		return Job{}, ErrJobNotFound
	}
	reply, err := m.jobsCall(ctx, ownerID, botID, profile, "stop", id)
	if err != nil {
		return Job{}, err
	}
	return stoppedJob(reply)
}

// runningJobs counts the jobs running in botID's computer without starting
// it: a stopped or missing computer has none.
func (m *Manager) runningJobs(ctx context.Context, botID uuid.UUID) (int, error) {
	return m.runningJobsIn(ctx, containerName(botID))
}

func (m *Manager) runningJobsIn(ctx context.Context, name string) (int, error) {
	info, err := m.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if cerrdefs.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if info.Container.State == nil || !info.Container.State.Running {
		return 0, nil
	}
	result, err := m.execInContainer(ctx, name, []string{"python3", "-c", jobsScript, "live"}, "/workspace", 20*time.Second)
	if err != nil {
		return 0, err
	}
	return parseRunningCount(result.Stdout)
}

func parseRunningCount(stdout string) (int, error) {
	var reply struct {
		Running *int `json:"running"`
	}
	trimmed := strings.TrimSpace(stdout)
	if i := strings.LastIndex(trimmed, "\n{"); i >= 0 {
		trimmed = trimmed[i+1:]
	}
	if err := json.Unmarshal([]byte(trimmed), &reply); err != nil || reply.Running == nil {
		return 0, fmt.Errorf("unreadable job count: %q", trimmed)
	}
	return *reply.Running, nil
}

// keepBusyComputersAwake marks computers that look idle but run a
// background job as used, so the idle sweep that follows leaves them
// running. Only computers about to be put to sleep are asked.
func (m *Manager) keepBusyComputersAwake(ctx context.Context, idle string) {
	if m.db == nil {
		return
	}
	rows, err := m.db.ListIdleRunningWorkspaces(ctx, idle)
	if err != nil {
		m.logger.Warn("list idle computers failed", "error", err)
		return
	}
	for _, ws := range rows {
		checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		n, err := m.runningJobs(checkCtx, ws.BotID)
		cancel()
		if err != nil || n == 0 {
			continue
		}
		if err := m.db.TouchWorkspaceUsed(ctx, ws.BotID); err != nil {
			m.logger.Warn("keep busy computer awake failed", "bot_id", ws.BotID, "error", err)
			continue
		}
		m.logger.Info("computer kept awake for its background jobs", "bot_id", ws.BotID, "jobs", n)
	}
}

// busyWithJobs reports whether botID's computer runs a background job; an
// error counts as busy, so a computer is never evicted on a failed check.
func (m *Manager) busyWithJobs(ctx context.Context, botID uuid.UUID) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	n, err := m.runningJobs(checkCtx, botID)
	return err != nil || n > 0
}
