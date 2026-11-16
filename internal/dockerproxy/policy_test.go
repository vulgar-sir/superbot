package dockerproxy

import (
	"encoding/json"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"superbot/internal/sandbox"
)

var (
	testBotID  = uuid.MustParse("0d4c7a52-9f3e-4b1a-8c2d-5e6f7a8b9c0d")
	testTeamID = uuid.MustParse("aaaaaaaa-1111-2222-3333-444444444444")
)

// managerSpec returns exactly what sandbox.Manager.Ensure sends for a
// profile. sandboxNetwork "" is local development (bridge + loopback ports).
func managerSpec(profile, sandboxNetwork string) client.ContainerCreateOptions {
	return sandbox.ContainerSpec{
		Image:          "superbot/computer:latest",
		DesktopImage:   "superbot/computer-desktop:latest",
		SandboxNetwork: sandboxNetwork,
		BotID:          testBotID,
		Profile:        profile,
		Workspace:      testBindRoot + "/workspaces/" + testBotID.String(),
		// Same format as teamBinds in manager.go.
		TeamMounts: []string{testBindRoot + "/teams/" + testTeamID.String() + "/shared:/teams/" + testTeamID.String() + "/shared"},
		TeamsLabel: testTeamID.String(),
	}.CreateOptions()
}

