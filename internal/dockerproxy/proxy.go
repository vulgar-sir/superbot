package dockerproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxCreateBody     = 1 << 20
	maxExecCreateBody = 16 << 20 // exec args can carry whole scripts
	maxExecStartBody  = 64 << 10
	maxIDResponse     = 64 << 10

	containerCacheTTL = 30 * time.Second
	execTTL           = time.Hour
	maxContainerCache = 4096
	maxTrackedExecs   = 16384
	resolveTimeout    = 10 * time.Second
)

// Proxy is an http.Handler that forwards allowlisted Docker Engine API
// requests to the daemon's unix socket and refuses everything else.
type Proxy struct {
	cfg      Config
	logger   *slog.Logger
	policy   *policy
	rp       *httputil.ReverseProxy
	upstream *http.Client
	now      func() time.Time

	mu         sync.Mutex
	containers map[string]cachedContainer // path reference → resolution
	execs      map[string]time.Time       // exec id → expiry
}

type cachedContainer struct {
	id      string
	expires time.Time
}

type routeKey struct{}

// New validates cfg and returns a proxy forwarding to cfg.Socket.
func New(cfg Config, logger *slog.Logger) (*Proxy, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", cfg.Socket)
		},
		DisableCompression: true,
		MaxIdleConns:       16,
		IdleConnTimeout:    90 * time.Second,
	}
	p := &Proxy{
		cfg:        cfg,
		logger:     logger,
		policy:     newPolicy(cfg),
		upstream:   &http.Client{Transport: transport, Timeout: resolveTimeout},
		now:        time.Now,
		containers: map[string]cachedContainer{},
		execs:      map[string]time.Time{},
	}
	p.rp = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			rt := pr.In.Context().Value(routeKey{}).(*route)
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "docker"
			pr.Out.URL.Path = rt.upstreamPath
			pr.Out.URL.RawPath = ""
			pr.Out.URL.RawQuery = rt.upstreamQuery
			pr.Out.Host = "docker"
		},
		Transport: transport,
		// Docker streams (attach, exec output) must reach the client as
		// soon as the daemon writes them.
		FlushInterval:  -1,
		ModifyResponse: p.modifyResponse,
		ErrorHandler:   p.proxyError,
		ErrorLog:       slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
	return p, nil
}

// ServeHTTP checks the request against the allowlist and forwards it.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == dirsPath {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "superbot docker proxy: POST only")
			return
		}
		p.handleDirs(w, r)
		return
	}
	rt, err := p.authorize(r)
	if err != nil {
		var d *denial
		if errors.As(err, &d) {
			p.logger.Warn("docker proxy denied request", "method", r.Method, "path", r.URL.Path, "reason", d.reason)
			writeError(w, d.status, "superbot docker proxy: "+d.reason)
			return
		}
		p.logger.Error("docker proxy failed to authorize request", "method", r.Method, "path", r.URL.Path, "error", err)
		writeError(w, http.StatusBadGateway, "superbot docker proxy: "+err.Error())
		return
	}
	p.logger.Debug("docker proxy request", "method", r.Method, "path", r.URL.Path, "endpoint", rt.kind.String())
	p.rp.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), routeKey{}, rt)))
}

// authorize returns the route for an allowed request, having replaced its
// body with the validated one. Policy refusals are *denial errors.
func (p *Proxy) authorize(r *http.Request) (*route, error) {
	rt, err := parseRoute(r.Method, r.URL)
	if err != nil {
		return nil, err
	}
	if rt.upstreamQuery, err = checkQuery(rt, r.URL.RawQuery); err != nil {
		return nil, err
	}
	// Only exec start may switch protocols, and only to Docker's raw stream.
	if rt.kind == routeExecStart {
		if up := r.Header.Get("Upgrade"); up != "" && !strings.EqualFold(up, "tcp") {
			return nil, deny("upgrade to %q is not allowed", up)
		}
	} else {
		r.Header.Del("Upgrade")
	}

	target := rt.ref
	switch rt.kind {
	case routeContainerInspect, routeContainerStart, routeContainerStop,
		routeContainerRestart, routeContainerRemove, routeExecCreate:
		if target, err = p.resolveContainer(r.Context(), rt.ref); err != nil {
			return nil, err
		}
	case routeExecStart, routeExecInspect:
		if !p.execKnown(rt.ref) {
			return nil, deny("exec %s was not created through this proxy", rt.ref)
		}
	}
	rt.setUpstream(target)

	switch rt.kind {
	case routeContainerCreate:
		return rt, p.replaceBody(r, maxCreateBody, p.policy.checkCreate)
	case routeExecCreate:
		return rt, p.replaceBody(r, maxExecCreateBody, checkExecCreate)
	case routeExecStart:
		return rt, p.replaceBody(r, maxExecStartBody, checkExecStart)
	default:
		if r.ContentLength != 0 {
			return nil, deny("a request body is not allowed on %s", rt.kind)
		}
		return rt, nil
	}
}

// replaceBody reads the body (up to limit), validates it with check, and
// installs the body check returns in its place.
func (p *Proxy) replaceBody(r *http.Request, limit int64, check func([]byte) ([]byte, error)) error {
	var body []byte
	if r.Body != nil {
		data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
		_ = r.Body.Close()
		if err != nil {
			return &denial{status: http.StatusBadRequest, reason: "cannot read the request body"}
		}
		if int64(len(data)) > limit {
			return &denial{status: http.StatusRequestEntityTooLarge, reason: fmt.Sprintf("request body exceeds %d bytes", limit)}
		}
		body = data
	}
	out, err := check(body)
	if err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(out))
	r.ContentLength = int64(len(out))
	r.TransferEncoding = nil
	r.Header.Del("Content-Length")
	r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(out)), nil }
	if len(out) > 0 {
		r.Header.Set("Content-Type", "application/json")
	}
	return nil
}

