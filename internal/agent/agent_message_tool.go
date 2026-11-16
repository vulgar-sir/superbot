package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/agentchat"
	"superbot/internal/tools"
)

// agentMessageTool lets a Bot send a private message to a teammate. The
// teammate's reply runs asynchronously in its own agent thread, while the
// origin chat gets a folded "与 X 的消息" card.
type agentMessageTool struct {
	chat       *agentchat.Service
	ownerID    uuid.UUID
	senderID   uuid.UUID
	originConv uuid.UUID
	// hop is how many bot-to-bot messages led to the current turn without a
	// human in between.
	hop int
}

func (t *agentMessageTool) Name() string { return "send_to_agent" }

func (t *agentMessageTool) Description() string {
	return "给同账号的另一个 Bot 发私信。对方会在独立的 Bot 私信线程中处理，回复会自动送回给你并唤醒你继续；当前聊天会出现一条折叠卡片。只在确实需要队友协作时使用，不要用它刷屏。"
}

func (t *agentMessageTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"bot_name": {"type": "string", "description": "接收方 Bot 的名称或 ID"},
			"message": {"type": "string", "description": "要发送的私信内容，写清目标、输入和期望输出"}
		},
		"required": ["bot_name", "message"]
	}`)
}

func (t *agentMessageTool) Execute(ctx context.Context, args json.RawMessage) (tools.Result, error) {
	var input struct {
		BotName string `json:"bot_name"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return tools.Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	recipient, err := t.chat.FindBot(ctx, t.ownerID, input.BotName)
	if err != nil {
		return tools.Result{Content: "找不到接收方 Bot: " + input.BotName, IsError: true}, nil
	}
	if recipient.ID == t.senderID {
		return tools.Result{Content: "不能给自己发私信。", IsError: true}, nil
	}
	result, err := t.chat.Send(ctx, t.ownerID, t.senderID, recipient.ID, &t.originConv, input.Message, t.hop)
	if errors.Is(err, agentchat.ErrTooManyHops) {
		return tools.Result{
			Content: "Bot 之间的私信接力已达到上限，没有再转发。请直接给出你目前的结论，或向用户说明还缺什么。",
			IsError: true,
		}, nil
	}
	if err != nil {
		return tools.Result{Content: "发送失败: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{
		Content: fmt.Sprintf("已发送给 %s（thread=%s）。对方处理完后，回复会自动送回给你，你不需要等待或轮询；现在可以先完成手头其它部分，或直接结束本轮。不要重复发送同一内容。",
			strings.TrimSpace(result.Recipient.Name), result.Thread.ID.String()),
	}, nil
}
