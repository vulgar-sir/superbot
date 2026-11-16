package sandbox

import (
	"context"
	"time"

	"github.com/moby/moby/client"
)

// execMarkerEnv tags every process of one exec. Docker has no "kill exec"
// API, so an abandoned exec is found again through the environment its
// process tree inherited.
const execMarkerEnv = "SUPERBOT_EXEC_MARK"

const execKillTimeout = 5 * time.Second

// execKillScript SIGKILLs every process whose environment carries the marker
// passed as $1. It scans twice to catch children forked during the first
// pass. The marker reaches the script as an argument, so the script and its
// own helpers never match.
const execKillScript = `for pass in 1 2; do
  for d in /proc/[0-9]*; do
    pid=${d#/proc/}
    [ "$pid" = "$$" ] && continue
    if { tr '\0' '\n' < "$d/environ"; } 2>/dev/null | grep -qxF -- "$1"; then
      kill -KILL "$pid" 2>/dev/null
    fi
  done
done
exit 0`

func execMarker(id string) string { return execMarkerEnv + "=" + id }

func execKillCommand(id string) []string {
	return []string{"sh", "-c", execKillScript, "sh", execMarker(id)}
}

// killExec best-effort kills the process tree of an abandoned exec. It runs
// on its own short deadline because the caller's context is already done.
func (m *Manager) killExec(ctx context.Context, name, id string) {
	killCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), execKillTimeout)
	defer cancel()
	created, err := m.cli.ExecCreate(killCtx, name, client.ExecCreateOptions{Cmd: execKillCommand(id)})
	if err != nil {
		m.logger.Warn("kill abandoned exec failed", "container", name, "error", err)
		return
	}
	if _, err := m.cli.ExecStart(killCtx, created.ID, client.ExecStartOptions{Detach: true}); err != nil {
		m.logger.Warn("kill abandoned exec failed", "container", name, "error", err)
	}
}