// resolveContainer returns the reference to forward for a container in a
// request path. Sandbox names are forwarded as they are: Docker resolves a
// non-hex string only by name. Anything else is looked up and must be a
// sandbox container (by name or label); it is then forwarded by full id,
// so it cannot later resolve to a different container.
func (p *Proxy) resolveContainer(ctx context.Context, ref string) (string, error) {
	if strings.HasPrefix(ref, namePrefix) {
		return ref, nil
	}
	if id, ok := p.cachedContainer(ref); ok {
		return id, nil
	}
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/"+ref+"/json", nil)
	if err != nil {
		return "", deny("invalid container reference")
	}
	res, err := p.upstream.Do(req)
	if err != nil {
		return "", fmt.Errorf("inspect container: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", deny("container %s is not a superbot sandbox container", ref)
	}
	var inspected struct {
		ID     string `json:"Id"`
		Name   string `json:"Name"`
		Config *struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&inspected); err != nil {
		return "", fmt.Errorf("decode container inspect: %w", err)
	}
	sandbox := strings.HasPrefix(strings.TrimPrefix(inspected.Name, "/"), namePrefix) ||
		(inspected.Config != nil && inspected.Config.Labels[botIDLabel] != "")
	if !sandbox || !fullIDRE.MatchString(inspected.ID) {
		return "", deny("container %s is not a superbot sandbox container", ref)
	}
	p.cacheContainer(ref, inspected.ID)
	return inspected.ID, nil
}

func (p *Proxy) cachedContainer(ref string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.containers[ref]
	if !ok || !p.now().Before(entry.expires) {
		delete(p.containers, ref)
		return "", false
	}
	return entry.id, true
}

func (p *Proxy) cacheContainer(ref, id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if len(p.containers) >= maxContainerCache {
		for key, entry := range p.containers {
			if !now.Before(entry.expires) {
				delete(p.containers, key)
			}
		}
		if len(p.containers) >= maxContainerCache {
			clear(p.containers)
		}
	}
	p.containers[ref] = cachedContainer{id: id, expires: now.Add(containerCacheTTL)}
}

func (p *Proxy) trackExec(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if len(p.execs) >= maxTrackedExecs {
		oldestID, oldest := "", time.Time{}
		for key, expires := range p.execs {
			if !now.Before(expires) {
				delete(p.execs, key)
			} else if oldestID == "" || expires.Before(oldest) {
				oldestID, oldest = key, expires
			}
		}
		if len(p.execs) >= maxTrackedExecs {
			delete(p.execs, oldestID)
		}
	}
	p.execs[id] = now.Add(execTTL)
}

func (p *Proxy) execKnown(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	expires, ok := p.execs[id]
	if ok && !p.now().Before(expires) {
		delete(p.execs, id)
		return false
	}
	return ok
}

// modifyResponse records the id of every exec created through the proxy so
// that only those can be started and inspected.
func (p *Proxy) modifyResponse(res *http.Response) error {
	rt, _ := res.Request.Context().Value(routeKey{}).(*route)
	if rt == nil || rt.kind != routeExecCreate || res.StatusCode < 200 || res.StatusCode > 299 {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxIDResponse))
	_ = res.Body.Close()
	if err != nil {
		return fmt.Errorf("read exec create response: %w", err)
	}
	var created struct {
		ID string `json:"Id"`
	}
	if json.Unmarshal(data, &created) == nil && execIDRE.MatchString(created.ID) {
		p.trackExec(created.ID)
	} else {
		p.logger.Warn("docker proxy could not record exec id", "path", res.Request.URL.Path)
	}
	res.Body = io.NopCloser(bytes.NewReader(data))
	res.ContentLength = int64(len(data))
	res.Header.Set("Content-Length", strconv.Itoa(len(data)))
	res.Header.Del("Transfer-Encoding")
	res.TransferEncoding = nil
	return nil
}

func (p *Proxy) proxyError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) {
		p.logger.Debug("docker proxy request canceled", "method", r.Method, "path", r.URL.Path)
	} else {
		p.logger.Warn("docker proxy upstream error", "method", r.Method, "path", r.URL.Path, "error", err)
	}
	writeError(w, http.StatusBadGateway, "superbot docker proxy: Docker Engine is unavailable")
}

func writeError(w http.ResponseWriter, status int, message string) {
	body, _ := json.Marshal(map[string]string{"message": message})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// ListenAndServe serves the proxy on cfg.Listen until ctx is cancelled.
func ListenAndServe(ctx context.Context, cfg Config, logger *slog.Logger) error {
	p, err := New(cfg, logger)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("docker proxy: listen: %w", err)
	}
	return p.serve(ctx, ln)
}

func (p *Proxy) serve(ctx context.Context, ln net.Listener) error {
	server := &http.Server{
		Handler:           p,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(p.logger.Handler(), slog.LevelWarn),
	}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(ln) }()
	p.logger.Info("docker proxy listening", "addr", ln.Addr().String(), "socket", p.cfg.Socket,
		"images", p.cfg.Images, "networks", p.cfg.Networks, "bind_root", p.cfg.BindRoot)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := server.Shutdown(shutdownCtx)
		<-errCh
		return err
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
