package dockerproxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

const (
	// namePrefix is the name every sandbox container carries; see
	// containerName in internal/sandbox/manager.go.
	namePrefix = "superbot-bot-"
	// botIDLabel marks sandbox containers; see Ensure in manager.go.
	botIDLabel = "superbot.bot_id"

	// minAPIMajor/minAPIMinor match the moby client's MinAPIVersion; older
	// versioned paths are refused so legacy daemon behaviour is unreachable.
	minAPIMajor = 1
	minAPIMinor = 40
)

type routeKind int

const (
	routePing routeKind = iota + 1
	routeContainerCreate
	routeContainerInspect
	routeContainerStart
	routeContainerStop
	routeContainerRestart
	routeContainerRemove
	routeExecCreate
	routeExecStart
	routeExecInspect
)

func (k routeKind) String() string {
	switch k {
	case routePing:
		return "ping"
	case routeContainerCreate:
		return "container create"
	case routeContainerInspect:
		return "container inspect"
	case routeContainerStart:
		return "container start"
	case routeContainerStop:
		return "container stop"
	case routeContainerRestart:
		return "container restart"
	case routeContainerRemove:
		return "container remove"
	case routeExecCreate:
		return "exec create"
	case routeExecStart:
		return "exec start"
	case routeExecInspect:
		return "exec inspect"
	default:
		return "unknown"
	}
}

// allowedQuery lists the query parameters each endpoint accepts; anything
// else is refused. Endpoints not listed accept none.
var allowedQuery = map[routeKind][]string{
	routeContainerCreate:  {"name", "platform"},
	routeContainerInspect: {"size"},
	routeContainerStop:    {"t", "signal"},
	routeContainerRestart: {"t", "signal"},
	routeContainerRemove:  {"force", "v"},
}

// route is a request that matched the allowlist. upstreamPath and
// upstreamQuery are rebuilt from the parsed request, so only what was
// checked is forwarded.
type route struct {
	kind          routeKind
	version       string // "v1.47", or "" for an unversioned path
	ref           string // container name/id or exec id from the path
	upstreamPath  string
	upstreamQuery string
}

