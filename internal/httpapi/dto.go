package httpapi

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"superbot/internal/store"
)

type userDTO struct {
	ID           uuid.UUID  `json:"id"`
	Email        string     `json:"email"`
	DisplayName  string     `json:"display_name"`
	Role         string     `json:"role"`
	PrimaryBotID *uuid.UUID `json:"primary_bot_id"`
}

func toUserDTO(u store.User) userDTO {
	return userDTO{
		ID:           u.ID,
		Email:        u.Email,
		DisplayName:  u.DisplayName,
		Role:         u.Role,
		PrimaryBotID: u.PrimaryBotID,
	}
}

type providerDTO struct {
	ID        uuid.UUID       `json:"id"`
	Type      string          `json:"type"`
	Kind      string          `json:"kind"`
	Name      string          `json:"name"`
	BaseURL   string          `json:"base_url"`
	HasKey    bool            `json:"has_key"`
	Config    json.RawMessage `json:"config"`
	Enabled   bool            `json:"enabled"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func toProviderDTO(p store.Provider) providerDTO {
	cfg := p.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	return providerDTO{
		ID:        p.ID,
		Type:      p.Type,
		Kind:      p.Kind,
		Name:      p.Name,
		BaseURL:   p.BaseUrl,
		HasKey:    len(p.ApiKeyEnc) > 0,
		Config:    cfg,
		Enabled:   p.Enabled,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

type modelDTO struct {
	ID                      uuid.UUID `json:"id"`
	ProviderID              uuid.UUID `json:"provider_id"`
	Name                    string    `json:"name"`
	ContextWindow           int32     `json:"context_window"`
	MaxOutput               int32     `json:"max_output"`
	SupportsTools           bool      `json:"supports_tools"`
	SupportsVision          bool      `json:"supports_vision"`
	SupportsReasoning       bool      `json:"supports_reasoning"`
	PriceInMicrosPerMtok    int64     `json:"price_in_micros_per_mtok"`
	PriceCacheMicrosPerMtok int64     `json:"price_cache_micros_per_mtok"`
	PriceOutMicrosPerMtok   int64     `json:"price_out_micros_per_mtok"`
}

func toModelDTO(m store.Model) modelDTO {
	return modelDTO{
		ID:                      m.ID,
		ProviderID:              m.ProviderID,
		Name:                    m.Name,
		ContextWindow:           m.ContextWindow,
		MaxOutput:               m.MaxOutput,
		SupportsTools:           m.SupportsTools,
		SupportsVision:          m.SupportsVision,
		SupportsReasoning:       m.SupportsReasoning,
		PriceInMicrosPerMtok:    m.PriceInMicrosPerMtok,
		PriceCacheMicrosPerMtok: m.PriceCacheMicrosPerMtok,
		PriceOutMicrosPerMtok:   m.PriceOutMicrosPerMtok,
	}
}

type botDTO struct {
	ID              uuid.UUID           `json:"id"`
	Name            string              `json:"name"`
	Slug            string              `json:"slug"`
	Avatar          string              `json:"avatar"`
	Description     string              `json:"description"`
	SystemPrompt    string              `json:"system_prompt"`
	ProviderID      *uuid.UUID          `json:"provider_id"`
	Model           string              `json:"model"`
	Params          json.RawMessage     `json:"params"`
	Capabilities    json.RawMessage     `json:"capabilities"`
	ToolPolicy      json.RawMessage     `json:"tool_policy"`
	MemoryEnabled   bool                `json:"memory_enabled"`
	ComputerProfile string              `json:"computer_profile"`
	Kind            string              `json:"kind"`
	Visibility      string              `json:"visibility"`
	CreatedByUserID *uuid.UUID          `json:"created_by_user_id"`
	CreatedByBotID  *uuid.UUID          `json:"created_by_bot_id"`
	SectionID       *uuid.UUID          `json:"section_id"`
	Position        int32               `json:"position"`
	Title           string              `json:"title"`
	AvatarShape     string              `json:"avatar_shape"`
	AvatarColor     string              `json:"avatar_color"`
	Pinned          bool                `json:"pinned"`
	LastActivityAt  *time.Time          `json:"last_activity_at"`
	Proactivity     json.RawMessage     `json:"proactivity"`
	LatestActivity  *activityPreviewDTO `json:"latest_activity,omitempty"`
	UnreadCount     int64               `json:"unread_count"`
	Hidden          bool                `json:"hidden"`
	MarkedUnread    bool                `json:"marked_unread"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

type activityPreviewDTO struct {
	Kind          string     `json:"kind"`
	Summary       string     `json:"summary"`
	AgentThreadID *uuid.UUID `json:"agent_thread_id"`
	CreatedAt     time.Time  `json:"created_at"`
}

func toBotDTO(b store.Bot) botDTO {
	params := b.Params
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	caps := b.Capabilities
	if len(caps) == 0 {
		caps = json.RawMessage(`{}`)
	}
	policy := b.ToolPolicy
	if len(policy) == 0 {
		policy = json.RawMessage(`{}`)
	}
	proactivity := b.Proactivity
	if len(proactivity) == 0 {
		proactivity = json.RawMessage(`{}`)
	}
	return botDTO{
		ID:              b.ID,
		Name:            b.Name,
		Slug:            b.Slug,
		Avatar:          b.Avatar,
		Description:     b.Description,
		SystemPrompt:    b.SystemPrompt,
		ProviderID:      b.ProviderID,
		Model:           b.Model,
		Params:          params,
		Capabilities:    caps,
		ToolPolicy:      policy,
		MemoryEnabled:   b.MemoryEnabled,
		ComputerProfile: b.ComputerProfile,
		Kind:            b.Kind,
		Visibility:      b.Visibility,
		CreatedByUserID: b.CreatedByUserID,
		CreatedByBotID:  b.CreatedByBotID,
		SectionID:       b.SectionID,
		Position:        b.Position,
		Title:           b.Title,
		AvatarShape:     b.AvatarShape,
		AvatarColor:     b.AvatarColor,
		Pinned:          b.Pinned,
		Hidden:          b.Hidden,
		MarkedUnread:    b.MarkedUnread,
		LastActivityAt:  b.LastActivityAt,
		Proactivity:     proactivity,
		CreatedAt:       b.CreatedAt,
		UpdatedAt:       b.UpdatedAt,
	}
}

type conversationDTO struct {
	ID           uuid.UUID  `json:"id"`
	TeamID       *uuid.UUID `json:"team_id"`
	DirectBotID  *uuid.UUID `json:"direct_bot_id"`
	Kind         string     `json:"kind"`
	Title        string     `json:"title"`
	Status       string     `json:"status"`
	BudgetMicros int64      `json:"budget_micros"`
	SpentMicros  int64      `json:"spent_micros"`
	LastEventSeq int64      `json:"last_event_seq"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func toConversationDTO(c store.Conversation) conversationDTO {
	return conversationDTO{
		ID:           c.ID,
		TeamID:       c.TeamID,
		DirectBotID:  c.DirectBotID,
		Kind:         c.Kind,
		Title:        c.Title,
		Status:       c.Status,
		BudgetMicros: c.BudgetMicros,
		SpentMicros:  c.SpentMicros,
		LastEventSeq: c.LastEventSeq,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}
}

type messageDTO struct {
	ID              uuid.UUID            `json:"id"`
	Ordinal         int64                `json:"ordinal"`
	CycleID         *uuid.UUID           `json:"cycle_id"`
	SenderType      string               `json:"sender_type"`
	SenderBotID     *uuid.UUID           `json:"sender_bot_id"`
	Content         string               `json:"content"`
	Metadata        json.RawMessage      `json:"metadata"`
	RunID           *uuid.UUID           `json:"run_id"`
	ParentMessageID *uuid.UUID           `json:"parent_message_id"`
	Reactions       []messageReactionDTO `json:"reactions"`
	Attachments     []attachmentDTO      `json:"attachments"`
	Poll            *pollDTO             `json:"poll,omitempty"`
	EditedAt        *time.Time           `json:"edited_at"`
	PinnedAt        *time.Time           `json:"pinned_at"`
	CreatedAt       time.Time            `json:"created_at"`
}

type pollOptionDTO struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Votes int    `json:"votes"`
	Voted bool   `json:"voted"`
}

type pollDTO struct {
	Question   string          `json:"question"`
	Multiple   bool            `json:"multiple"`
	Options    []pollOptionDTO `json:"options"`
	TotalVotes int             `json:"total_votes"`
}

type attachmentDTO struct {
	ID        uuid.UUID `json:"id"`
	Filename  string    `json:"filename"`
	MimeType  string    `json:"mime_type"`
	SizeBytes int64     `json:"size_bytes"`
	URL       string    `json:"url"`
}

func toAttachmentDTO(a store.Attachment) attachmentDTO {
	return attachmentDTO{
		ID:        a.ID,
		Filename:  a.Filename,
		MimeType:  a.MimeType,
		SizeBytes: a.SizeBytes,
		URL:       "/api/attachments/" + a.ID.String(),
	}
}

type messageReactionDTO struct {
	Emoji   string `json:"emoji"`
	Count   int32  `json:"count"`
	Reacted bool   `json:"reacted"`
}

func toMessageDTO(m store.Message) messageDTO {
	meta := m.Metadata
	if len(meta) == 0 {
		meta = json.RawMessage(`{}`)
	}
	return messageDTO{
		ID:              m.ID,
		Ordinal:         m.Ordinal,
		CycleID:         m.CycleID,
		SenderType:      m.SenderType,
		SenderBotID:     m.SenderBotID,
		Content:         m.Content,
		Metadata:        meta,
		RunID:           m.RunID,
		ParentMessageID: m.ParentMessageID,
		Reactions:       []messageReactionDTO{},
		Attachments:     []attachmentDTO{},
		EditedAt:        m.EditedAt,
		PinnedAt:        m.PinnedAt,
		CreatedAt:       m.CreatedAt,
	}
}

type runDTO struct {
	ID         uuid.UUID  `json:"id"`
	CycleID    uuid.UUID  `json:"cycle_id"`
	BotID      uuid.UUID  `json:"bot_id"`
	Status     string     `json:"status"`
	Step       int32      `json:"step"`
	UsageIn    int64      `json:"usage_in"`
	UsageOut   int64      `json:"usage_out"`
	CostMicros int64      `json:"cost_micros"`
	Warning    string     `json:"warning"`
	Error      string     `json:"error"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func toRunDTO(r store.Run) runDTO {
	return runDTO{
		ID:         r.ID,
		CycleID:    r.CycleID,
		BotID:      r.BotID,
		Status:     r.Status,
		Step:       r.Step,
		UsageIn:    r.UsageIn,
		UsageOut:   r.UsageOut,
		CostMicros: r.CostMicros,
		Warning:    r.Warning,
		Error:      r.Error,
		StartedAt:  r.StartedAt,
		FinishedAt: r.FinishedAt,
		CreatedAt:  r.CreatedAt,
	}
}

type toolInvocationDTO struct {
	ID             uuid.UUID       `json:"id"`
	CallID         string          `json:"call_id"`
	Name           string          `json:"name"`
	Args           json.RawMessage `json:"args"`
	Status         string          `json:"status"`
	ResultArtifact string          `json:"result_artifact"`
	Error          string          `json:"error"`
	StartedAt      *time.Time      `json:"started_at"`
	FinishedAt     *time.Time      `json:"finished_at"`
}

func toToolInvocationDTO(t store.ToolInvocation) toolInvocationDTO {
	args := t.Args
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	return toolInvocationDTO{
		ID:             t.ID,
		CallID:         t.CallID,
		Name:           t.Name,
		Args:           args,
		Status:         t.Status,
		ResultArtifact: t.ResultArtifact,
		Error:          t.Error,
		StartedAt:      t.StartedAt,
		FinishedAt:     t.FinishedAt,
	}
}

type eventDTO struct {
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

func toEventDTO(e store.Event) eventDTO {
	payload := e.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	return eventDTO{Seq: e.Seq, Type: e.Type, Payload: payload, CreatedAt: e.CreatedAt}
}