func createBody(t *testing.T, opts client.ContainerCreateOptions) []byte {
	t.Helper()
	body, err := json.Marshal(container.CreateRequest{Config: opts.Config, HostConfig: opts.HostConfig, NetworkingConfig: opts.NetworkingConfig})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestCreateAllowsEveryManagerProfile(t *testing.T) {
	pol := newPolicy(testConfig("/var/run/docker.sock"))
	for _, profile := range []string{"none", "browser", "desktop"} {
		for _, network := range []string{"superbot_sandbox", ""} {
			opts := managerSpec(profile, network)
			name := profile + "/" + network
			if _, err := checkQuery(&route{kind: routeContainerCreate}, url.Values{"name": {opts.Name}}.Encode()); err != nil {
				t.Errorf("%s: manager container name %q refused: %v", name, opts.Name, err)
			}
			forwarded, err := pol.checkCreate(createBody(t, opts))
			if err != nil {
				t.Errorf("%s: manager create request refused: %v", name, err)
				continue
			}
			var got container.CreateRequest
			if err := json.Unmarshal(forwarded, &got); err != nil {
				t.Fatalf("%s: forwarded body is not JSON: %v", name, err)
			}
			if string(createBody(t, client.ContainerCreateOptions{Config: got.Config, HostConfig: got.HostConfig})) != string(createBody(t, opts)) {
				t.Errorf("%s: forwarded body differs from the manager's request", name)
			}
		}
	}
}

func TestCreateRejections(t *testing.T) {
	pol := newPolicy(testConfig("/var/run/docker.sock"))
	cases := []struct {
		name   string
		mutate func(*container.Config, *container.HostConfig)
		reason string
	}{
		{"privileged", func(_ *container.Config, h *container.HostConfig) { h.Privileged = true }, "privileged"},
		{"cap add", func(_ *container.Config, h *container.HostConfig) { h.CapAdd = []string{"CAP_SYS_ADMIN"} }, "CapAdd"},
		{"no cap drop", func(_ *container.Config, h *container.HostConfig) { h.CapDrop = nil }, "CapDrop must contain ALL"},
		{"partial cap drop", func(_ *container.Config, h *container.HostConfig) { h.CapDrop = []string{"CAP_NET_RAW"} }, "CapDrop must contain ALL"},
		{"no no-new-privileges", func(_ *container.Config, h *container.HostConfig) { h.SecurityOpt = nil }, "no-new-privileges"},
		{"seccomp unconfined", func(_ *container.Config, h *container.HostConfig) {
			h.SecurityOpt = append(h.SecurityOpt, "seccomp=unconfined")
		}, "security option"},
		{"apparmor unconfined", func(_ *container.Config, h *container.HostConfig) {
			h.SecurityOpt = append(h.SecurityOpt, "apparmor:unconfined")
		}, "security option"},
		{"host network", func(_ *container.Config, h *container.HostConfig) { h.NetworkMode = "host" }, "host networking"},
		{"container network", func(_ *container.Config, h *container.HostConfig) { h.NetworkMode = "container:superbot-postgres" }, "another container's network"},
		{"unlisted network", func(_ *container.Config, h *container.HostConfig) { h.NetworkMode = "superbot_default" }, "network mode"},
		{"default network mode", func(_ *container.Config, h *container.HostConfig) { h.NetworkMode = "" }, "network mode"},
		{"host pid", func(_ *container.Config, h *container.HostConfig) { h.PidMode = "host" }, "PidMode"},
		{"container pid", func(_ *container.Config, h *container.HostConfig) { h.PidMode = "container:x" }, "PidMode"},
		{"host ipc", func(_ *container.Config, h *container.HostConfig) { h.IpcMode = "host" }, "IpcMode"},
		{"host uts", func(_ *container.Config, h *container.HostConfig) { h.UTSMode = "host" }, "UTSMode"},
		{"host userns", func(_ *container.Config, h *container.HostConfig) { h.UsernsMode = "host" }, "UsernsMode"},
		{"host cgroupns", func(_ *container.Config, h *container.HostConfig) { h.CgroupnsMode = "host" }, "CgroupnsMode"},
		{"devices", func(_ *container.Config, h *container.HostConfig) {
			h.Devices = []container.DeviceMapping{{PathOnHost: "/dev/sda", PathInContainer: "/dev/sda", CgroupPermissions: "rwm"}}
		}, "devices"},
		{"device requests", func(_ *container.Config, h *container.HostConfig) {
			h.DeviceRequests = []container.DeviceRequest{{Driver: "nvidia", Count: -1}}
		}, "devices"},
		{"device cgroup rules", func(_ *container.Config, h *container.HostConfig) { h.DeviceCgroupRules = []string{"c *:* rwm"} }, "devices"},
		{"bind outside root", func(_ *container.Config, h *container.HostConfig) { h.Binds = append(h.Binds, "/etc:/host-etc") }, "outside"},
		{"bind sibling prefix", func(_ *container.Config, h *container.HostConfig) {
			h.Binds = append(h.Binds, "/srv/superbot/data-other/x:/x")
		}, "outside"},
		{"bind root itself", func(_ *container.Config, h *container.HostConfig) { h.Binds = append(h.Binds, testBindRoot+":/data") }, "outside"},
		{"dotdot escape", func(_ *container.Config, h *container.HostConfig) {
			h.Binds = append(h.Binds, testBindRoot+"/workspaces/../../../../etc:/x")
		}, "not a clean path"},
		{"docker socket bind", func(_ *container.Config, h *container.HostConfig) {
			h.Binds = append(h.Binds, "/var/run/docker.sock:/var/run/docker.sock")
		}, "Docker socket"},
		{"root bind", func(_ *container.Config, h *container.HostConfig) { h.Binds = append(h.Binds, "/:/host") }, "mounting /"},
		{"named volume", func(_ *container.Config, h *container.HostConfig) { h.Binds = append(h.Binds, "superbot_pgdata:/pg") }, "named volumes"},
		{"bind propagation option", func(_ *container.Config, h *container.HostConfig) {
			h.Binds[0] += ":rshared"
		}, "bind option"},
		{"malformed bind", func(_ *container.Config, h *container.HostConfig) {
			h.Binds = append(h.Binds, testBindRoot+"/workspaces/x")
		}, "malformed"},
		{"mount outside root", func(_ *container.Config, h *container.HostConfig) {
			h.Mounts = []mount.Mount{{Type: mount.TypeBind, Source: "/", Target: "/host"}}
		}, "mounting /"},
		{"mount docker socket", func(_ *container.Config, h *container.HostConfig) {
			h.Mounts = []mount.Mount{{Type: mount.TypeBind, Source: "/run/docker.sock", Target: "/s"}}
		}, "Docker socket"},
		{"volume mount", func(_ *container.Config, h *container.HostConfig) {
			h.Mounts = []mount.Mount{{Type: mount.TypeVolume, Source: "superbot_pgdata", Target: "/pg"}}
		}, "mount type"},
		{"volumes from", func(_ *container.Config, h *container.HostConfig) { h.VolumesFrom = []string{"superbot-worker-1"} }, "VolumesFrom"},
		{"anonymous volume", func(c *container.Config, _ *container.HostConfig) {
			c.Volumes = map[string]struct{}{"/data": {}}
		}, "anonymous volumes"},
		{"root user", func(c *container.Config, _ *container.HostConfig) { c.User = "root" }, "root"},
		{"uid 0", func(c *container.Config, _ *container.HostConfig) { c.User = "0" }, "root"},
		{"uid 0 gid 0", func(c *container.Config, _ *container.HostConfig) { c.User = "0:0" }, "root"},
		{"root group", func(c *container.Config, _ *container.HostConfig) { c.User = "1000:0" }, "root group"},
		{"empty user", func(c *container.Config, _ *container.HostConfig) { c.User = "" }, "non-root user"},
		{"disallowed image", func(c *container.Config, _ *container.HostConfig) { c.Image = "alpine:latest" }, "image"},
		{"missing label", func(c *container.Config, _ *container.HostConfig) { delete(c.Labels, botIDLabel) }, botIDLabel},
		{"publish all ports", func(_ *container.Config, h *container.HostConfig) { h.PublishAllPorts = true }, "PublishAllPorts"},
		{"port on all interfaces", func(_ *container.Config, h *container.HostConfig) {
			h.PortBindings = network.PortMap{network.MustParsePort("22/tcp"): {{HostPort: "2222"}}}
		}, "port binding"},
		{"fixed loopback port", func(_ *container.Config, h *container.HostConfig) {
			h.PortBindings = network.PortMap{network.MustParsePort("22/tcp"): {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "8080"}}}
		}, "port binding"},
		{"cgroup parent", func(_ *container.Config, h *container.HostConfig) { h.CgroupParent = "/" }, "cgroups"},
		{"runtime", func(_ *container.Config, h *container.HostConfig) { h.Runtime = "runc-debug" }, "runtimes"},
		{"log driver", func(_ *container.Config, h *container.HostConfig) {
			h.LogConfig = container.LogConfig{Type: "syslog", Config: map[string]string{"syslog-address": "udp://10.0.0.1:514"}}
		}, "log drivers"},
		{"unmasked proc", func(_ *container.Config, h *container.HostConfig) { h.MaskedPaths = []string{} }, "masked"},
		{"sysctls", func(_ *container.Config, h *container.HostConfig) {
			h.Sysctls = map[string]string{"net.ipv4.ip_forward": "1"}
		}, "Sysctls"},
		{"group add", func(_ *container.Config, h *container.HostConfig) { h.GroupAdd = []string{"999"} }, "GroupAdd"},
		{"annotations", func(_ *container.Config, h *container.HostConfig) {
			h.Annotations = map[string]string{"run.oci.x": "y"}
		}, "Annotations"},
		{"no memory limit", func(_ *container.Config, h *container.HostConfig) { h.Memory = 0 }, "memory limit"},
		{"no pids limit", func(_ *container.Config, h *container.HostConfig) { h.PidsLimit = nil }, "pids limit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := managerSpec("none", "superbot_sandbox")
			tc.mutate(opts.Config, opts.HostConfig)
			_, err := pol.checkCreate(createBody(t, opts))
			if err == nil {
				t.Fatal("create request was allowed")
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("reason %q does not mention %q", err.Error(), tc.reason)
			}
		})
	}
}

