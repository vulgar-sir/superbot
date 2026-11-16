package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"superbot/internal/devices"
)

type localExecTool struct {
	devices *devices.Service
	ownerID uuid.UUID
}

func NewLocalExecTool(service *devices.Service, ownerID uuid.UUID) Tool {
	return &localExecTool{devices: service, ownerID: ownerID}
}

func (t *localExecTool) Name() string { return "local_exec" }

func (t *localExecTool) Description() string {
	return "在用户已配对的本地电脑上执行 shell 命令。设备必须在设置里显式允许 Bot 使用；命令会在用户自己的电脑上运行，务必先确认用户意图，不要执行破坏性操作。"
}

func (t *localExecTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"device_name": {"type": "string", "description": "设备名称"},
			"command": {"type": "string", "description": "要执行的 shell 命令"},
			"timeout_seconds": {"type": "integer", "description": "最长等待秒数，默认 120"}
		},
		"required": ["device_name", "command"]
	}`)
}

type localScreenshotTool struct {
	devices *devices.Service
	ownerID uuid.UUID
}

func NewLocalScreenshotTool(service *devices.Service, ownerID uuid.UUID) Tool {
	return &localScreenshotTool{devices: service, ownerID: ownerID}
}

func (t *localScreenshotTool) Name() string { return "local_screenshot" }
func (t *localScreenshotTool) Description() string {
	return "截取用户已允许 Bot 使用的本地电脑屏幕。返回受权访问的截图 URL；设备需在线且 allow_bot=true。"
}
func (t *localScreenshotTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"device_name":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["device_name"]}`)
}
func (t *localScreenshotTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		DeviceName     string `json:"device_name"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	device, err := t.devices.FindByName(ctx, t.ownerID, input.DeviceName)
	if err != nil {
		return Result{Content: "找不到设备: " + input.DeviceName, IsError: true}, nil
	}
	if !device.AllowBot || device.Status != "online" {
		return Result{Content: "设备未允许 Bot 使用或不在线。", IsError: true}, nil
	}
	job, err := t.devices.CreateJob(ctx, t.ownerID, device.ID, "screenshot", "")
	if err != nil {
		return Result{Content: "创建截图任务失败: " + err.Error(), IsError: true}, nil
	}
	timeout := time.Duration(input.TimeoutSeconds) * time.Second
	if timeout <= 0 || timeout > 120*time.Second {
		timeout = 120 * time.Second
	}
	result, err := t.devices.Wait(ctx, t.ownerID, job.ID, timeout)
	if err != nil {
		return Result{Content: "等待截图失败: " + err.Error(), IsError: true}, nil
	}
	if result.Status == "failed" {
		return Result{Content: "截图失败: " + result.Error, IsError: true}, nil
	}
	url := "/api/devices/" + device.ID.String() + "/jobs/" + job.ID.String() + "/artifact"
	return Result{Content: "截图已保存：" + url, ArtifactPath: url}, nil
}

func (t *localExecTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	var input struct {
		DeviceName     string `json:"device_name"`
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	input.DeviceName = strings.TrimSpace(input.DeviceName)
	input.Command = strings.TrimSpace(input.Command)
	if input.DeviceName == "" || input.Command == "" {
		return Result{Content: "device_name and command are required", IsError: true}, nil
	}
	device, err := t.devices.FindByName(ctx, t.ownerID, input.DeviceName)
	if err != nil {
		return Result{Content: "找不到设备: " + input.DeviceName, IsError: true}, nil
	}
	if !device.AllowBot {
		return Result{Content: "这台设备还没有允许 Bot 使用；请让用户在设置 → 电脑里打开允许开关。", IsError: true}, nil
	}
	if device.Status != "online" {
		return Result{Content: "设备当前离线，无法执行。", IsError: true}, nil
	}
	job, err := t.devices.CreateJob(ctx, t.ownerID, device.ID, "shell", input.Command)
	if err != nil {
		return Result{Content: "创建设备任务失败: " + err.Error(), IsError: true}, nil
	}
	timeout := time.Duration(input.TimeoutSeconds) * time.Second
	if timeout <= 0 || timeout > 120*time.Second {
		timeout = 120 * time.Second
	}
	result, err := t.devices.Wait(ctx, t.ownerID, job.ID, timeout)
	if err != nil {
		return Result{Content: "等待设备结果失败: " + err.Error(), IsError: true}, nil
	}
	if result.Status == "failed" {
		return Result{Content: fmt.Sprintf("设备执行失败：\n%s\n%s", result.Error, result.Output), IsError: true}, nil
	}
	return Result{Content: result.Output}, nil
}

type localClipboardTool struct {
	devices *devices.Service
	ownerID uuid.UUID
}

func NewLocalClipboardTool(service *devices.Service, ownerID uuid.UUID) Tool {
	return &localClipboardTool{devices: service, ownerID: ownerID}
}
func (t *localClipboardTool) Name() string { return "local_clipboard" }
func (t *localClipboardTool) Description() string {
	return "读取或写入用户已允许 Bot 使用的本地电脑剪贴板；每次操作需要用户在设备上批准。"
}
func (t *localClipboardTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"device_name":{"type":"string"},"action":{"type":"string","enum":["read","write"]},"text":{"type":"string"}},"required":["device_name","action"]}`)
}
func (t *localClipboardTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		DeviceName string `json:"device_name"`
		Action     string `json:"action"`
		Text       string `json:"text"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	device, err := t.devices.FindByName(ctx, t.ownerID, strings.TrimSpace(input.DeviceName))
	if err != nil {
		return Result{Content: "找不到设备", IsError: true}, nil
	}
	if !device.AllowBot || device.Status != "online" {
		return Result{Content: "设备未允许 Bot 使用或不在线。", IsError: true}, nil
	}
	kind := "clipboard_" + strings.TrimSpace(input.Action)
	command := ""
	if kind == "clipboard_write" {
		command = input.Text
	} else if kind != "clipboard_read" {
		return Result{Content: "action 必须是 read 或 write", IsError: true}, nil
	}
	job, err := t.devices.CreateJob(ctx, t.ownerID, device.ID, kind, command)
	if err != nil {
		return Result{Content: "创建设备任务失败: " + err.Error(), IsError: true}, nil
	}
	result, err := t.devices.Wait(ctx, t.ownerID, job.ID, 120*time.Second)
	if err != nil {
		return Result{Content: "等待设备结果失败: " + err.Error(), IsError: true}, nil
	}
	if result.Status == "failed" {
		return Result{Content: "设备执行失败: " + result.Error, IsError: true}, nil
	}
	return Result{Content: result.Output}, nil
}

type localFileTool struct {
	devices *devices.Service
	ownerID uuid.UUID
}

func NewLocalFileTool(service *devices.Service, ownerID uuid.UUID) Tool {
	return &localFileTool{devices: service, ownerID: ownerID}
}
func (t *localFileTool) Name() string { return "local_file" }
func (t *localFileTool) Description() string {
	return "读取或写入用户已允许 Bot 使用的本地电脑文件；每次操作需要用户在设备上批准。"
}
func (t *localFileTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"device_name":{"type":"string"},"action":{"type":"string","enum":["read","write"]},"path":{"type":"string"},"content":{"type":"string"}},"required":["device_name","action","path"]}`)
}
func (t *localFileTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		DeviceName string `json:"device_name"`
		Action     string `json:"action"`
		Path       string `json:"path"`
		Content    string `json:"content"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	device, err := t.devices.FindByName(ctx, t.ownerID, strings.TrimSpace(input.DeviceName))
	if err != nil {
		return Result{Content: "找不到设备", IsError: true}, nil
	}
	if !device.AllowBot || device.Status != "online" {
		return Result{Content: "设备未允许 Bot 使用或不在线。", IsError: true}, nil
	}
	kind := "fs_" + strings.TrimSpace(input.Action)
	var command string
	if kind == "fs_read" {
		command = strings.TrimSpace(input.Path)
	} else if kind == "fs_write" {
		encoded, _ := json.Marshal(map[string]string{"path": input.Path, "content": input.Content})
		command = string(encoded)
	} else {
		return Result{Content: "action 必须是 read 或 write", IsError: true}, nil
	}
	job, err := t.devices.CreateJob(ctx, t.ownerID, device.ID, kind, command)
	if err != nil {
		return Result{Content: "创建设备任务失败: " + err.Error(), IsError: true}, nil
	}
	result, err := t.devices.Wait(ctx, t.ownerID, job.ID, 120*time.Second)
	if err != nil {
		return Result{Content: "等待设备结果失败: " + err.Error(), IsError: true}, nil
	}
	if result.Status == "failed" {
		return Result{Content: "设备执行失败: " + result.Error, IsError: true}, nil
	}
	return Result{Content: result.Output}, nil
}

type localAppTool struct {
	devices *devices.Service
	ownerID uuid.UUID
}

func NewLocalAppTool(service *devices.Service, ownerID uuid.UUID) Tool {
	return &localAppTool{devices: service, ownerID: ownerID}
}
func (t *localAppTool) Name() string { return "local_app" }
func (t *localAppTool) Description() string {
	return "在本机列出可见窗口或启动应用；设备需允许 Bot 使用，操作需要本地审批。"
}
func (t *localAppTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"device_name":{"type":"string"},"action":{"type":"string","enum":["list","open"]},"name":{"type":"string"}},"required":["device_name","action"]}`)
}
func (t *localAppTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		DeviceName string `json:"device_name"`
		Action     string `json:"action"`
		Name       string `json:"name"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	device, err := t.devices.FindByName(ctx, t.ownerID, strings.TrimSpace(input.DeviceName))
	if err != nil {
		return Result{Content: "找不到设备", IsError: true}, nil
	}
	if !device.AllowBot || device.Status != "online" {
		return Result{Content: "设备未允许 Bot 使用或不在线。", IsError: true}, nil
	}
	kind := "app_" + strings.TrimSpace(input.Action)
	if kind != "app_list" && kind != "app_open" {
		return Result{Content: "action 必须是 list 或 open", IsError: true}, nil
	}
	if kind == "app_open" && strings.TrimSpace(input.Name) == "" {
		return Result{Content: "name 不能为空", IsError: true}, nil
	}
	job, err := t.devices.CreateJob(ctx, t.ownerID, device.ID, kind, strings.TrimSpace(input.Name))
	if err != nil {
		return Result{Content: "创建设备任务失败: " + err.Error(), IsError: true}, nil
	}
	result, err := t.devices.Wait(ctx, t.ownerID, job.ID, 120*time.Second)
	if err != nil {
		return Result{Content: "等待设备结果失败: " + err.Error(), IsError: true}, nil
	}
	if result.Status == "failed" {
		return Result{Content: "设备执行失败: " + result.Error, IsError: true}, nil
	}
	return Result{Content: result.Output}, nil
}

type localLocationTool struct {
	devices *devices.Service
	ownerID uuid.UUID
}

func NewLocalLocationTool(service *devices.Service, ownerID uuid.UUID) Tool {
	return &localLocationTool{devices: service, ownerID: ownerID}
}
func (t *localLocationTool) Name() string { return "local_location" }
func (t *localLocationTool) Description() string {
	return "通过设备公网 IP 查询大致位置；设备需允许 Bot 使用，查询需要本地审批。"
}
func (t *localLocationTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"device_name":{"type":"string"}},"required":["device_name"]}`)
}
func (t *localLocationTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		DeviceName string `json:"device_name"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	return runLocalJob(ctx, t.devices, t.ownerID, input.DeviceName, "location", "")
}

type localAudioTool struct {
	devices *devices.Service
	ownerID uuid.UUID
}

func NewLocalAudioTool(service *devices.Service, ownerID uuid.UUID) Tool {
	return &localAudioTool{devices: service, ownerID: ownerID}
}
func (t *localAudioTool) Name() string { return "local_audio" }
func (t *localAudioTool) Description() string {
	return "读取设备音频设备列表；设备需允许 Bot 使用，查询需要本地审批。"
}
func (t *localAudioTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"device_name":{"type":"string"}},"required":["device_name"]}`)
}
func (t *localAudioTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		DeviceName string `json:"device_name"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	return runLocalJob(ctx, t.devices, t.ownerID, input.DeviceName, "audio_list", "")
}

func runLocalJob(ctx context.Context, service *devices.Service, ownerID uuid.UUID, deviceName, kind, command string) (Result, error) {
	device, err := service.FindByName(ctx, ownerID, strings.TrimSpace(deviceName))
	if err != nil {
		return Result{Content: "找不到设备", IsError: true}, nil
	}
	if !device.AllowBot || device.Status != "online" {
		return Result{Content: "设备未允许 Bot 使用或不在线。", IsError: true}, nil
	}
	job, err := service.CreateJob(ctx, ownerID, device.ID, kind, command)
	if err != nil {
		return Result{Content: "创建设备任务失败: " + err.Error(), IsError: true}, nil
	}
	result, err := service.Wait(ctx, ownerID, job.ID, 120*time.Second)
	if err != nil {
		return Result{Content: "等待设备结果失败: " + err.Error(), IsError: true}, nil
	}
	if result.Status == "failed" {
		return Result{Content: "设备执行失败: " + result.Error, IsError: true}, nil
	}
	return Result{Content: result.Output}, nil
}

type localHTTPTool struct {
	devices *devices.Service
	ownerID uuid.UUID
}

func NewLocalHTTPTool(service *devices.Service, ownerID uuid.UUID) Tool {
	return &localHTTPTool{devices: service, ownerID: ownerID}
}

func (t *localHTTPTool) Name() string { return "local_http_request" }

func (t *localHTTPTool) Description() string {
	return "通过用户已允许 Bot 使用的本地电脑发起 HTTP/HTTPS 请求，用于访问只有该电脑能访问的内网或本地服务。每次请求都会在用户电脑上弹出审批；不支持 file:// 等协议。"
}

func (t *localHTTPTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"device_name": {"type": "string"},
			"url": {"type": "string"},
			"method": {"type": "string", "enum": ["GET", "POST", "PUT", "PATCH", "DELETE"]},
			"headers": {"type": "object", "additionalProperties": {"type": "string"}},
			"body": {"type": "string"},
			"timeout_seconds": {"type": "integer"}
		},
		"required": ["device_name", "url"]
	}`)
}

