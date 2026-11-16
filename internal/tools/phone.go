package tools

import (
	"context"
	"encoding/json"
	"errors"

	"superbot/internal/phone"
)

type sendSMSTool struct {
	service *phone.Service
}

func NewSendSMSTool(service *phone.Service) Tool {
	return &sendSMSTool{service: service}
}

func (t *sendSMSTool) Name() string { return "send_sms" }
func (t *sendSMSTool) Description() string {
	return "通过已配置的 Twilio 号码发送短信。目标号码必须是 E.164 格式（例如 +8613800000000）。"
}
func (t *sendSMSTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"to":{"type":"string"},"message":{"type":"string"}},"required":["to","message"]}`)
}
func (t *sendSMSTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		To      string `json:"to"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	sid, err := t.service.SendSMS(ctx, input.To, input.Message)
	if err != nil {
		if errors.Is(err, phone.ErrNotConfigured) {
			return Result{Content: "短信服务未配置：需要 SUPERBOT_TWILIO_ACCOUNT_SID/AUTH_TOKEN/FROM_NUMBER", IsError: true}, nil
		}
		return Result{Content: "发送短信失败: " + err.Error(), IsError: true}, nil
	}
	return Result{Content: "短信已提交，sid=" + sid}, nil
}

type callPhoneTool struct {
	service *phone.Service
}

func NewCallPhoneTool(service *phone.Service) Tool {
	return &callPhoneTool{service: service}
}

func (t *callPhoneTool) Name() string { return "make_call" }
func (t *callPhoneTool) Description() string {
	return "通过已配置的 Twilio 号码拨打语音电话，并用语音播报给定内容；号码必须是 E.164 格式。"
}
func (t *callPhoneTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"to":{"type":"string"},"message":{"type":"string"}},"required":["to","message"]}`)
}
func (t *callPhoneTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		To      string `json:"to"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	sid, err := t.service.Call(ctx, input.To, input.Message)
	if err != nil {
		if errors.Is(err, phone.ErrNotConfigured) {
			return Result{Content: "电话服务未配置：需要 SUPERBOT_TWILIO_ACCOUNT_SID/AUTH_TOKEN/FROM_NUMBER", IsError: true}, nil
		}
		return Result{Content: "拨打电话失败: " + err.Error(), IsError: true}, nil
	}
	return Result{Content: "电话已提交，sid=" + sid}, nil
}
