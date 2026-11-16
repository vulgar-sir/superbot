package sandbox

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"superbot/internal/config"
	"superbot/internal/store"
)

const (
	maxOutputBytes = 256 * 1024
	sandboxMemory  = 512 * 1024 * 1024
	sandboxPids    = 256
	computerMemory = 2 * 1024 * 1024 * 1024
	computerPids   = 512
)

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Info struct {
	Name      string
	Profile   string
	VNCAddr   string
	CDPAddr   string
	Workspace string
}

// Manager owns per-bot Docker containers. The worker is the only process that
// talks to the Docker Engine.
type Manager struct {
	cli            *client.Client
	cfg            config.Config
	db             *store.DB
	logger         *slog.Logger
	image          string
	desktopImage   string
	hostAlias      string
	sandboxNetwork string

	// botLocks serializes container lifecycle work per bot so one slow bot
	// (e.g. waiting for CDP) does not stall every other bot.
	botLocks keyedMutex[uuid.UUID]
}

func New(cfg config.Config, db *store.DB, logger *slog.Logger) (*Manager, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}
	image := strings.TrimSpace(os.Getenv("SUPERBOT_SANDBOX_IMAGE"))
	if image == "" {
		image = "superbot/computer:latest"
	}
	desktopImage := strings.TrimSpace(os.Getenv("SUPERBOT_DESKTOP_IMAGE"))
	if desktopImage == "" {
		desktopImage = "superbot/computer-desktop:latest"
	}
	hostAlias := strings.TrimSpace(os.Getenv("SUPERBOT_DOCKER_HOST_ALIAS"))
	if hostAlias == "" {
		hostAlias = "127.0.0.1"
	}
	sandboxNetwork := strings.TrimSpace(os.Getenv("SUPERBOT_SANDBOX_NETWORK"))
	return &Manager{
		cli:            cli,
		cfg:            cfg,
		db:             db,
		logger:         logger,
		image:          image,
		desktopImage:   desktopImage,
		hostAlias:      hostAlias,
		sandboxNetwork: sandboxNetwork,
	}, nil
}

func (m *Manager) Close() error {
	return m.cli.Close()
}

func (m *Manager) Ping(ctx context.Context) error {
	_, err := m.cli.Ping(ctx, client.PingOptions{})
	return err
}

func (m *Manager) WorkspacePath(botID uuid.UUID) string {
	return filepath.Join(m.cfg.DataDir, "workspaces", botID.String())
}

func containerName(botID uuid.UUID) string {
	raw := strings.ReplaceAll(botID.String(), "-", "")
	if len(raw) > 12 {
		raw = raw[:12]
	}
	return "superbot-bot-" + raw
}

func normalizeProfile(profile string) string {
	switch profile {
	case "browser", "desktop":
		return profile
	default:
		return "none"
	}
}

