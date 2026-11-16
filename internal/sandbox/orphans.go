package sandbox

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/moby/moby/client"
)

const orphanSweepEvery = 5 * time.Minute

// removeOrphans stops and removes computers whose bot no longer exists.
// Deleting a bot removes its database rows, including the workspace row the
// idle sweep works from, so without this its container kept running forever.
// A container is only removed when the database says positively that its bot
// is gone; any lookup error leaves it alone.
func (m *Manager) removeOrphans(ctx context.Context) int {
	if m.db == nil {
		return 0
	}
	m.orphanMu.Lock()
	if time.Since(m.lastOrphanSweep) < orphanSweepEvery {
		m.orphanMu.Unlock()
		return 0
	}
	m.lastOrphanSweep = time.Now()
	m.orphanMu.Unlock()

	listed, err := m.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: client.Filters{}.Add("label", "superbot.bot_id"),
	})
	if err != nil {
		m.logger.Warn("list bot containers failed", "error", err)
		return 0
	}
	removed := 0
	for _, item := range listed.Items {
		botID, err := uuid.Parse(item.Labels["superbot.bot_id"])
		if err != nil || !ownsName(item.Names, containerName(botID)) {
			continue
		}
		exists, err := m.db.BotExists(ctx, botID)
		if err != nil || exists {
			continue
		}
		unlock := m.botLocks.Lock(botID)
		timeout := 5
		_, _ = m.cli.ContainerStop(ctx, item.ID, client.ContainerStopOptions{Timeout: &timeout})
		_, err = m.cli.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true})
		if err == nil {
			// The bot is gone, so are its files; the directory itself stays
			// because its parent is not writable by the services.
			_ = clearDir(m.WorkspacePath(botID))
			removed++
		}
		unlock()
		if err != nil {
			m.logger.Warn("remove orphan computer failed", "bot_id", botID, "error", err)
		}
	}
	if removed > 0 {
		m.logger.Info("removed computers of deleted bots", "count", removed)
	}
	return removed
}

func ownsName(names []string, want string) bool {
	for _, name := range names {
		if strings.TrimPrefix(name, "/") == want {
			return true
		}
	}
	return false
}
