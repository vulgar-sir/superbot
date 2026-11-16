// Package fetch retrieves web pages with SSRF protection and extracts the
// main article as Markdown.
package fetch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
)

const (
	maxBodyBytes   = 2 * 1024 * 1024
	defaultMaxChar = 12000
	maxMaxChar     = 40000
)

type Result struct {
	Title    string
	URL      string
	SiteName string
	Byline   string
	Content  string
}

type Fetcher struct {
	client *http.Client
	logger *slog.Logger
}

func New(logger *slog.Logger) *Fetcher {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DialContext:           safeDialContext(dialer),
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          20,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   25 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return validateURL(req.URL)
		},
	}
	return &Fetcher{client: client, logger: logger}
}

func (f *Fetcher) Fetch(ctx context.Context, rawURL string, maxChars int) (Result, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return Result{}, fmt.Errorf("invalid url: %w", err)
	}
	if err := validateURL(parsed); err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Result{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "superbot/0.1 (+self-hosted agent)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,application/json,application/xml;q=0.9,*/*;q=0.5")

	resp, err := f.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("fetch url: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("fetch returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return Result{}, fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxBodyBytes {
		body = body[:maxBodyBytes]
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	isHTML := strings.Contains(contentType, "text/html") ||
		strings.Contains(contentType, "application/xhtml") ||
		(contentType == "" && bytes.Contains(bytes.ToLower(body[:min(len(body), 512)]), []byte("<html")))
	if !isHTML && !isTextType(contentType) {
		return Result{}, fmt.Errorf("unsupported content type %q", contentType)
	}

	article, err := readability.FromReader(bytes.NewReader(body), parsed)
	if err == nil && article.Node != nil {
		markdown, convErr := htmltomarkdown.ConvertNode(article.Node)
		if convErr == nil && strings.TrimSpace(string(markdown)) != "" {
			return f.buildResult(article.Title(), parsed.String(), article.SiteName(), article.Byline(), string(markdown), maxChars), nil
		}
	}

	// Fallback: convert the whole document (or return text as-is).
	content := string(body)
	if isHTML {
		if markdown, convErr := htmltomarkdown.ConvertString(content); convErr == nil {
			content = markdown
		}
	}
	return f.buildResult("", parsed.String(), "", "", content, maxChars), nil
}

func (f *Fetcher) buildResult(title, rawURL, site, byline, content string, maxChars int) Result {
	limit := maxChars
	if limit <= 0 {
		limit = defaultMaxChar
	}
	if limit > maxMaxChar {
		limit = maxMaxChar
	}
	runes := []rune(strings.TrimSpace(content))
	truncated := false
	if len(runes) > limit {
		runes = runes[:limit]
		truncated = true
	}
	content = string(runes)
	if truncated {
		content += "\n\n[content truncated]"
	}
	return Result{Title: title, URL: rawURL, SiteName: site, Byline: byline, Content: content}
}

func isTextType(contentType string) bool {
	return strings.HasPrefix(contentType, "text/") ||
		strings.Contains(contentType, "application/json") ||
		strings.Contains(contentType, "application/xml") ||
		strings.Contains(contentType, "application/x-yaml")
}

func validateURL(parsed *url.URL) error {
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("only http and https URLs are allowed")
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("url has no host")
	}
	if parsed.User != nil {
		return fmt.Errorf("urls with userinfo are not allowed")
	}
	return nil
}

func safeDialContext(dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if ip := net.ParseIP(host); ip != nil {
			if err := checkIP(ip); err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve host: %w", err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("host has no addresses")
		}
		// If a hostname resolves to any blocked address, reject all of them to
		// avoid DNS-rebinding tricks.
		for _, resolved := range ips {
			if err := checkIP(resolved.IP); err != nil {
				return nil, err
			}
		}
		var lastErr error
		for _, resolved := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

var blockedNets = mustParseCIDRs(
	"100.64.0.0/10",   // carrier-grade NAT
	"192.0.0.0/24",    // IETF protocol assignments
	"192.0.2.0/24",    // documentation
	"198.51.100.0/24", // documentation
	"203.0.113.0/24",  // documentation
)

func checkIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("invalid ip address")
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return fmt.Errorf("access to private or loopback addresses is blocked")
	}
	for _, network := range blockedNets {
		if network.Contains(ip) {
			return fmt.Errorf("access to reserved addresses is blocked")
		}
	}
	return nil
}

func mustParseCIDRs(values ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err == nil {
			out = append(out, network)
		}
	}
	return out
}