// Ensure returns a running sandbox for the bot. Browser/desktop profiles use
// the full computer image with Chromium, noVNC and CDP.
func (m *Manager) Ensure(ctx context.Context, ownerID, botID uuid.UUID, profile string) (Info, error) {
	profile = normalizeProfile(profile)
	workspace := m.WorkspacePath(botID)
	if err := m.EnsureDirs(ctx, workspace); err != nil {
		return Info{}, fmt.Errorf("create workspace: %w", err)
	}

	unlock := m.botLocks.Lock(botID)
	defer unlock()

	// Only the shared directories of the bot's own teams are mounted; the
	// container is recreated when its team membership changes.
	teamMounts, teamsLabel, err := m.teamMounts(ctx, botID)
	if err != nil {
		return Info{}, err
	}

	name := containerName(botID)
	if info, err := m.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); err == nil {
		labels := info.Container.Config.Labels
		existingProfile := normalizeProfile(labels["superbot.profile"])
		if existingProfile != profile || labels[teamsLabelKey] != teamsLabel || labels[mountsVersionKey] != mountsVersion {
			// Profile or team membership changed, or the container predates
			// per-team mounts; recreate it. The workspace persists.
			timeout := 5
			_, _ = m.cli.ContainerStop(ctx, info.Container.ID, client.ContainerStopOptions{Timeout: &timeout})
			_, _ = m.cli.ContainerRemove(ctx, info.Container.ID, client.ContainerRemoveOptions{Force: true})
		} else {
			if info.Container.State == nil || !info.Container.State.Running {
				if _, err := m.cli.ContainerStart(ctx, info.Container.ID, client.ContainerStartOptions{}); err != nil {
					return Info{}, fmt.Errorf("start sandbox: %w", err)
				}
				info, err = m.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
				if err != nil {
					return Info{}, fmt.Errorf("inspect sandbox: %w", err)
				}
			}
			return m.infoFromInspect(ctx, ownerID, botID, profile, workspace, name, info.Container)
		}
	}

	created, err := m.cli.ContainerCreate(ctx, ContainerSpec{
		Image:          m.image,
		DesktopImage:   m.desktopImage,
		SandboxNetwork: m.sandboxNetwork,
		BotID:          botID,
		Profile:        profile,
		Workspace:      workspace,
		TeamMounts:     teamMounts,
		TeamsLabel:     teamsLabel,
	}.CreateOptions())
	if err != nil {
		if info, inspectErr := m.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); inspectErr == nil {
			if info.Container.State == nil || !info.Container.State.Running {
				_, _ = m.cli.ContainerStart(ctx, info.Container.ID, client.ContainerStartOptions{})
			}
			return m.infoFromInspect(ctx, ownerID, botID, profile, workspace, name, info.Container)
		}
		return Info{}, fmt.Errorf("create sandbox: %w", err)
	}
	if _, err := m.cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return Info{}, fmt.Errorf("start sandbox: %w", err)
	}
	info, err := m.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err != nil {
		return Info{}, fmt.Errorf("inspect sandbox: %w", err)
	}
	m.logger.Info("sandbox started", "bot_id", botID, "container", name, "profile", profile)
	return m.infoFromInspect(ctx, ownerID, botID, profile, workspace, name, info.Container)
}

// ContainerSpec holds what goes into a bot sandbox's create request.
type ContainerSpec struct {
	Image          string
	DesktopImage   string
	SandboxNetwork string
	BotID          uuid.UUID
	Profile        string
	Workspace      string
	TeamMounts     []string
	TeamsLabel     string
}

// CreateOptions builds the POST /containers/create request Ensure sends. It
// is kept separate so the Docker API proxy (internal/dockerproxy) can test
// its allowlist against exactly what the worker sends.
func (s ContainerSpec) CreateOptions() client.ContainerCreateOptions {
	profile := normalizeProfile(s.Profile)
	isComputer := profile != "none"
	configObj := &container.Config{
		Image:      s.Image,
		WorkingDir: "/workspace",
		User:       "1000:1000",
		Env: []string{
			"HOME=/tmp",
			"TMPDIR=/tmp",
			"DISPLAY=:99",
			"TZ=CST-8",
			"PYTHONDONTWRITEBYTECODE=1",
			"PIP_NO_CACHE_DIR=1",
			"LANG=C.UTF-8",
		},
		Labels: map[string]string{
			"superbot.bot_id":  s.BotID.String(),
			"superbot.profile": profile,
			teamsLabelKey:      s.TeamsLabel,
			mountsVersionKey:   mountsVersion,
		},
	}
	hostConfig := &container.HostConfig{
		Binds:         append([]string{filepath.ToSlash(s.Workspace) + ":/workspace"}, s.TeamMounts...),
		AutoRemove:    false,
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		CapDrop:       []string{"ALL"},
		SecurityOpt:   []string{"no-new-privileges"},
		ShmSize:       64 * 1024 * 1024,
	}
	if isComputer {
		if profile == "desktop" {
			configObj.Image = s.DesktopImage
			configObj.Cmd = []string{"/usr/local/bin/start-desktop.sh"}
		} else {
			configObj.Image = s.Image
			configObj.Cmd = []string{"/usr/local/bin/start-browser.sh"}
		}
		if s.SandboxNetwork != "" {
			// Production: computers live on a dedicated Docker network with
			// the API and worker. No host ports are published.
			hostConfig.NetworkMode = container.NetworkMode(s.SandboxNetwork)
		} else {
			// Local development: the API and worker run on the host, so
			// publish dynamic loopback ports instead.
			hostConfig.NetworkMode = container.NetworkMode("bridge")
			exposed := network.PortSet{}
			bindings := network.PortMap{}
			for _, raw := range []string{"9223/tcp", "6080/tcp"} {
				port := network.MustParsePort(raw)
				exposed[port] = struct{}{}
				bindings[port] = []network.PortBinding{{
					HostIP:   netip.MustParseAddr("127.0.0.1"),
					HostPort: "",
				}}
			}
			configObj.ExposedPorts = exposed
			hostConfig.PortBindings = bindings
		}
		hostConfig.ShmSize = 512 * 1024 * 1024
		hostConfig.Tmpfs = map[string]string{"/tmp": "rw,size=1g,mode=1777"}
		hostConfig.Resources = container.Resources{
			Memory:     computerMemory,
			MemorySwap: -1,
			NanoCPUs:   2_000_000_000,
			PidsLimit:  int64Ptr(computerPids),
		}
	} else {
		configObj.Image = s.Image
		configObj.Cmd = []string{"sleep", "infinity"}
		hostConfig.NetworkMode = container.NetworkMode("none")
		hostConfig.Tmpfs = map[string]string{"/tmp": "rw,size=128m,mode=1777"}
		hostConfig.Resources = container.Resources{
			Memory:     sandboxMemory,
			MemorySwap: -1,
			NanoCPUs:   1_000_000_000,
			PidsLimit:  int64Ptr(sandboxPids),
		}
	}
	return client.ContainerCreateOptions{
		Name:       containerName(s.BotID),
		Config:     configObj,
		HostConfig: hostConfig,
	}
}

