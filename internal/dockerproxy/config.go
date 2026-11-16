// Package dockerproxy is an allowlisting reverse proxy in front of the Docker
// Engine socket. The worker reaches Docker only through it, so a compromised
// worker can manage bot sandbox containers but cannot create a privileged
// container, mount the host, or call any other Engine endpoint.
package dockerproxy

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
)

// Config configures the proxy.
type Config struct {
	// Socket is the Docker Engine unix socket requests are forwarded to.
	Socket string
	// Listen is the TCP address the proxy serves on.
	Listen string
	// Images are the only images containers may be created from.
	Images []string
	// Networks are the networks, besides none and bridge, a container may
	// use as its network mode.
	Networks []string
	// BindRoot is the host directory every bind mount source must lie
	// under. Paths are host paths as the Docker daemon sees them.
	BindRoot string
}

const (
	DefaultSocket = "/var/run/docker.sock"
	DefaultListen = "0.0.0.0:2375"
)

var (
	defaultImages   = []string{"superbot/computer:latest", "superbot/computer-desktop:latest"}
	defaultNetworks = []string{"superbot_sandbox"}
)

// ConfigFromEnv reads the configuration from SUPERBOT_DOCKERPROXY_SOCKET,
// SUPERBOT_DOCKERPROXY_LISTEN, SUPERBOT_DOCKERPROXY_IMAGES and
// SUPERBOT_DOCKERPROXY_NETWORKS (comma-separated) and the required
// SUPERBOT_DOCKERPROXY_BIND_ROOT.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Socket:   envOr("SUPERBOT_DOCKERPROXY_SOCKET", DefaultSocket),
		Listen:   envOr("SUPERBOT_DOCKERPROXY_LISTEN", DefaultListen),
		Images:   envList("SUPERBOT_DOCKERPROXY_IMAGES", defaultImages),
		Networks: envList("SUPERBOT_DOCKERPROXY_NETWORKS", defaultNetworks),
		BindRoot: strings.TrimSpace(os.Getenv("SUPERBOT_DOCKERPROXY_BIND_ROOT")),
	}
	if cfg.BindRoot == "" {
		return Config{}, errors.New("SUPERBOT_DOCKERPROXY_BIND_ROOT is required (host path of the data directory, e.g. /srv/superbot/data)")
	}
	if strings.HasPrefix(cfg.BindRoot, "/") {
		cfg.BindRoot = path.Clean(cfg.BindRoot)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if strings.TrimSpace(c.Socket) == "" {
		return errors.New("docker proxy: socket path is required")
	}
	if strings.TrimSpace(c.Listen) == "" {
		return errors.New("docker proxy: listen address is required")
	}
	if len(c.Images) == 0 {
		return errors.New("docker proxy: at least one allowed image is required")
	}
	for _, image := range c.Images {
		if strings.TrimSpace(image) == "" {
			return errors.New("docker proxy: allowed images must not be empty")
		}
	}
	for _, name := range c.Networks {
		lower := strings.ToLower(strings.TrimSpace(name))
		if lower == "" || lower == "host" || strings.HasPrefix(lower, "container:") {
			return fmt.Errorf("docker proxy: network %q cannot be allowed", name)
		}
	}
	root := c.BindRoot
	if !strings.HasPrefix(root, "/") || path.Clean(root) != root {
		return fmt.Errorf("docker proxy: bind root %q must be a clean absolute path", root)
	}
	if root == "/" {
		return errors.New("docker proxy: bind root must not be /")
	}
	for _, sock := range []string{c.Socket, "/var/run/docker.sock", "/run/docker.sock"} {
		if sock == root || strings.HasPrefix(sock, root+"/") {
			return fmt.Errorf("docker proxy: bind root %q contains the Docker socket", root)
		}
	}
	return nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// envList parses a comma-separated variable. An unset variable yields the
// fallback; a set but empty one yields an empty list.
func envList(key string, fallback []string) []string {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return append([]string(nil), fallback...)
	}
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if value := strings.TrimSpace(item); value != "" {
			out = append(out, value)
		}
	}
	return out
}
