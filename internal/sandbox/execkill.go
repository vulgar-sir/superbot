package sandbox

import (
	"context"
	"fmt"
	"time"

	"github.com/moby/moby/client"
)

// execMarkerEnv tags every process of one exec. Docker has no "kill exec"
// API, so an abandoned exec is found again through what it left behind.
const execMarkerEnv = "SUPERBOT_EXEC_MARK"

const (
	execKillTimeout = 5 * time.Second
	execStateDir    = "/tmp/.sbexec"
)

// execWrapperScript runs a command as the leader of its own process group
// and records the group id and its own start time, so a cancelled exec can be
// found and killed as a whole. Invoked as: sh -c SCRIPT <marker> <timeout> cmd...
const execWrapperScript = `d=` + execStateDir + `; mkdir -p "$d" "${HOME:-/tmp}" 2>/dev/null
echo "$$ $(cut -d' ' -f22 /proc/$$/stat)" > "$d/$0" 2>/dev/null
timeout "$@"; rc=$?
rm -f "$d/$0"
exit $rc`

// execKillScript kills what an abandoned exec left running, in three ways:
// its process group; any process still carrying its marker in the
// environment; and any orphan adopted by PID 1 that started after the exec
// did, which catches processes that left the group and cleared their
// environment. It scans twice to catch children forked during the first
// pass. The marker reaches the script as an argument, so the script and its
// helpers never match. Background job runners (jobs.py) are orphans too, but
// were started on purpose and are never touched.
const execKillScript = `mark=$1
f=` + execStateDir + `/${mark#*=}
pgid=; since=
[ -f "$f" ] && read pgid since < "$f"
[ -n "$pgid" ] && /bin/kill -s KILL -- "-$pgid" 2>/dev/null
jobs=" "
for j in /tmp/.sbjobs/*; do
  [ -f "$j" ] && read jp _ < "$j" && jobs="$jobs$jp "
done
for pass in 1 2; do
  for d in /proc/[0-9]*; do
    pid=${d#/proc/}
    [ "$pid" = "$$" ] && continue
    [ "$pid" = 1 ] && continue
    if { tr '\0' '\n' < "$d/environ"; } 2>/dev/null | grep -qxF -- "$mark"; then
      kill -KILL "$pid" 2>/dev/null
      continue
    fi
    [ -n "$since" ] || continue
    case "$jobs" in *" $pid "*) continue ;; esac
    stat=$(cat "$d/stat" 2>/dev/null) || continue
    rest=${stat##*) }
    set -- $rest
    ppid=$2
    shift 19
    started=$1
    if [ "$ppid" = 1 ] && [ "$started" -ge "$since" ] 2>/dev/null; then
      kill -KILL "$pid" 2>/dev/null
    fi
  done
done
rm -f "$f"
exit 0`

func execMarker(id string) string { return execMarkerEnv + "=" + id }

// wrapExecCommand runs cmd in its own process group under a timeout.
func wrapExecCommand(id string, seconds int, cmd []string) []string {
	wrapped := []string{"setsid", "-w", "sh", "-c", execWrapperScript, id, fmt.Sprintf("%ds", seconds)}
	return append(wrapped, cmd...)
}

func execKillCommand(id string) []string {
	return []string{"sh", "-c", execKillScript, "sh", execMarker(id)}
}

// killExec stops the process tree of an abandoned exec. It runs on its own
// short deadline because the caller's context is already done. When the
// kill cannot run at all — typically because the container is at its
// process limit, so not even a new exec can start — the container is
// restarted, which ends everything in it; the workspace lives on the host
// and survives.
func (m *Manager) killExec(ctx context.Context, name, id string) {
	killCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), execKillTimeout)
	defer cancel()
	if err := m.runKill(killCtx, name, id); err != nil {
		m.logger.Warn("kill abandoned exec failed; restarting the container", "container", name, "error", err)
		restartCtx, restartCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer restartCancel()
		stopAfter := 2
		if _, err := m.cli.ContainerRestart(restartCtx, name, client.ContainerRestartOptions{Signal: "SIGKILL", Timeout: &stopAfter}); err != nil {
			m.logger.Error("restart after failed exec kill failed", "container", name, "error", err)
		}
	}
}

func (m *Manager) runKill(ctx context.Context, name, id string) error {
	// Same user as the bot's processes: with every capability dropped, root
	// in the container could not signal them.
	created, err := m.cli.ExecCreate(ctx, name, client.ExecCreateOptions{Cmd: execKillCommand(id)})
	if err != nil {
		return fmt.Errorf("create kill exec: %w", err)
	}
	if _, err := m.cli.ExecStart(ctx, created.ID, client.ExecStartOptions{Detach: true}); err != nil {
		return fmt.Errorf("start kill exec: %w", err)
	}
	for ctx.Err() == nil {
		info, err := m.cli.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
		if err != nil {
			return fmt.Errorf("inspect kill exec: %w", err)
		}
		if !info.Running {
			if info.ExitCode != 0 {
				return fmt.Errorf("kill exec exited with %d", info.ExitCode)
			}
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return ctx.Err()
}