const (
	teamsLabelKey    = "superbot.teams"
	mountsVersionKey = "superbot.mounts"
	// mountsVersion changes whenever the mount layout does, so containers
	// created under an older layout are rebuilt once.
	mountsVersion = "2"
)

// teamMounts returns the bind mounts for the shared directories of the
// teams the bot belongs to, and a label value identifying that set.
func (m *Manager) teamMounts(ctx context.Context, botID uuid.UUID) ([]string, string, error) {
	if m.db == nil {
		return nil, "", nil
	}
	teamIDs, err := m.db.ListTeamIDsForBot(ctx, botID)
	if err != nil {
		return nil, "", fmt.Errorf("list bot teams: %w", err)
	}
	return teamBinds(m.cfg.DataDir, teamIDs, func(dir string) error {
		if err := m.EnsureDirs(ctx, dir); err != nil {
			return fmt.Errorf("create team shared dir: %w", err)
		}
		return nil
	})
}

func teamBinds(dataDir string, teamIDs []uuid.UUID, ensureDir func(string) error) ([]string, string, error) {
	ids := make([]string, 0, len(teamIDs))
	binds := make([]string, 0, len(teamIDs))
	sorted := append([]uuid.UUID(nil), teamIDs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].String() < sorted[j].String() })
	for _, id := range sorted {
		host := filepath.Join(dataDir, "teams", id.String(), "shared")
		if err := ensureDir(host); err != nil {
			return nil, "", err
		}
		binds = append(binds, filepath.ToSlash(host)+":/teams/"+id.String()+"/shared")
		ids = append(ids, id.String())
	}
	return binds, strings.Join(ids, ","), nil
}