func TestCreateRejectsMalformedBodies(t *testing.T) {
	pol := newPolicy(testConfig("/var/run/docker.sock"))
	good := string(createBody(t, managerSpec("none", "superbot_sandbox")))
	for name, body := range map[string]string{
		"not json":        "{",
		"trailing data":   good + `{"HostConfig":{"Privileged":true}}`,
		"no config":       `{"HostConfig":{}}`,
		"no host config":  `{"Image":"superbot/computer:latest","User":"1000","Labels":{"superbot.bot_id":"x"}}`,
		"wrong type":      `{"Image":["superbot/computer:latest"]}`,
		"endpoint config": strings.TrimSuffix(good, "}") + `,"NetworkingConfig":{"EndpointsConfig":{"superbot_default":{}}}}`,
		"lowercase key":   strings.Replace(good, `"Privileged":false`, `"privileged":true`, 1),
	} {
		if _, err := pol.checkCreate([]byte(body)); err == nil {
			t.Errorf("%s: body was allowed", name)
		}
	}
}

func TestCreateDropsUnknownFields(t *testing.T) {
	pol := newPolicy(testConfig("/var/run/docker.sock"))
	good := string(createBody(t, managerSpec("none", "superbot_sandbox")))
	body := strings.Replace(good, `"HostConfig":{`, `"FutureTopLevel":1,"HostConfig":{"FutureEscape":true,`, 1)
	out, err := pol.checkCreate([]byte(body))
	if err != nil {
		t.Fatalf("unknown zero-impact fields must not cause rejection: %v", err)
	}
	if strings.Contains(string(out), "Future") {
		t.Fatalf("unknown fields must not be forwarded: %s", out)
	}
}

