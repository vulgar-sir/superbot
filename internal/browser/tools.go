package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/google/uuid"

	"superbot/internal/tools"
)

type snapshotElement struct {
	Ref         int    `json:"ref"`
	Tag         string `json:"tag"`
	Type        string `json:"type,omitempty"`
	Text        string `json:"text,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Href        string `json:"href,omitempty"`
}

type snapshotResult struct {
	Title    string            `json:"title"`
	URL      string            `json:"url"`
	Text     string            `json:"text"`
	Elements []snapshotElement `json:"elements"`
}

func Tools(session *Session) []tools.Tool {
	return []tools.Tool{
		&openTool{session: session},
		&snapshotTool{session: session},
		&clickTool{session: session},
		&typeTool{session: session},
		&screenshotTool{session: session},
	}
}

type openTool struct{ session *Session }

func (t *openTool) Name() string { return "browser_open" }
func (t *openTool) Description() string {
	return "Open a URL in the bot's browser (inside the computer container). Use browser_snapshot to see the page and its interactive elements."
}
func (t *openTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`)
}

func (t *openTool) Execute(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if err := validateBrowserURL(args.URL); err != nil {
		return tools.Result{}, err
	}
	runCtx, cancel := t.session.runCtx(45 * time.Second)
	defer cancel()
	if err := chromedp.Do(runCtx,
		chromedp.Navigate(args.URL),
		chromedp.WaitReady(chromedp.CSS("body")),
		chromedp.Sleep(800*time.Millisecond),
	); err != nil {
		return tools.Result{}, fmt.Errorf("open url: %w", err)
	}
	return snapshotPage(runCtx, t.session)
}

type snapshotTool struct{ session *Session }

func (t *snapshotTool) Name() string { return "browser_snapshot" }
func (t *snapshotTool) Description() string {
	return "Return the current page title, URL, visible text and a numbered list of interactive elements (links, buttons, inputs). Use the numbers with browser_click / browser_type."
}
func (t *snapshotTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *snapshotTool) Execute(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	runCtx, cancel := t.session.runCtx(45 * time.Second)
	defer cancel()
	return snapshotPage(runCtx, t.session)
}

type clickTool struct{ session *Session }

func (t *clickTool) Name() string { return "browser_click" }
func (t *clickTool) Description() string {
	return "Click an interactive element by its ref number from browser_snapshot. The page may navigate; call browser_snapshot again afterwards."
}
func (t *clickTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"ref":{"type":"integer"}},"required":["ref"]}`)
}

func (t *clickTool) Execute(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	var args struct {
		Ref int `json:"ref"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Ref <= 0 {
		return tools.Result{}, fmt.Errorf("ref must be a positive number")
	}
	script := fmt.Sprintf(`(() => {
		const el = document.querySelector('[data-superbot-ref="%d"]');
		if (!el) return { ok: false, error: 'ref not found; call browser_snapshot again' };
		el.scrollIntoView({ block: 'center' });
		el.click();
		return { ok: true };
	})()`, args.Ref)
	runCtx, cancel := t.session.runCtx(45 * time.Second)
	defer cancel()
	result, err := chromedp.Run(runCtx, chromedp.Evaluate[map[string]any](script))
	if err != nil {
		return tools.Result{}, fmt.Errorf("click ref %d: %w", args.Ref, err)
	}
	if ok, _ := result["ok"].(bool); !ok {
		return tools.Result{}, fmt.Errorf("%v", result["error"])
	}
	_ = chromedp.Do(runCtx, chromedp.Sleep(800*time.Millisecond))
	title, _ := chromedp.Run(runCtx, chromedp.Title())
	location, _ := chromedp.Run(runCtx, chromedp.Location())
	return tools.Result{Content: fmt.Sprintf("clicked ref %d; title=%q url=%s", args.Ref, title, location)}, nil
}

type typeTool struct{ session *Session }

func (t *typeTool) Name() string { return "browser_type" }
func (t *typeTool) Description() string {
	return "Type text into an input, textarea or contenteditable element by its ref number. This focuses the element and dispatches input/change events."
}
func (t *typeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"ref":{"type":"integer"},"text":{"type":"string"},"press_enter":{"type":"boolean","description":"press Enter after typing"}},"required":["ref","text"]}`)
}

func (t *typeTool) Execute(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	var args struct {
		Ref        int    `json:"ref"`
		Text       string `json:"text"`
		PressEnter bool   `json:"press_enter"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Ref <= 0 {
		return tools.Result{}, fmt.Errorf("ref must be a positive number")
	}
	encoded, _ := json.Marshal(args.Text)
	script := fmt.Sprintf(`(() => {
		const el = document.querySelector('[data-superbot-ref="%d"]');
		if (!el) return { ok: false, error: 'ref not found; call browser_snapshot again' };
		el.focus();
		const text = %s;
		if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
			const proto = el.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
			const setter = Object.getOwnPropertyDescriptor(proto, 'value')?.set;
			if (setter) setter.call(el, text); else el.value = text;
		} else {
			el.textContent = text;
		}
		el.dispatchEvent(new Event('input', { bubbles: true }));
		el.dispatchEvent(new Event('change', { bubbles: true }));
		return { ok: true };
	})()`, args.Ref, encoded)
	runCtx, cancel := t.session.runCtx(45 * time.Second)
	defer cancel()
	result, err := chromedp.Run(runCtx, chromedp.Evaluate[map[string]any](script))
	if err != nil {
		return tools.Result{}, fmt.Errorf("type ref %d: %w", args.Ref, err)
	}
	if ok, _ := result["ok"].(bool); !ok {
		return tools.Result{}, fmt.Errorf("%v", result["error"])
	}
	if args.PressEnter {
		if err := chromedp.Do(runCtx, chromedp.KeyEvent("\r")); err != nil {
			return tools.Result{Content: "typed, but pressing Enter failed: " + err.Error(), IsError: true}, nil
		}
		_ = chromedp.Do(runCtx, chromedp.Sleep(800*time.Millisecond))
	}
	return tools.Result{Content: fmt.Sprintf("typed into ref %d", args.Ref)}, nil
}