func (m *Manager) infoFromInspect(ctx context.Context, ownerID, botID uuid.UUID, profile, workspace, name string, inspected container.InspectResponse) (Info, error) {
	vncAddr := ""
	cdpAddr := ""
	if profile != "none" {
		if m.sandboxNetwork != "" {
			vncAddr = name + ":6080"
			cdpAddr = name + ":9223"
		} else {
			vncAddr = m.hostAddr(inspected.NetworkSettings, "6080/tcp")
			cdpAddr = m.hostAddr(inspected.NetworkSettings, "9223/tcp")
		}
		if err := m.waitForCDP(ctx, cdpAddr, 45*time.Second); err != nil {
			m.logger.Warn("computer cdp not ready", "bot_id", botID, "error", err)
		}
	}
	if m.db != nil {
		if _, err := m.db.UpsertWorkspaceRunning(ctx, store.UpsertWorkspaceRunningParams{
			OwnerID:       ownerID,
			BotID:         botID,
			Profile:       profile,
			ContainerName: name,
			WorkspacePath: workspace,
			VncAddr:       vncAddr,
			CdpAddr:       cdpAddr,
		}); err != nil {
			m.logger.Warn("record workspace failed", "bot_id", botID, "error", err)
		}
	}
	return Info{Name: name, Profile: profile, VNCAddr: vncAddr, CDPAddr: cdpAddr, Workspace: workspace}, nil
}

func (m *Manager) hostAddr(settings *container.NetworkSettings, port string) string {
	if settings == nil {
		return ""
	}
	for p, bindings := range settings.Ports {
		if strings.EqualFold(p.String(), port) && len(bindings) > 0 && bindings[0].HostPort != "" {
			return m.hostAlias + ":" + bindings[0].HostPort
		}
	}
	return ""
}

func (m *Manager) waitForCDP(ctx context.Context, addr string, timeout time.Duration) error {
	if addr == "" {
		return fmt.Errorf("cdp address is empty")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, "http://"+addr+"/json/version", nil)
		if err == nil {
			// Chrome rejects non-loopback Host headers; dial the alias but
			// present 127.0.0.1 so the probe returns JSON.
			if _, port, found := strings.Cut(addr, ":"); found && port != "" {
				req.Host = "127.0.0.1:" + port
			}
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				cancel()
				if resp.StatusCode == 200 {
					return nil
				}
				continue
			}
		}
		cancel()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for CDP")
}

func (m *Manager) Exec(ctx context.Context, ownerID, botID uuid.UUID, profile string, cmd []string, workdir string, timeout time.Duration) (ExecResult, error) {
	info, err := m.Ensure(ctx, ownerID, botID, profile)
	if err != nil {
		return ExecResult{}, err
	}
	return m.execInContainer(ctx, info.Name, cmd, workdir, timeout)
}

