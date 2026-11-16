package sandbox

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/moby/moby/client"

	"superbot/internal/store"
)

// Every bot has its own computer. It is created with the bot, sleeps when it
// is not used, and is removed with the bot. The rules below keep many
// computers affordable on one server:
//
//   - a new computer is created but not started; a stopped container costs no
//     CPU or memory, only its (tiny) writable layer, and every computer shares
//     one image;
//   - computers sleep after 30 minutes without use (Reconcile);
//   - at most maxRunning computers run at once; starting one more puts the
//     least recently used idle computer to sleep first;
//   - deleting a bot removes its container, workspace files and snapshots
//     within seconds, and a periodic sweep removes anything left behind;
//   - Chromium keeps its disk cache in the container's tmpfs, capped, so
//     browsing does not grow the workspace on disk.

const (
	// evictIdleAfter protects a computer that is in the middle of a task from
	// being put to sleep to make room for another one.
	evictIdleAfter = 5 * time.Minute
	// Per sweep, how many workspaces get their disk usage measured, and how
	// many entries a single measurement may walk.
	measurePerSweep = 10
	measureMaxFiles = 200_000
)

// maxRunningComputers reads SUPERBOT_MAX_RUNNING_COMPUTERS, or derives a cap
// from the host's memory: a running browser computer uses about 400 MB, so
// leaving 4 GiB for the services and budgeting 768 MiB per computer keeps
// the server responsive with headroom for heavy pages.
func maxRunningComputers() int {
	if raw := strings.TrimSpace(os.Getenv("SUPERBOT_MAX_RUNNING_COMPUTERS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	total := hostMemoryBytes()
	if total <= 0 {
		return 8
	}
	n := int((total - 4<<30) / (768 << 20))
	return min(max(n, 2), 32)
}

func hostMemoryBytes() int64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil {
				return kb * 1024
			}
		}
	}
	return 0
}

// Provision creates the bot's computer without starting it, so a new bot has
// a computer straight away. An existing container is left as it is; Ensure
// rebuilds it on the next start if its configuration is out of date.
func (m *Manager) Provision(ctx context.Context, ownerID, botID uuid.UUID, profile string) error {
	_, err := m.ensure(ctx, ownerID, botID, profile, false)
	return err
}

// makeRoom puts least recently used idle computers to sleep when starting
// botID's computer would exceed the running cap. It runs before botID's own
// lock is taken and holds one other bot's lock at a time, so two bots making
// room for each other cannot deadlock. The cap is soft: if every running
// computer is busy, the new one starts anyway.
func (m *Manager) makeRoom(ctx context.Context, botID uuid.UUID) {
	if m.db == nil || m.maxRunning <= 0 {
		return
	}
	running, err := m.db.ListRunningWorkspaces(ctx)
	if err != nil {
		return
	}
	others := running[:0:0]
	for _, ws := range running {
		if ws.BotID == botID {
			return // already running; nothing new starts
		}
		others = append(others, ws)
	}
	excess := len(others) + 1 - m.maxRunning
	for _, ws := range others {
		if excess <= 0 {
			return
		}
		if time.Since(ws.LastUsed) < evictIdleAfter {
			break // sorted oldest first: the rest are busier still
		}
		unlock := m.botLocks.Lock(ws.BotID)
		err := m.Stop(ctx, ws.BotID)
		unlock()
		if err != nil {
			m.logger.Warn("put idle computer to sleep failed", "bot_id", ws.BotID, "error", err)
			continue
		}
		m.logger.Info("put idle computer to sleep to make room", "bot_id", ws.BotID, "for_bot", botID, "cap", m.maxRunning)
		excess--
	}
	if excess > 0 {
		m.logger.Warn("running computer cap reached; starting anyway", "bot_id", botID, "cap", m.maxRunning, "running", len(others))
	}
}

// processRemovals deletes the computers of deleted bots. The bots table
// queues them (computer_removals) in the same transaction that deletes the
// bot, so this runs within one reconcile tick of the deletion.
func (m *Manager) processRemovals(ctx context.Context) {
	if m.db == nil {
		return
	}
	due, err := m.db.ListDueComputerRemovals(ctx, 10)
	if err != nil {
		m.logger.Warn("list computer removals failed", "error", err)
		return
	}
	for _, removal := range due {
		// A bot can only be deleted once, but stay safe if the ID comes back.
		if exists, err := m.db.BotExists(ctx, removal.BotID); err != nil || exists {
			if err == nil {
				_ = m.db.DeleteComputerRemoval(ctx, removal.BotID)
			}
			continue
		}
		if err := m.removeComputer(ctx, removal.BotID); err != nil {
			m.logger.Warn("remove computer of deleted bot failed", "bot_id", removal.BotID, "attempt", removal.Attempts+1, "error", err)
			_ = m.db.DeferComputerRemoval(ctx, store.DeferComputerRemovalParams{BotID: removal.BotID, LastError: err.Error()})
			continue
		}
		_ = m.db.DeleteComputerRemoval(ctx, removal.BotID)
		m.logger.Info("removed computer of deleted bot", "bot_id", removal.BotID)
	}
}

