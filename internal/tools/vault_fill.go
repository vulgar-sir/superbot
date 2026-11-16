package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"superbot/internal/sandbox"
	"superbot/internal/store"
	"superbot/internal/vault"
)

// vaultFillTool injects a Vault login into the Bot's browser through CDP. The
// credential never enters the model context or tool results; only field-level
// success flags are returned.
type vaultFillTool struct {
	manager *sandbox.Manager
	vault   *vault.Service
	db      *store.DB
	ownerID uuid.UUID
	botID   uuid.UUID
}

func NewVaultFillTool(manager *sandbox.Manager, vaultService *vault.Service, db *store.DB, ownerID, botID uuid.UUID) Tool {
	return &vaultFillTool{manager: manager, vault: vaultService, db: db, ownerID: ownerID, botID: botID}
}

func (t *vaultFillTool) Name() string { return "vault_fill_credentials" }
func (t *vaultFillTool) Description() string {
	return "Fill a login form in the Bot's browser using a Vault item. The password is injected server-side and is never returned to the model. Requires approval."
}
func (t *vaultFillTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"item":{"type":"string","description":"Vault item name"},"url":{"type":"string","description":"Login page URL; defaults to the item URL"},"username_selector":{"type":"string"},"password_selector":{"type":"string"},"submit":{"type":"boolean"}},"required":["item"]}`)
}

func (t *vaultFillTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Item             string `json:"item"`
		URL              string `json:"url"`
		UsernameSelector string `json:"username_selector"`
		PasswordSelector string `json:"password_selector"`
		Submit           bool   `json:"submit"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	itemName := strings.TrimSpace(args.Item)
	if itemName == "" {
		return Result{}, errors.New("item is required")
	}
	items, err := t.vault.List(ctx, t.ownerID)
	if err != nil {
		return Result{Content: "读取保险库失败", IsError: true}, nil
	}
	var target *store.VaultItem
	for i := range items {
		if strings.EqualFold(strings.TrimSpace(items[i].Name), itemName) {
			target = &items[i]
			break
		}
	}
	if target == nil {
		return Result{Content: "保险库中没有名为「" + itemName + "」的条目", IsError: true}, nil
	}
	item, password, _, err := t.vault.Reveal(ctx, t.ownerID, target.ID)
	if err != nil {
		return Result{Content: "读取凭据失败", IsError: true}, nil
	}
	pageURL := strings.TrimSpace(args.URL)
	if pageURL == "" {
		pageURL = strings.TrimSpace(item.Url)
	}
	parsed, err := url.Parse(pageURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Result{Content: "需要 http(s) 登录页地址", IsError: true}, nil
	}
	// Never inject credentials into a different site than the item was saved
	// for: the model could otherwise exfiltrate the password.
	if stored := strings.TrimSpace(item.Url); stored != "" {
		storedURL, parseErr := url.Parse(stored)
		if parseErr == nil && storedURL.Host != "" && !strings.EqualFold(storedURL.Host, parsed.Host) {
			return Result{Content: "目标站点与保险库条目保存的地址不一致，已拒绝填写", IsError: true}, nil
		}
	}
	if strings.TrimSpace(item.Username) == "" && password == "" {
		return Result{Content: "该保险库条目没有登录信息", IsError: true}, nil
	}

	info, err := t.manager.Ensure(ctx, t.ownerID, t.botID, "browser")
	if err != nil {
		return Result{Content: "启动浏览器失败: " + err.Error(), IsError: true}, nil
	}
	if strings.TrimSpace(info.CDPAddr) == "" {
		return Result{Content: "浏览器调试端口不可用", IsError: true}, nil
	}
	script := buildFillScript(args.UsernameSelector, args.PasswordSelector, args.Submit, item.Username, password)
	usernameFilled, passwordFilled, err := t.fill(ctx, info.CDPAddr, pageURL, script)
	if err != nil {
		return Result{Content: "填写失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(map[string]any{
		"url":             parsed.Scheme + "://" + parsed.Host + parsed.Path,
		"username_filled": usernameFilled,
		"password_filled": passwordFilled,
		"submitted":       args.Submit && (usernameFilled || passwordFilled),
	})
	return Result{Content: string(encoded)}, nil
}