func (m *Manager) execInContainer(ctx context.Context, name string, cmd []string, workdir string, timeout time.Duration) (ExecResult, error) {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	seconds := int(timeout / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	if workdir == "" {
		workdir = "/workspace"
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout+15*time.Second)
	defer cancel()
	marker := uuid.NewString()
	wrapped := wrapExecCommand(marker, seconds, cmd)

	created, err := m.cli.ExecCreate(execCtx, name, client.ExecCreateOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          wrapped,
		WorkingDir:   workdir,
		Env:          []string{"HOME=/tmp", "TMPDIR=/tmp", "TZ=CST-8", "PYTHONDONTWRITEBYTECODE=1", "LANG=C.UTF-8", execMarker(marker)},
	})
	if err != nil {
		return ExecResult{}, fmt.Errorf("create exec: %w", err)
	}
	attach, err := m.cli.ExecAttach(execCtx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return ExecResult{}, fmt.Errorf("attach exec: %w", err)
	}
	defer attach.Close()

	stdout := newBoundedBuffer(maxOutputBytes)
	stderr := newBoundedBuffer(maxOutputBytes)
	copyDone := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(stdout, stderr, attach.Reader)
		copyDone <- err
	}()
	select {
	case err := <-copyDone:
		if err != nil && execCtx.Err() == nil {
			return ExecResult{}, fmt.Errorf("read exec output: %w", err)
		}
	case <-execCtx.Done():
		// The attach stream is not tied to the context, and closing it does
		// not stop the process: without an explicit kill it would run on
		// until its timeout wrapper fires.
		m.killExec(ctx, name, marker)
		attach.Close()
		<-copyDone
	}
	exitCode := -1
	inspectCtx, inspectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer inspectCancel()
	for i := 0; i < 25; i++ {
		info, err := m.cli.ExecInspect(inspectCtx, created.ID, client.ExecInspectOptions{})
		if err == nil && !info.Running {
			exitCode = info.ExitCode
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, nil
}

func (m *Manager) Stop(ctx context.Context, botID uuid.UUID) error {
	name := containerName(botID)
	info, err := m.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err != nil {
		_, _ = m.db.MarkWorkspaceStopped(ctx, botID)
		return nil
	}
	timeout := 5
	if _, err := m.cli.ContainerStop(ctx, info.Container.ID, client.ContainerStopOptions{Timeout: &timeout}); err != nil {
		return err
	}
	_, _ = m.db.MarkWorkspaceStopped(ctx, botID)
	return nil
}

// ProcessActions applies update/reset actions requested from the UI. Update
// recreates the container but keeps the workspace; reset also removes the
// workspace directory.
func (m *Manager) ProcessActions(ctx context.Context) {
	actions, err := m.db.ClaimWorkspaceActions(ctx, 5)
	if err != nil {
		m.logger.Warn("claim workspace actions failed", "error", err)
		return
	}
	for _, action := range actions {
		bot, err := m.db.GetBot(ctx, store.GetBotParams{ID: action.BotID, OwnerID: action.OwnerID})
		if err != nil {
			_ = m.db.MarkWorkspaceActionFailed(ctx, store.MarkWorkspaceActionFailedParams{
				ID: action.ID, Error: "bot not found",
			})
			continue
		}
		if action.Kind == "snapshot" {
			if err := m.snapshotWorkspace(ctx, action); err != nil {
				_ = m.db.MarkWorkspaceActionFailed(ctx, store.MarkWorkspaceActionFailedParams{
					ID: action.ID, Error: err.Error(),
				})
				continue
			}
			_ = m.db.MarkWorkspaceActionDone(ctx, action.ID)
			continue
		}
		if action.Kind == "restore" {
			if err := m.restoreWorkspace(ctx, action, bot); err != nil {
				_ = m.db.MarkWorkspaceActionFailed(ctx, store.MarkWorkspaceActionFailedParams{
					ID: action.ID, Error: err.Error(),
				})
				continue
			}
			_ = m.db.MarkWorkspaceActionDone(ctx, action.ID)
			continue
		}
		name := containerName(action.BotID)
		if info, inspectErr := m.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); inspectErr == nil {
			timeout := 5
			_, _ = m.cli.ContainerStop(ctx, info.Container.ID, client.ContainerStopOptions{Timeout: &timeout})
			_, _ = m.cli.ContainerRemove(ctx, info.Container.ID, client.ContainerRemoveOptions{Force: true})
		}
		if action.Kind == "reset" {
			if err := clearDir(m.WorkspacePath(action.BotID)); err != nil {
				_ = m.db.MarkWorkspaceActionFailed(ctx, store.MarkWorkspaceActionFailedParams{
					ID: action.ID, Error: err.Error(),
				})
				continue
			}
		}
		if _, err := m.Ensure(ctx, action.OwnerID, action.BotID, bot.ComputerProfile); err != nil {
			_ = m.db.MarkWorkspaceActionFailed(ctx, store.MarkWorkspaceActionFailedParams{
				ID: action.ID, Error: err.Error(),
			})
			continue
		}
		_ = m.db.MarkWorkspaceActionDone(ctx, action.ID)
	}
}

