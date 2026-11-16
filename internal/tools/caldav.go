package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"superbot/internal/caldav"
	"superbot/internal/connectors"
)

func loadCalDAVConfig(ctx context.Context, service *connectors.Service, ownerID uuid.UUID) (caldav.Config, string, error) {
	connector, err := service.ByProvider(ctx, ownerID, "caldav")
	if err != nil {
		return caldav.Config{}, "", errNoCalDAV
	}
	password, err := service.AccessToken(connector)
	if err != nil {
		return caldav.Config{}, "", err
	}
	var cfg caldav.Config
	if err := json.Unmarshal(connector.Config, &cfg); err != nil {
		return caldav.Config{}, "", err
	}
	if strings.TrimSpace(cfg.URL) == "" {
		return caldav.Config{}, "", errCalDAVConfig
	}
	return cfg, password, nil
}

type caldavConfigError string

func (e caldavConfigError) Error() string { return string(e) }

const (
	errNoCalDAV     = caldavConfigError("尚未连接 CalDAV 日历。请先在扩展页连接 CalDAV。")
	errCalDAVConfig = caldavConfigError("CalDAV 配置缺少 url")
)

type caldavListTool struct {
	connectors *connectors.Service
	caldav     *caldav.Service
	ownerID    uuid.UUID
}

func NewCalDAVListTool(connectorsService *connectors.Service, caldavService *caldav.Service, ownerID uuid.UUID) Tool {
	return &caldavListTool{connectors: connectorsService, caldav: caldavService, ownerID: ownerID}
}
func (t *caldavListTool) Name() string { return "list_external_calendar" }
func (t *caldavListTool) Description() string {
	return "读取已连接 CalDAV 日历的事件。需要先在扩展页连接 CalDAV。"
}
func (t *caldavListTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *caldavListTool) Execute(ctx context.Context, _ json.RawMessage) (Result, error) {
	cfg, password, err := loadCalDAVConfig(ctx, t.connectors, t.ownerID)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	events, err := t.caldav.List(ctx, cfg, password)
	if err != nil {
		return Result{Content: "读取 CalDAV 失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(events)
	return Result{Content: string(encoded)}, nil
}

type caldavCreateTool struct {
	connectors *connectors.Service
	caldav     *caldav.Service
	ownerID    uuid.UUID
}

func NewCalDAVCreateTool(connectorsService *connectors.Service, caldavService *caldav.Service, ownerID uuid.UUID) Tool {
	return &caldavCreateTool{connectors: connectorsService, caldav: caldavService, ownerID: ownerID}
}
func (t *caldavCreateTool) Name() string { return "create_external_event" }
func (t *caldavCreateTool) Description() string {
	return "在已连接的 CalDAV 日历创建事件；需要 CalDAV connector。"
}
func (t *caldavCreateTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"start":{"type":"string"},"end":{"type":"string"},"description":{"type":"string"},"location":{"type":"string"}},"required":["title","start","end"]}`)
}
func (t *caldavCreateTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Title       string `json:"title"`
		Start       string `json:"start"`
		End         string `json:"end"`
		Description string `json:"description"`
		Location    string `json:"location"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(input.Start))
	if err != nil {
		return Result{Content: "start must be RFC3339", IsError: true}, nil
	}
	end, err := time.Parse(time.RFC3339, strings.TrimSpace(input.End))
	if err != nil || !end.After(start) {
		return Result{Content: "end must be after start", IsError: true}, nil
	}
	cfg, password, err := loadCalDAVConfig(ctx, t.connectors, t.ownerID)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	href, err := t.caldav.Create(ctx, cfg, password, caldav.NewEvent{
		UID: uuid.NewString(), Title: input.Title, Description: input.Description, Location: input.Location,
		StartsAt: start, EndsAt: end,
	})
	if err != nil {
		return Result{Content: "创建 CalDAV 事件失败: " + err.Error(), IsError: true}, nil
	}
	return Result{Content: "已创建 CalDAV 事件: " + href}, nil
}
