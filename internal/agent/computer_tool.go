package agent

import (
	"context"
	"encoding/json"

	"superbot/internal/browser"
	"superbot/internal/store"
	"superbot/internal/tools"
)

// computerEnableTool lets a bot without a computer start its browser
// computer when a task needs one. New bots start without a computer to save
// resources; before this tool existed a bot asked to "open a site in the
// browser" had no browser tools, no network in its shell, and spent its
// turn probing the sandbox and inventing settings for the user.
type computerEnableTool struct {
	engine   *Engine
	bot      *store.Bot
	registry *tools.Registry
}

func (t *computerEnableTool) Name() string { return "computer_enable" }

func (t *computerEnableTool) Description() string {
	return "启用你的浏览器电脑。当用户要求用浏览器打开/操作网页、登录网站、截图网页时调用；" +
		"启用后从下一步起可以使用 browser_open / browser_snapshot / browser_click / browser_type / browser_screenshot，" +
		"shell 也会切换到这台可以联网的电脑。只读地查资料不需要启用，直接用 fetch_url 或 web_search。"
}

func (t *computerEnableTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"reason":{"type":"string","description":"为什么需要浏览器，一句话"}}}`)
}

func (t *computerEnableTool) Execute(ctx context.Context, _ json.RawMessage) (tools.Result, error) {
	e := t.engine
	if e.sandbox == nil {
		return tools.Result{Content: "这个部署没有启用电脑沙箱，无法启动浏览器。请改用 fetch_url 查看网页内容，并如实告诉用户。", IsError: true}, nil
	}
	if t.bot.ComputerProfile == "browser" || t.bot.ComputerProfile == "desktop" {
		return tools.Result{Content: "浏览器电脑已经启用，直接使用 browser_* 工具。"}, nil
	}
	updated, err := e.db.SetBotComputerProfile(ctx, store.SetBotComputerProfileParams{
		ID: t.bot.ID, OwnerID: t.bot.OwnerID, ComputerProfile: "browser",
	})
	if err != nil {
		return tools.Result{Content: "启用电脑失败：" + err.Error(), IsError: true}, nil
	}
	*t.bot = updated
	session, err := e.browserSession(ctx, updated)
	if err != nil {
		return tools.Result{Content: "电脑已切换为浏览器模式，但浏览器没有启动成功：" + err.Error() + "。请如实告诉用户。", IsError: true}, nil
	}
	// Shell and python must run in the new computer; tools built for the old
	// profile would recreate the old container on their next call.
	t.registry.Register(tools.NewShellTool(e.sandbox, updated.OwnerID, updated.ID, updated.ComputerProfile))
	t.registry.Register(tools.NewPythonTool(e.sandbox, updated.OwnerID, updated.ID, updated.ComputerProfile))
	for _, tool := range browser.Tools(session) {
		t.registry.Register(tool)
	}
	e.logger.Info("bot enabled its browser computer", "bot_id", updated.ID)
	return tools.Result{Content: "浏览器电脑已启用（可以联网）。现在可以用 browser_open 打开网页，用 browser_snapshot 查看页面元素后再点击或输入，" +
		"需要给用户看时用 browser_screenshot。用户也能在右侧「Bot 的电脑」里实时看到和接管。"}, nil
}