var (
	versionRE      = regexp.MustCompile(`^v([0-9]{1,3})\.([0-9]{1,4})$`)
	containerRefRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`)
	execIDRE       = regexp.MustCompile(`^[a-zA-Z0-9]{1,128}$`)
	fullIDRE       = regexp.MustCompile(`^[a-f0-9]{64}$`)
	createNameRE   = regexp.MustCompile(`^` + regexp.QuoteMeta(namePrefix) + `[a-zA-Z0-9_.-]{1,200}$`)
)

// denial is a request refused by policy. It is reported to the client with
// status and message, and logged.
type denial struct {
	status int
	reason string
}

func (d *denial) Error() string { return d.reason }

func deny(format string, args ...any) error {
	return &denial{status: http.StatusForbidden, reason: fmt.Sprintf(format, args...)}
}

// parseRoute matches method and path against the allowlist.
func parseRoute(method string, u *url.URL) (*route, error) {
	if u.RawPath != "" {
		return nil, deny("escaped characters in the path are not allowed")
	}
	p := u.Path
	if !strings.HasPrefix(p, "/") {
		return nil, deny("path must be absolute")
	}
	segs := strings.Split(p[1:], "/")
	rt := &route{}
	if m := versionRE.FindStringSubmatch(segs[0]); m != nil {
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		if major < minAPIMajor || (major == minAPIMajor && minor < minAPIMinor) {
			return nil, deny("API version %s is too old", segs[0])
		}
		rt.version = segs[0]
		segs = segs[1:]
	}
	for _, seg := range segs {
		if seg == "" || seg == "." || seg == ".." {
			return nil, deny("endpoint %s %s is not allowed", method, p)
		}
	}
	switch {
	case len(segs) == 1 && segs[0] == "_ping" && (method == http.MethodGet || method == http.MethodHead):
		rt.kind = routePing
	case len(segs) == 2 && segs[0] == "containers" && segs[1] == "create" && method == http.MethodPost:
		rt.kind = routeContainerCreate
	case (len(segs) == 2 || len(segs) == 3) && segs[0] == "containers":
		action := ""
		if len(segs) == 3 {
			action = segs[2]
		}
		switch {
		case action == "json" && method == http.MethodGet:
			rt.kind = routeContainerInspect
		case action == "start" && method == http.MethodPost:
			rt.kind = routeContainerStart
		case action == "stop" && method == http.MethodPost:
			rt.kind = routeContainerStop
		case action == "restart" && method == http.MethodPost:
			rt.kind = routeContainerRestart
		case action == "exec" && method == http.MethodPost:
			rt.kind = routeExecCreate
		case len(segs) == 2 && method == http.MethodDelete:
			rt.kind = routeContainerRemove
		}
		if rt.kind != 0 {
			if !containerRefRE.MatchString(segs[1]) {
				return nil, deny("invalid container reference")
			}
			rt.ref = segs[1]
		}
	case len(segs) == 3 && segs[0] == "exec":
		switch {
		case segs[2] == "start" && method == http.MethodPost:
			rt.kind = routeExecStart
		case segs[2] == "json" && method == http.MethodGet:
			rt.kind = routeExecInspect
		}
		if rt.kind != 0 {
			if !execIDRE.MatchString(segs[1]) {
				return nil, deny("invalid exec id")
			}
			rt.ref = segs[1]
		}
	}
	if rt.kind == 0 {
		return nil, deny("endpoint %s %s is not allowed", method, p)
	}
	return rt, nil
}

// setUpstream builds the forwarded path, substituting target for the
// container or exec reference.
func (rt *route) setUpstream(target string) {
	var b strings.Builder
	if rt.version != "" {
		b.WriteString("/" + rt.version)
	}
	switch rt.kind {
	case routePing:
		b.WriteString("/_ping")
	case routeContainerCreate:
		b.WriteString("/containers/create")
	case routeContainerInspect:
		b.WriteString("/containers/" + target + "/json")
	case routeContainerStart:
		b.WriteString("/containers/" + target + "/start")
	case routeContainerStop:
		b.WriteString("/containers/" + target + "/stop")
	case routeContainerRestart:
		b.WriteString("/containers/" + target + "/restart")
	case routeContainerRemove:
		b.WriteString("/containers/" + target)
	case routeExecCreate:
		b.WriteString("/containers/" + target + "/exec")
	case routeExecStart:
		b.WriteString("/exec/" + target + "/start")
	case routeExecInspect:
		b.WriteString("/exec/" + target + "/json")
	}
	rt.upstreamPath = b.String()
}

// checkQuery validates the query string and returns its canonical encoding.
func checkQuery(rt *route, rawQuery string) (string, error) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", deny("malformed query string")
	}
	allowed := allowedQuery[rt.kind]
	for key, vals := range values {
		ok := false
		for _, a := range allowed {
			if key == a {
				ok = true
				break
			}
		}
		if !ok {
			return "", deny("query parameter %q is not allowed on %s", key, rt.kind)
		}
		if len(vals) != 1 {
			return "", deny("query parameter %q must appear once", key)
		}
	}
	if rt.kind == routeContainerCreate {
		name := values.Get("name")
		if !createNameRE.MatchString(name) {
			return "", deny("container name %q is not allowed: it must start with %s", name, namePrefix)
		}
	}
	return values.Encode(), nil
}

// decodeJSON decodes exactly one JSON value from body.
func decodeJSON(body []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(v); err != nil {
		return deny("request body is not valid JSON for this endpoint")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return deny("unexpected data after the JSON body")
	}
	return nil
}

// policy holds the create-request allowlists.
type policy struct {
	images   map[string]bool
	networks map[string]bool
	bindRoot string
	// verifyPaths is set when the bind root is visible to the proxy, so bind
	// sources can be checked on disk and created on request.
	verifyPaths bool
}

func newPolicy(cfg Config) *policy {
	p := &policy{images: map[string]bool{}, networks: map[string]bool{}, bindRoot: cfg.BindRoot}
	if info, err := os.Stat(cfg.BindRoot); err == nil && info.IsDir() {
		p.verifyPaths = true
	}
	for _, image := range cfg.Images {
		p.images[strings.TrimSpace(image)] = true
	}
	for _, name := range cfg.Networks {
		p.networks[strings.TrimSpace(name)] = true
	}
	return p
}

// checkCreate validates a POST /containers/create body and returns the
// body to forward: the validated request re-encoded, so fields this proxy
// does not know about never reach the daemon. Messages never quote Env or
// Cmd values.
func (p *policy) checkCreate(body []byte) ([]byte, error) {
	var req container.CreateRequest
	if err := decodeJSON(body, &req); err != nil {
		return nil, err
	}
	cfg := req.Config
	if cfg == nil {
		return nil, deny("container config is required")
	}
	if !p.images[cfg.Image] {
		return nil, deny("image %q is not allowed", cfg.Image)
	}
	if cfg.Labels[botIDLabel] == "" {
		return nil, deny("label %s is required", botIDLabel)
	}
	if err := checkUser(cfg.User, false); err != nil {
		return nil, err
	}
	if len(cfg.Volumes) > 0 {
		return nil, deny("anonymous volumes are not allowed")
	}
	if req.HostConfig == nil {
		return nil, deny("HostConfig is required")
	}
	if err := p.checkHostConfig(req.HostConfig); err != nil {
		return nil, err
	}
	if req.NetworkingConfig != nil && len(req.NetworkingConfig.EndpointsConfig) > 0 {
		return nil, deny("NetworkingConfig endpoints are not allowed")
	}
	out, err := json.Marshal(req)
	if err != nil {
		return nil, deny("cannot re-encode the create request")
	}
	return out, nil
}

func (p *policy) checkHostConfig(hc *container.HostConfig) error {
	if hc.Privileged {
		return deny("privileged containers are not allowed")
	}
	if len(hc.CapAdd) > 0 {
		return deny("CapAdd is not allowed")
	}
	if !containsFold(hc.CapDrop, "ALL") {
		return deny("CapDrop must contain ALL")
	}
	if err := checkSecurityOpt(hc.SecurityOpt); err != nil {
		return err
	}
	if len(hc.Devices) > 0 || len(hc.DeviceRequests) > 0 || len(hc.DeviceCgroupRules) > 0 {
		return deny("devices are not allowed")
	}
	for field, mode := range map[string]string{
		"PidMode":      string(hc.PidMode),
		"IpcMode":      string(hc.IpcMode),
		"UTSMode":      string(hc.UTSMode),
		"UsernsMode":   string(hc.UsernsMode),
		"CgroupnsMode": string(hc.CgroupnsMode),
	} {
		if mode != "" && mode != "private" {
			return deny("%s %q is not allowed", field, mode)
		}
	}
	if hc.Cgroup != "" || hc.CgroupParent != "" {
		return deny("custom cgroups are not allowed")
	}
	if err := p.checkNetworkMode(string(hc.NetworkMode)); err != nil {
		return err
	}
	if hc.PublishAllPorts {
		return deny("PublishAllPorts is not allowed")
	}
	for port, bindings := range hc.PortBindings {
		for _, b := range bindings {
			if b.HostIP != netip.MustParseAddr("127.0.0.1") || b.HostPort != "" {
				return deny("port binding for %s must be 127.0.0.1 with a dynamic host port", port)
			}
		}
	}
	for _, bind := range hc.Binds {
		if err := p.checkBind(bind); err != nil {
			return err
		}
	}
	for _, m := range hc.Mounts {
		if err := p.checkMount(m); err != nil {
			return err
		}
	}
	switch {
	case hc.VolumeDriver != "":
		return deny("VolumeDriver is not allowed")
	case len(hc.VolumesFrom) > 0:
		return deny("VolumesFrom is not allowed")
	case len(hc.Links) > 0:
		return deny("Links are not allowed")
	case len(hc.GroupAdd) > 0:
		return deny("GroupAdd is not allowed")
	case len(hc.Sysctls) > 0:
		return deny("Sysctls are not allowed")
	case len(hc.Annotations) > 0:
		return deny("Annotations are not allowed")
	case len(hc.StorageOpt) > 0:
		return deny("StorageOpt is not allowed")
	case hc.Runtime != "":
		return deny("custom runtimes are not allowed")
	case hc.LogConfig.Type != "" || len(hc.LogConfig.Config) > 0:
		return deny("custom log drivers are not allowed")
	case hc.Isolation != "" && hc.Isolation != container.IsolationDefault:
		return deny("Isolation %q is not allowed", hc.Isolation)
	case hc.MaskedPaths != nil || hc.ReadonlyPaths != nil:
		return deny("overriding masked or read-only paths is not allowed")
	case hc.OomScoreAdj != 0:
		return deny("OomScoreAdj is not allowed")
	case hc.OomKillDisable != nil && *hc.OomKillDisable:
		return deny("disabling the OOM killer is not allowed")
	case len(hc.Ulimits) > 0:
		return deny("Ulimits are not allowed")
	case hc.CPURealtimePeriod != 0 || hc.CPURealtimeRuntime != 0:
		return deny("realtime CPU scheduling is not allowed")
	case hc.Memory <= 0:
		return deny("a memory limit is required")
	case hc.PidsLimit == nil || *hc.PidsLimit <= 0:
		return deny("a pids limit is required")
	}
	return nil
}

func (p *policy) checkNetworkMode(mode string) error {
	switch {
	case mode == "none" || mode == "bridge":
		return nil
	case mode == "host":
		return deny("host networking is not allowed")
	case strings.HasPrefix(mode, "container:"):
		return deny("sharing another container's network is not allowed")
	case p.networks[mode]:
		return nil
	default:
		return deny("network mode %q is not allowed", mode)
	}
}

// checkSecurityOpt requires no-new-privileges and refuses every other
// option, since the rest (seccomp=, apparmor=, label=, systempaths=) can
// only loosen the default confinement.
func checkSecurityOpt(opts []string) error {
	found := false
	for _, opt := range opts {
		switch strings.ToLower(opt) {
		case "no-new-privileges", "no-new-privileges:true", "no-new-privileges=true":
			found = true
		default:
			return deny("security option %q is not allowed", opt)
		}
	}
	if !found {
		return deny("SecurityOpt must contain no-new-privileges")
	}
	return nil
}

// checkBind validates a "source:target[:options]" bind as the Linux daemon
// parses it.
func (p *policy) checkBind(bind string) error {
	parts := strings.Split(bind, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return deny("bind %q is malformed", bind)
	}
	if err := p.checkSource(parts[0]); err != nil {
		return err
	}
	if !strings.HasPrefix(parts[1], "/") {
		return deny("bind target %q must be absolute", parts[1])
	}
	if len(parts) == 3 {
		for _, opt := range strings.Split(parts[2], ",") {
			if opt != "ro" && opt != "rw" {
				return deny("bind option %q is not allowed", opt)
			}
		}
	}
	return nil
}

func (p *policy) checkMount(m mount.Mount) error {
	switch m.Type {
	case mount.TypeBind:
		if err := p.checkSource(m.Source); err != nil {
			return err
		}
		if !strings.HasPrefix(m.Target, "/") {
			return deny("mount target %q must be absolute", m.Target)
		}
		if m.BindOptions != nil && m.BindOptions.Propagation != "" {
			return deny("bind propagation is not allowed")
		}
		if m.VolumeOptions != nil || m.ImageOptions != nil {
			return deny("bind mounts must not carry volume or image options")
		}
		return nil
	case mount.TypeTmpfs:
		if m.Source != "" {
			return deny("tmpfs mounts must not have a source")
		}
		return nil
	default:
		return deny("mount type %q is not allowed", m.Type)
	}
}

// checkSource requires a clean absolute host path strictly under the bind
// root. Named volumes, relative paths and any ".." are refused.
func (p *policy) checkSource(src string) error {
	if !strings.HasPrefix(src, "/") {
		return deny("bind source %q must be an absolute host path; named volumes are not allowed", src)
	}
	if strings.ContainsAny(src, "\\\x00") || path.Clean(src) != src {
		return deny("bind source %q is not a clean path", src)
	}
	if src == "/" {
		return deny("mounting / is not allowed")
	}
	if path.Base(src) == "docker.sock" {
		return deny("mounting the Docker socket is not allowed")
	}
	if !strings.HasPrefix(src, p.bindRoot+"/") {
		return deny("bind source %q is outside %s", src, p.bindRoot)
	}
	if !p.allowedSource(src) {
		return deny("bind source %q is neither a bot workspace nor a team shared folder", src)
	}
	return p.checkNoLinks(src)
}

// checkUser refuses root (by name or uid 0, and gid 0). An empty user is
// accepted only for exec, which then runs as the container's user, itself
// checked at create.
func checkUser(user string, allowEmpty bool) error {
	if user == "" {
		if allowEmpty {
			return nil
		}
		return deny("User must be set to a non-root user")
	}
	if strings.TrimSpace(user) != user {
		return deny("User %q is not allowed", user)
	}
	uid, gid, hasGroup := strings.Cut(user, ":")
	if uid == "" || isRootID(uid) {
		return deny("running as root is not allowed")
	}
	if hasGroup && isRootID(gid) {
		return deny("the root group is not allowed")
	}
	return nil
}

func isRootID(id string) bool {
	if id == "root" {
		return true
	}
	if id == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return strings.Trim(id, "0") == ""
}

// checkExecCreate validates a POST /containers/{id}/exec body.
func checkExecCreate(body []byte) ([]byte, error) {
	var req container.ExecCreateRequest
	if err := decodeJSON(body, &req); err != nil {
		return nil, err
	}
	if req.Privileged {
		return nil, deny("privileged exec is not allowed")
	}
	if err := checkUser(req.User, true); err != nil {
		return nil, err
	}
	out, err := json.Marshal(req)
	if err != nil {
		return nil, deny("cannot re-encode the exec request")
	}
	return out, nil
}

// checkExecStart validates a POST /exec/{id}/start body. An empty body is
// passed through.
func checkExecStart(body []byte) ([]byte, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	var req container.ExecStartRequest
	if err := decodeJSON(body, &req); err != nil {
		return nil, err
	}
	out, err := json.Marshal(req)
	if err != nil {
		return nil, deny("cannot re-encode the exec start request")
	}
	return out, nil
}

func containsFold(list []string, want string) bool {
	for _, item := range list {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}