func buildFillScript(usernameSelector, passwordSelector string, submit bool, username, password string) string {
	userJSON, _ := json.Marshal(username)
	passJSON, _ := json.Marshal(password)
	userSel, _ := json.Marshal(usernameSelector)
	passSel, _ := json.Marshal(passwordSelector)
	submitJSON, _ := json.Marshal(submit)
	return fmt.Sprintf(`(async () => {
  const delay = (ms) => new Promise((r) => setTimeout(r, ms));
  const pick = (sel, fallback) => {
    if (%s) { const el = document.querySelector(%s); if (el) return el; }
    for (const rule of fallback) { const el = document.querySelector(rule); if (el) return el; }
    return null;
  };
  let user = pick(%s, ['input[autocomplete="username"]','input[type="email"]','input[name*="user" i]','input[name*="email" i]','input[type="text"]']);
  let pass = pick(%s, ['input[type="password"]','input[autocomplete="current-password"]']);
  if (!pass) {
    const inputs = Array.from(document.querySelectorAll('input'));
    pass = inputs.find((el) => el.type === 'password') || null;
  }
  if (!user) {
    const inputs = Array.from(document.querySelectorAll('input')).filter((el) => el.type !== 'password' && el.type !== 'hidden');
    user = inputs[0] || null;
  }
  const set = (el, value) => {
    if (!el || value === undefined || value === null || value === '') return false;
    const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    const setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
    setter.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
    return true;
  };
  const userFilled = set(user, %s);
  const passFilled = set(pass, %s);
  if (%s && (userFilled || passFilled)) {
    const form = (pass && pass.form) || (user && user.form);
    await delay(150);
    if (form) { form.requestSubmit ? form.requestSubmit() : form.submit(); }
  }
  return { username: userFilled, password: passFilled };
})()`, userSel, userSel, userSel, passSel, userJSON, passJSON, submitJSON)
}

func (t *vaultFillTool) fill(ctx context.Context, cdpAddr, pageURL, script string) (bool, bool, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+cdpAddr+"/json/list", nil)
	if err != nil {
		return false, false, err
	}
	// Chrome rejects HTTP requests whose Host is not localhost/IP; dial the
	// container alias but present a loopback Host header.
	_, port, splitErr := strings.Cut(cdpAddr, ":")
	if splitErr && port != "" {
		request.Host = "127.0.0.1:" + port
	}
	response, err := client.Do(request)
	if err != nil {
		return false, false, err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	var targets []struct {
		Type         string `json:"type"`
		WebSocketURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(raw, &targets); err != nil {
		return false, false, err
	}
	wsURL := ""
	for _, target := range targets {
		if target.Type == "page" && target.WebSocketURL != "" {
			wsURL = target.WebSocketURL
			break
		}
	}
	if wsURL == "" {
		return false, false, errors.New("no browser page target")
	}
	// Chrome rejects non-IP Host headers. Resolve the container alias to an IP
	// and dial the WebSocket through that address.
	wsHost := cdpAddr
	if host, port, found := strings.Cut(cdpAddr, ":"); found {
		if addresses, lookupErr := net.LookupHost(host); lookupErr == nil && len(addresses) > 0 {
			wsHost = net.JoinHostPort(addresses[0], port)
		}
	}
	if parsedWS, parseErr := url.Parse(wsURL); parseErr == nil {
		parsedWS.Host = wsHost
		wsURL = parsedWS.String()
	}
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return false, false, err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(4 << 20)

	send := func(id int, method string, params any) error {
		payload, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
		writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return conn.Write(writeCtx, websocket.MessageText, payload)
	}
	wait := func(id int) (map[string]any, error) {
		for {
			readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			_, data, err := conn.Read(readCtx)
			cancel()
			if err != nil {
				return nil, err
			}
			var message map[string]any
			if json.Unmarshal(data, &message) != nil {
				continue
			}
			if number, ok := message["id"].(float64); ok && int(number) == id {
				return message, nil
			}
		}
	}
	if err := send(1, "Page.navigate", map[string]any{"url": pageURL}); err != nil {
		return false, false, err
	}
	if _, err := wait(1); err != nil {
		return false, false, err
	}
	select {
	case <-time.After(1500 * time.Millisecond):
	case <-ctx.Done():
		return false, false, ctx.Err()
	}
	if err := send(2, "Runtime.evaluate", map[string]any{
		"expression": script, "awaitPromise": true, "returnByValue": true,
	}); err != nil {
		return false, false, err
	}
	message, err := wait(2)
	if err != nil {
		return false, false, err
	}
	result, _ := message["result"].(map[string]any)
	value, _ := result["result"].(map[string]any)
	remote, _ := value["value"].(map[string]any)
	username, _ := remote["username"].(bool)
	password, _ := remote["password"].(bool)
	return username, password, nil
}