// removeComputer removes a deleted bot's container, workspace directory and
// snapshots. Callers make sure the bot no longer exists.
func (m *Manager) removeComputer(ctx context.Context, botID uuid.UUID) error {
	unlock := m.botLocks.Lock(botID)
	defer unlock()
	name := containerName(botID)
	info, err := m.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	switch {
	case err == nil:
		// The name holds only 12 hex digits of the ID; never remove another
		// bot's computer on a prefix collision.
		if info.Container.Config != nil && info.Container.Config.Labels["superbot.bot_id"] == botID.String() {
			timeout := 2
			_, _ = m.cli.ContainerStop(ctx, info.Container.ID, client.ContainerStopOptions{Timeout: &timeout})
			if _, err := m.cli.ContainerRemove(ctx, info.Container.ID, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
				return fmt.Errorf("remove container: %w", err)
			}
		}
	case cerrdefs.IsNotFound(err):
	default:
		return fmt.Errorf("inspect container: %w", err)
	}
	if err := m.RemoveDirs(ctx, m.WorkspacePath(botID)); err != nil {
		return fmt.Errorf("remove workspace: %w", err)
	}
	if err := forceRemoveAll(filepath.Join(m.cfg.DataDir, "snapshots", botID.String())); err != nil {
		return fmt.Errorf("remove snapshots: %w", err)
	}
	return nil
}

// removeLeftoverDirs removes workspace and snapshot directories whose bot no
// longer exists, such as those of bots deleted before computers were removed
// with their bot. A directory is only removed when the database says
// positively that its bot is gone.
func (m *Manager) removeLeftoverDirs(ctx context.Context) int {
	if m.db == nil {
		return 0
	}
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, parent := range []string{"workspaces", "snapshots"} {
		entries, err := os.ReadDir(filepath.Join(m.cfg.DataDir, parent))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			id, err := uuid.Parse(entry.Name())
			if err != nil || !entry.IsDir() || seen[id] || id.String() != entry.Name() {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return 0
	}
	existing, err := m.db.ExistingBotIDs(ctx, ids)
	if err != nil {
		m.logger.Warn("check bots of workspace directories failed", "error", err)
		return 0
	}
	alive := make(map[uuid.UUID]bool, len(existing))
	for _, id := range existing {
		alive[id] = true
	}
	removed := 0
	for _, id := range ids {
		if alive[id] {
			continue
		}
		if err := m.removeComputer(ctx, id); err != nil {
			m.logger.Warn("remove leftover computer files failed", "bot_id", id, "error", err)
			continue
		}
		removed++
	}
	if removed > 0 {
		m.logger.Info("removed files of deleted bots", "count", removed)
	}
	return removed
}

// measureDisks records how much disk each recently used workspace takes, for
// the computer view. Only workspaces used since their last measurement (or
// not measured for hours) are walked, a few per sweep.
func (m *Manager) measureDisks(ctx context.Context) {
	if m.db == nil {
		return
	}
	botIDs, err := m.db.ListWorkspacesToMeasure(ctx, measurePerSweep)
	if err != nil {
		return
	}
	for _, botID := range botIDs {
		size, err := dirSize(m.WorkspacePath(botID), measureMaxFiles)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			m.logger.Debug("measure workspace failed", "bot_id", botID, "error", err)
		}
		_ = m.db.SetWorkspaceDiskUsage(ctx, store.SetWorkspaceDiskUsageParams{BotID: botID, DiskBytes: size})
	}
}

// dirSize adds up the sizes of the regular files under dir without following
// links, stopping after maxFiles entries so one huge tree cannot stall the
// worker.
func dirSize(dir string, maxFiles int) (int64, error) {
	var total int64
	count := 0
	errStop := errors.New("stop")
	err := filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return nil
			}
			return err
		}
		count++
		if count > maxFiles {
			return errStop
		}
		if entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	if errors.Is(err, errStop) {
		err = nil
	}
	return total, err
}
