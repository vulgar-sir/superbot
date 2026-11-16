package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"superbot/internal/store"
)

type calendarEventTool struct {
	db             *store.DB
	ownerID        uuid.UUID
	botID          uuid.UUID
	conversationID uuid.UUID
}

func NewCalendarEventTool(db *store.DB, ownerID, botID, conversationID uuid.UUID) Tool {
	return &calendarEventTool{db: db, ownerID: ownerID, botID: botID, conversationID: conversationID}
}

func (t *calendarEventTool) Name() string { return "create_calendar_event" }
func (t *calendarEventTool) Description() string {
	return "创建日程/会议事件，生成标准 ICS 文件并保存到 Bot 资料库；用户可下载后用日历应用打开或转发邀请。"
}
func (t *calendarEventTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"start":{"type":"string","description":"RFC3339，例如 2026-11-12T10:00:00+08:00"},"end":{"type":"string"},"description":{"type":"string"},"location":{"type":"string"}},"required":["title","start","end"]}`)
}

func (t *calendarEventTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
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
	if err != nil {
		return Result{Content: "end must be RFC3339", IsError: true}, nil
	}
	if !end.After(start) {
		return Result{Content: "end must be after start", IsError: true}, nil
	}
	uid := uuid.NewString()
	ics := fmt.Sprintf("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//superbot//calendar//EN\r\nBEGIN:VEVENT\r\nUID:%s\r\nDTSTAMP:%s\r\nDTSTART:%s\r\nDTEND:%s\r\nSUMMARY:%s\r\nDESCRIPTION:%s\r\nLOCATION:%s\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		uid, time.Now().UTC().Format("20060102T150405Z"), start.UTC().Format("20060102T150405Z"),
		end.UTC().Format("20060102T150405Z"), escapeICS(input.Title), escapeICS(input.Description), escapeICS(input.Location))
	if _, err := t.db.CreateCalendarEvent(ctx, store.CreateCalendarEventParams{
		OwnerID: t.ownerID, Title: strings.TrimSpace(input.Title), Description: input.Description,
		Location: input.Location, StartsAt: start, EndsAt: end,
	}); err != nil {
		return Result{Content: "保存日程失败: " + err.Error(), IsError: true}, nil
	}
	item, err := t.db.CreateLibraryItem(ctx, store.CreateLibraryItemParams{
		OwnerID: t.ownerID, BotID: t.botID, Kind: "file",
		Title: strings.TrimSpace(input.Title), Description: "日历事件",
		Content: ics, MimeType: "text/calendar", FilePath: "",
		ConversationID: &t.conversationID, Metadata: json.RawMessage(`{"calendar":true}`),
	})
	if err != nil {
		return Result{Content: "保存日程失败: " + err.Error(), IsError: true}, nil
	}
	return Result{Content: fmt.Sprintf("日程已创建并保存到资料库：%s（id=%s）", item.Title, item.ID)}, nil
}

func escapeICS(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, ";", "\\;")
	value = strings.ReplaceAll(value, ",", "\\,")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return value
}

type calendarListTool struct {
	db      *store.DB
	ownerID uuid.UUID
}

func NewCalendarListTool(db *store.DB, ownerID uuid.UUID) Tool {
	return &calendarListTool{db: db, ownerID: ownerID}
}
func (t *calendarListTool) Name() string { return "list_calendar_events" }
func (t *calendarListTool) Description() string {
	return "列出账户未来及近期的日程；可指定天数窗口。"
}
func (t *calendarListTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"days":{"type":"integer"}}}`)
}
func (t *calendarListTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Days int `json:"days"`
	}
	_ = json.Unmarshal(raw, &input)
	from := time.Now().Add(-24 * time.Hour)
	if input.Days > 0 {
		from = time.Now()
	}
	events, err := t.db.ListCalendarEvents(ctx, store.ListCalendarEventsParams{
		OwnerID: t.ownerID, EndsAt: from,
	})
	if err != nil {
		return Result{Content: "读取日程失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(events)
	return Result{Content: string(encoded)}, nil
}
