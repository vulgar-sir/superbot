package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/skills"
)

type SkillListTool struct {
	service *skills.Service
	ownerID uuid.UUID
	botID   uuid.UUID
}

func NewSkillListTool(service *skills.Service, ownerID, botID uuid.UUID) *SkillListTool {
	return &SkillListTool{service: service, ownerID: ownerID, botID: botID}
}

func (t *SkillListTool) Name() string { return "skill_list" }
func (t *SkillListTool) Description() string {
	return "List the skills enabled for this bot. Use skill_read to load a skill's full instructions."
}
func (t *SkillListTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *SkillListTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	items, err := t.service.ListForBot(ctx, t.botID)
	if err != nil {
		return Result{}, err
	}
	builtins, builtinErr := t.service.ListBuiltins(ctx, t.ownerID)
	if builtinErr == nil {
		seen := make(map[uuid.UUID]bool, len(items))
		for _, item := range items {
			seen[item.ID] = true
		}
		for _, item := range builtins {
			if !seen[item.ID] {
				items = append(items, item)
			}
		}
	}
	if len(items) == 0 {
		return Result{Content: "no skills enabled"}, nil
	}
	var b strings.Builder
	for _, skill := range items {
		fmt.Fprintf(&b, "- %s: %s\n", skill.Name, skill.Description)
	}
	return Result{Content: strings.TrimSpace(b.String())}, nil
}

type SkillReadTool struct {
	service *skills.Service
	ownerID uuid.UUID
	botID   uuid.UUID
}

func NewSkillReadTool(service *skills.Service, ownerID, botID uuid.UUID) *SkillReadTool {
	return &SkillReadTool{service: service, ownerID: ownerID, botID: botID}
}

func (t *SkillReadTool) Name() string { return "skill_read" }
func (t *SkillReadTool) Description() string {
	return "Read the full instructions of an enabled skill by name."
}
func (t *SkillReadTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
}

func (t *SkillReadTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	content, err := t.service.ReadContent(ctx, t.ownerID, t.botID, args.Name)
	if err != nil {
		return Result{}, err
	}
	return Result{Content: content}, nil
}

type ExtensionProposeTool struct {
	service *skills.Service
	ownerID uuid.UUID
}

func NewExtensionProposeTool(service *skills.Service, ownerID uuid.UUID) *ExtensionProposeTool {
	return &ExtensionProposeTool{service: service, ownerID: ownerID}
}

func (t *ExtensionProposeTool) Name() string { return "extension_propose" }
func (t *ExtensionProposeTool) Description() string {
	return "Propose a new skill for the user to approve. The skill is stored as a draft and does not take effect until approved in the Extensions page."
}
func (t *ExtensionProposeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string","enum":["skill"]},"name":{"type":"string"},"description":{"type":"string"},"content":{"type":"string","description":"Markdown instructions for the skill"}},"required":["kind","name","content"]}`)
}

func (t *ExtensionProposeTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Kind        string `json:"kind"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Kind != "skill" {
		return Result{}, fmt.Errorf("only skill proposals are supported in M2")
	}
	skill, err := t.service.Create(ctx, t.ownerID, args.Name, args.Description, args.Content, "bot")
	if err != nil {
		return Result{}, err
	}
	return Result{
		Content: fmt.Sprintf("Skill draft %q created (%s). Ask the user to approve it on the Extensions page.", skill.Name, skill.ID),
	}, nil
}