func (t *localHTTPTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		DeviceName     string            `json:"device_name"`
		URL            string            `json:"url"`
		Method         string            `json:"method"`
		Headers        map[string]string `json:"headers"`
		Body           string            `json:"body"`
		TimeoutSeconds int               `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	input.DeviceName = strings.TrimSpace(input.DeviceName)
	input.URL = strings.TrimSpace(input.URL)
	if input.DeviceName == "" || input.URL == "" {
		return Result{Content: "device_name and url are required", IsError: true}, nil
	}
	if !strings.HasPrefix(input.URL, "http://") && !strings.HasPrefix(input.URL, "https://") {
		return Result{Content: "only http:// and https:// URLs are allowed", IsError: true}, nil
	}
	method := strings.ToUpper(strings.TrimSpace(input.Method))
	if method == "" {
		method = "GET"
	}
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return Result{Content: "unsupported HTTP method", IsError: true}, nil
	}
	payload, err := json.Marshal(map[string]any{
		"url": input.URL, "method": method, "headers": input.Headers, "body": input.Body,
	})
	if err != nil {
		return Result{Content: "could not encode request", IsError: true}, nil
	}
	device, err := t.devices.FindByName(ctx, t.ownerID, input.DeviceName)
	if err != nil {
		return Result{Content: "找不到设备: " + input.DeviceName, IsError: true}, nil
	}
	if !device.AllowBot {
		return Result{Content: "这台设备还没有允许 Bot 使用；请让用户在设置 → 电脑里打开允许开关。", IsError: true}, nil
	}
	if device.Status != "online" {
		return Result{Content: "设备当前离线，无法执行。", IsError: true}, nil
	}
	job, err := t.devices.CreateJob(ctx, t.ownerID, device.ID, "http_request", string(payload))
	if err != nil {
		return Result{Content: "创建设备任务失败: " + err.Error(), IsError: true}, nil
	}
	timeout := time.Duration(input.TimeoutSeconds) * time.Second
	if timeout <= 0 || timeout > 70*time.Second {
		timeout = 70 * time.Second
	}
	result, err := t.devices.Wait(ctx, t.ownerID, job.ID, timeout)
	if err != nil {
		return Result{Content: "等待设备结果失败: " + err.Error(), IsError: true}, nil
	}
	if result.Status == "failed" {
		return Result{Content: "请求失败: " + result.Error, IsError: true}, nil
	}
	return Result{Content: result.Output}, nil
}
