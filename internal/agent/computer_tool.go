package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/browser"
	"superbot/internal/store"
	"superbot/internal/tools"
)

// computerEnableTool lets a bot start its computer when a task needs one, or
// upgrade its browser computer to a desktop. New bots start without a
// computer to save resources; before this tool existed a bot asked to "open
// a site in the browser" had no browser tools, no network in its shell, and
// spent its turn probing the sandbox and inventing settings for the user.
type computerEnableTool struct {
	engine   *Engine
	bot      *store.Bot
	registry *tools.Registry
}

func (t *computerEnableTool) Name() string { return "computer_enable" }

func (t *computerEnableTool) Description() string {
	return "启用或升级你的电脑（可以联网，没有 root）。profile=browser：浏览器电脑，用于打开/操作网页、登录网站、截图网页，" +
		"启用后可以使用 browser_open / browser_snapshot / browser_click / browser_type / browser_screenshot；" +
		"profile=desktop：桌面电脑（XFCE 桌面 + 浏览器），用于安装和操作图形界面的 Linux 软件，启用后另有 desktop_* 工具（读屏、点击、输入、拖拽、启动程序）。" +
		"shell 也会切换到这台可以联网的电脑。只读地查资料不需要启用，直接用 fetch_url 或 web_search。"
}

func (t *computerEnableTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"profile":{"type":"string","enum":["browser","desktop"],"description":"browser（默认）或 desktop"},` +
		`"reason":{"type":"string","description":"为什么需要，一句话"}}}`)
}

func (t *computerEnableTool) Execute(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
	var args struct {
		Profile string `json:"profile"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &args)
	}
	want := strings.TrimSpace(args.Profile)
	if want == "" {
		want = "browser"
	}
	if want != "browser" && want != "desktop" {
		return tools.Result{Content: "profile 只能是 browser 或 desktop", IsError: true}, nil
	}
	e := t.engine
	if e.sandbox == nil {
		return tools.Result{Content: "这个部署没有启用电脑沙箱，无法启动电脑。请改用 fetch_url 查看网页内容，并如实告诉用户。", IsError: true}, nil
	}
	current := t.bot.ComputerProfile
	if current == want || (current == "desktop" && want == "browser") {
		if current == "desktop" {
			return tools.Result{Content: "桌面电脑已经启用（包含浏览器），直接使用 browser_* 和 desktop_* 工具。"}, nil
		}
		return tools.Result{Content: "浏览器电脑已经启用，直接使用 browser_* 工具。"}, nil
	}
	updated, err := e.db.SetBotComputerProfile(ctx, store.SetBotComputerProfileParams{
		ID: t.bot.ID, OwnerID: t.bot.OwnerID, ComputerProfile: want,
	})
	if err != nil {
		return tools.Result{Content: "启用电脑失败：" + err.Error(), IsError: true}, nil
	}
	*t.bot = updated
	// The open tab belongs to the container being replaced; a cached session
	// would answer for the old computer until it disappeared.
	e.dropBrowserSession(updated.ID)
	session, err := e.browserSession(ctx, updated)
	if err != nil {
		return tools.Result{Content: "电脑已切换为 " + want + " 模式，但没有启动成功：" + err.Error() + "。请如实告诉用户。", IsError: true}, nil
	}
	t.registerComputerTools(updated, session)
	e.logger.Info("bot enabled its computer", "bot_id", updated.ID, "from", current, "to", want)
	if want == "desktop" {
		return tools.Result{Content: "桌面电脑已启用（可以联网）。用 desktop_screenshot 或 desktop_read_screen 看屏幕（返回屏幕上的文字和可点击坐标），" +
			"用 desktop_click / desktop_type / desktop_key / desktop_drag 操作，用 desktop_launch 启动图形程序；" +
			"没有 root：装软件用 shell 里的 userapt install 包名、appimage-install、pip install、npm install -g。" +
			"浏览器工具 browser_* 照常可用。用户也能在右侧「Bot 的电脑」里实时看到和接管。"}, nil
	}
	return tools.Result{Content: "浏览器电脑已启用（可以联网）。现在可以用 browser_open 打开网页，用 browser_snapshot 查看页面元素后再点击或输入，" +
		"需要给用户看时用 browser_screenshot。用户也能在右侧「Bot 的电脑」里实时看到和接管。"}, nil
}

// registerComputerTools rebinds the tools that act on the computer to the
// new one: tools built for the old profile would recreate the old container
// on their next call.
func (t *computerEnableTool) registerComputerTools(bot store.Bot, session *browser.Session) {
	e := t.engine
	t.registry.Register(tools.NewShellTool(e.sandbox, bot.OwnerID, bot.ID, bot.ComputerProfile))
	t.registry.Register(tools.NewPythonTool(e.sandbox, bot.OwnerID, bot.ID, bot.ComputerProfile))
	t.registry.Register(tools.NewShellJobsTool(e.sandbox, bot.OwnerID, bot.ID, bot.ComputerProfile))
	for _, tool := range browser.Tools(session) {
		t.registry.Register(tool)
	}
	if bot.ComputerProfile == "desktop" {
		for _, tool := range tools.NewDesktopTools(e.sandbox, bot.OwnerID, bot.ID) {
			t.registry.Register(tool)
		}
	}
}

// dropBrowserSession closes the cached browser tab of a bot whose computer
// is being replaced.
func (e *Engine) dropBrowserSession(botID uuid.UUID) {
	e.browserMu.Lock()
	defer e.browserMu.Unlock()
	if session := e.browserSessions[botID]; session != nil {
		session.Close()
		delete(e.browserSessions, botID)
	}
}