// Reconcile applies desired_state changes requested by the API.
func (m *Manager) Reconcile(ctx context.Context) {
	m.ProcessActions(ctx)
	// Put computers to sleep after 30 minutes without bot activity; a new
	// message touches the bot and wakes it on the next reconcile.
	if n, err := m.db.StopIdleWorkspaces(ctx, "30 minutes"); err == nil && n > 0 {
		m.logger.Info("auto-slept idle workspaces", "count", n)
	} else if err != nil {
		m.logger.Warn("auto-sleep workspaces failed", "error", err)
	}
	rows, err := m.db.ListWorkspacesForReconcile(ctx)
	if err != nil {
		m.logger.Warn("list workspaces for reconcile failed", "error", err)
		return
	}
	for _, ws := range rows {
		switch ws.DesiredState {
		case "running":
			bot, err := m.db.GetBot(ctx, store.GetBotParams{ID: ws.BotID, OwnerID: ws.OwnerID})
			if err != nil {
				_ = m.db.MarkWorkspaceError(ctx, store.MarkWorkspaceErrorParams{BotID: ws.BotID, LastError: err.Error()})
				continue
			}
			profile := normalizeProfile(bot.ComputerProfile)
			if _, err := m.Ensure(ctx, ws.OwnerID, ws.BotID, profile); err != nil {
				m.logger.Warn("ensure workspace failed", "bot_id", ws.BotID, "error", err)
				_ = m.db.MarkWorkspaceError(ctx, store.MarkWorkspaceErrorParams{BotID: ws.BotID, LastError: err.Error()})
			}
		case "stopped":
			if err := m.Stop(ctx, ws.BotID); err != nil {
				m.logger.Warn("stop workspace failed", "bot_id", ws.BotID, "error", err)
			}
		}
	}
}

func int64Ptr(value int64) *int64 { return &value }

func (m *Manager) snapshotWorkspace(ctx context.Context, action store.WorkspaceAction) error {
	workspace := m.WorkspacePath(action.BotID)
	if _, err := os.Stat(workspace); err != nil {
		return fmt.Errorf("workspace not found: %w", err)
	}
	directory := filepath.Join(m.cfg.DataDir, "snapshots", action.BotID.String())
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	path := filepath.Join(directory, fmt.Sprintf("%d.tar.gz", time.Now().Unix()))
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	writer := gzip.NewWriter(file)
	archive := tar.NewWriter(writer)
	walkErr := filepath.Walk(workspace, func(item string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(workspace, item)
		if err != nil || relative == "." {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			source, err := os.Open(item)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(archive, source)
			source.Close()
			return copyErr
		}
		return nil
	})
	closeErr1 := archive.Close()
	closeErr2 := writer.Close()
	closeErr3 := file.Close()
	if walkErr != nil || closeErr1 != nil || closeErr2 != nil || closeErr3 != nil {
		_ = os.Remove(path)
		return fmt.Errorf("archive workspace: %v %v %v %v", walkErr, closeErr1, closeErr2, closeErr3)
	}
	info, _ := os.Stat(path)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	_, err = m.db.CreateComputerSnapshot(ctx, store.CreateComputerSnapshotParams{
		AccountID: action.OwnerID, BotID: action.BotID,
		Label: time.Now().Format("2006-01-02 15:04:05"), ArchivePath: path, SizeBytes: size,
	})
	return err
}

func (m *Manager) restoreWorkspace(ctx context.Context, action store.WorkspaceAction, bot store.Bot) error {
	if action.SnapshotID == nil {
		return fmt.Errorf("snapshot is required")
	}
	snapshot, err := m.db.GetComputerSnapshot(ctx, store.GetComputerSnapshotParams{
		ID: *action.SnapshotID, AccountID: action.OwnerID,
	})
	if err != nil {
		return fmt.Errorf("snapshot not found")
	}
	name := containerName(action.BotID)
	if info, inspectErr := m.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); inspectErr == nil {
		timeout := 5
		_, _ = m.cli.ContainerStop(ctx, info.Container.ID, client.ContainerStopOptions{Timeout: &timeout})
		_, _ = m.cli.ContainerRemove(ctx, info.Container.ID, client.ContainerRemoveOptions{Force: true})
	}
	workspace := m.WorkspacePath(action.BotID)
	if err := clearDir(workspace); err != nil {
		return err
	}
	if err := m.EnsureDirs(ctx, workspace); err != nil {
		return err
	}
	file, err := os.Open(snapshot.ArchivePath)
	if err != nil {
		return fmt.Errorf("open snapshot: %w", err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer reader.Close()
	archive := tar.NewReader(reader)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(workspace, filepath.FromSlash(header.Name))
		if !strings.HasPrefix(target, workspace+string(os.PathSeparator)) {
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, archive); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	if _, err := m.Ensure(ctx, action.OwnerID, action.BotID, bot.ComputerProfile); err != nil {
		return err
	}
	return nil
}