func TestCheckQueryCreateName(t *testing.T) {
	rt := &route{kind: routeContainerCreate}
	for _, query := range []string{"", "name=evil", "name=superbot-bot-", "name=/superbot-bot-abc", "name=superbot-bot-abc&name=evil", "name=superbot-botx"} {
		if _, err := checkQuery(rt, query); err == nil {
			t.Errorf("query %q was allowed", query)
		}
	}
	if q, err := checkQuery(rt, "name=superbot-bot-0d4c7a529f3e"); err != nil || q != "name=superbot-bot-0d4c7a529f3e" {
		t.Errorf("got %q, %v", q, err)
	}
}

func TestExecCreateChecks(t *testing.T) {
	for name, body := range map[string]string{
		"manager exec": `{"AttachStdout":true,"AttachStderr":true,"Cmd":["setsid","-w","sh","-c","x"],"WorkingDir":"/workspace","Env":["SUPERBOT_EXEC_MARK=1"]}`,
		"kill exec":    `{"Cmd":["sh","-c","kill","sh","SUPERBOT_EXEC_MARK=1"]}`,
		"non-root":     `{"User":"1000:1000","Cmd":["id"]}`,
	} {
		if _, err := checkExecCreate([]byte(body)); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
	for name, body := range map[string]string{
		"privileged": `{"Privileged":true,"Cmd":["id"]}`,
		"root":       `{"User":"root","Cmd":["id"]}`,
		"uid 0":      `{"User":"0","Cmd":["id"]}`,
		"gid 0":      `{"User":"1000:0","Cmd":["id"]}`,
		"padded":     `{"User":" 0","Cmd":["id"]}`,
		"not json":   `Cmd=id`,
	} {
		if _, err := checkExecCreate([]byte(body)); err == nil {
			t.Errorf("%s was allowed", name)
		}
	}
}

func TestIsRootID(t *testing.T) {
	for id, want := range map[string]bool{"root": true, "0": true, "000": true, "1000": false, "10": false, "": false, "nobody": false} {
		if got := isRootID(id); got != want {
			t.Errorf("isRootID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestConfigValidation(t *testing.T) {
	good := testConfig("/var/run/docker.sock")
	if err := good.validate(); err != nil {
		t.Fatalf("good config refused: %v", err)
	}
	bad := map[string]func(*Config){
		"empty bind root":        func(c *Config) { c.BindRoot = "" },
		"relative bind root":     func(c *Config) { c.BindRoot = "data" },
		"slash bind root":        func(c *Config) { c.BindRoot = "/" },
		"unclean bind root":      func(c *Config) { c.BindRoot = "/srv/../srv/superbot" },
		"bind root holds socket": func(c *Config) { c.BindRoot = "/var/run" },
		"bind root holds /run":   func(c *Config) { c.BindRoot = "/run" },
		"host network":           func(c *Config) { c.Networks = []string{"host"} },
		"container network":      func(c *Config) { c.Networks = []string{"container:abc"} },
		"no images":              func(c *Config) { c.Images = nil },
		"no socket":              func(c *Config) { c.Socket = "" },
	}
	for name, mutate := range bad {
		cfg := testConfig("/var/run/docker.sock")
		mutate(&cfg)
		if err := cfg.validate(); err == nil {
			t.Errorf("%s: config accepted", name)
		}
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("SUPERBOT_DOCKERPROXY_BIND_ROOT", "")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("missing bind root must be an error")
	}
	t.Setenv("SUPERBOT_DOCKERPROXY_BIND_ROOT", "/srv/superbot/data/")
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Socket != DefaultSocket || cfg.Listen != DefaultListen || cfg.BindRoot != "/srv/superbot/data" {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if strings.Join(cfg.Images, ",") != "superbot/computer:latest,superbot/computer-desktop:latest" ||
		strings.Join(cfg.Networks, ",") != "superbot_sandbox" {
		t.Fatalf("default allowlists wrong: %+v", cfg)
	}
	t.Setenv("SUPERBOT_DOCKERPROXY_IMAGES", " a:1 , b:2 ,")
	t.Setenv("SUPERBOT_DOCKERPROXY_NETWORKS", "")
	cfg, err = ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Images, ",") != "a:1,b:2" || len(cfg.Networks) != 0 {
		t.Fatalf("env lists not parsed: %+v", cfg)
	}
}