type screenshotTool struct{ session *Session }

func (t *screenshotTool) Name() string { return "browser_screenshot" }
func (t *screenshotTool) Description() string {
	return "Capture a PNG screenshot of the current browser page and save it in the bot workspace. The human can view it in the live computer panel."
}
func (t *screenshotTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *screenshotTool) Execute(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	runCtx, cancel := t.session.runCtx(45 * time.Second)
	defer cancel()
	data, err := chromedp.Run(runCtx, chromedp.CaptureScreenshot())
	if err != nil {
		return tools.Result{}, fmt.Errorf("screenshot: %w", err)
	}
	dir := filepath.Join(t.session.WorkspacePath(), "artifacts", "browser")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return tools.Result{}, err
	}
	filePath := filepath.Join(dir, uuid.NewString()+".png")
	if err := os.WriteFile(filePath, data, 0o666); err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Content: fmt.Sprintf("screenshot saved: %s (%d bytes)", filePath, len(data))}, nil
}

func snapshotPage(ctx context.Context, session *Session) (tools.Result, error) {
	const script = `(() => {
		const text = (document.body?.innerText || '').slice(0, 12000);
		const selector = 'a,button,input,textarea,select,[role="button"],[role="link"],[contenteditable="true"]';
		const elements = [];
		let ref = 1;
		for (const el of document.querySelectorAll(selector)) {
			const rect = el.getBoundingClientRect();
			const style = getComputedStyle(el);
			if (rect.width < 2 || rect.height < 2 || style.display === 'none' || style.visibility === 'hidden') continue;
			el.setAttribute('data-superbot-ref', String(ref));
			elements.push({
				ref,
				tag: el.tagName.toLowerCase(),
				type: el.getAttribute('type') || '',
				text: (el.innerText || el.value || el.getAttribute('aria-label') || '').trim().slice(0, 140),
				placeholder: el.getAttribute('placeholder') || '',
				href: el.getAttribute('href') || '',
			});
			ref++;
			if (ref > 100) break;
		}
		return { title: document.title || '', url: location.href, text, elements };
	})()`
	result, err := chromedp.Run(ctx, chromedp.Evaluate[snapshotResult](script))
	if err != nil {
		return tools.Result{}, fmt.Errorf("snapshot: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\nURL: %s\n", result.Title, result.URL)
	if strings.TrimSpace(result.Text) != "" {
		b.WriteString("\n--- visible text ---\n")
		b.WriteString(strings.TrimSpace(result.Text))
		b.WriteString("\n")
	}
	b.WriteString("\n--- interactive elements ---\n")
	if len(result.Elements) == 0 {
		b.WriteString("(none)\n")
	}
	for _, element := range result.Elements {
		fmt.Fprintf(&b, "[%d] %s", element.Ref, element.Tag)
		if element.Type != "" {
			fmt.Fprintf(&b, " type=%s", element.Type)
		}
		if element.Text != "" {
			fmt.Fprintf(&b, " %q", truncateText(element.Text, 140))
		}
		if element.Placeholder != "" {
			fmt.Fprintf(&b, " placeholder=%q", element.Placeholder)
		}
		if element.Href != "" {
			fmt.Fprintf(&b, " href=%s", element.Href)
		}
		b.WriteString("\n")
	}
	return tools.Result{Content: b.String()}, nil
}

func validateBrowserURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("only http and https URLs are allowed")
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("url has no host")
	}
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("localhost is blocked")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return fmt.Errorf("private or loopback addresses are blocked")
		}
	}
	return nil
}

func truncateText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

// LazyTools returns the browser tools without starting the computer: the
// first browser call opens the session (booting the computer if it is on
// standby). Every bot has a computer, and most turns never browse, so a turn
// must not boot one just to offer the tools.
func LazyTools(open func(ctx context.Context) (*Session, error)) []tools.Tool {
	described := Tools(nil) // Name, Description and Parameters do not touch the session
	lazy := make([]tools.Tool, len(described))
	for index, tool := range described {
		lazy[index] = &lazyTool{described: tool, index: index, open: open}
	}
	return lazy
}

type lazyTool struct {
	described tools.Tool
	index     int
	open      func(ctx context.Context) (*Session, error)
}

func (t *lazyTool) Name() string                { return t.described.Name() }
func (t *lazyTool) Description() string         { return t.described.Description() }
func (t *lazyTool) Parameters() json.RawMessage { return t.described.Parameters() }

func (t *lazyTool) Execute(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
	session, err := t.open(ctx)
	if err != nil {
		return tools.Result{}, fmt.Errorf("start the computer's browser: %w", err)
	}
	return Tools(session)[t.index].Execute(ctx, raw)
}
