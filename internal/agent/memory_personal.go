package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/memory"
	"superbot/internal/store"
)

// personalCheckBatch bounds how many group memories one maintenance pass
// looks at; there is one model call per group.
const personalCheckBatch = 60

// movePersonalTeamMemories moves facts about a person that the earlier
// automatic review stored in a group's memory to that person's own memory.
// It only moves where the person is certain: an account with one member.
// In other accounts the memories stay in the group (its panel shows them)
// and are marked as checked so they are not asked about again.
func (e *Engine) movePersonalTeamMemories(ctx context.Context) {
	rows, err := e.db.ListUncheckedTeamReviewMemories(ctx, personalCheckBatch)
	if err != nil || len(rows) == 0 {
		return
	}
	byTeam := make(map[uuid.UUID][]store.Memory)
	var order []uuid.UUID
	for _, row := range rows {
		if _, seen := byTeam[row.ScopeID]; !seen {
			order = append(order, row.ScopeID)
		}
		byTeam[row.ScopeID] = append(byTeam[row.ScopeID], row)
	}
	for _, teamID := range order {
		if ctx.Err() != nil {
			return
		}
		e.movePersonalForTeam(ctx, teamID, byTeam[teamID])
	}
}

func (e *Engine) movePersonalForTeam(ctx context.Context, teamID uuid.UUID, items []store.Memory) {
	mark := func(item store.Memory, outcome string) {
		if err := e.db.MarkMemoryPersonalChecked(ctx, store.MarkMemoryPersonalCheckedParams{ID: item.ID, Outcome: outcome}); err != nil {
			e.logger.Warn("mark group memory checked failed", "memory_id", item.ID, "error", err)
		}
	}
	markAll := func(outcome string) {
		for _, item := range items {
			mark(item, outcome)
		}
	}
	ownerID := items[0].OwnerID
	members, err := e.db.ListAccountMembers(ctx, ownerID)
	if err != nil {
		return
	}
	if len(members) != 1 {
		markAll("several_people")
		return
	}
	person := members[0].ID
	team, err := e.db.GetTeam(ctx, store.GetTeamParams{ID: teamID, OwnerID: ownerID})
	if err != nil {
		markAll("no_group")
		return
	}
	reviewer, ok := e.personalCheckBot(ctx, team)
	if !ok {
		markAll("no_model")
		return
	}
	convs, err := e.db.ListTeamConversations(ctx, store.ListTeamConversationsParams{TeamID: &team.ID, Limit: 1})
	if err != nil || len(convs) == 0 {
		markAll("no_conversation")
		return
	}

	var list strings.Builder
	for i, item := range items {
		fmt.Fprintf(&list, "%d. %s\n", i+1, strings.TrimSpace(item.Content))
	}
	reply, err := (&memoryExtractor{engine: e}).Extract(ctx, memory.ExtractRequest{
		BotID: reviewer.ID, OwnerID: ownerID, UserID: person, ConversationID: convs[0].ID,
		System: personalCheckSystem,
		User:   "群聊「" + team.Name + "」的群记忆：\n" + list.String(),
	})
	if err != nil {
		// Budget, provider or model trouble: try again on a later pass.
		e.logger.Warn("group memory personal check failed", "team_id", teamID, "error", err)
		return
	}
	personal, ok := parsePersonalIndexes(reply, len(items))
	if !ok {
		e.logger.Warn("group memory personal check returned no verdict", "team_id", teamID)
		return
	}
	moved := 0
	for i, item := range items {
		if !personal[i+1] {
			mark(item, "group")
			continue
		}
		meta, _ := json.Marshal(map[string]any{"moved_from_team": teamID.String()})
		saved, err := e.memory.SaveWith(ctx, memory.SaveInput{
			OwnerID: ownerID, ScopeType: "user", ScopeID: person,
			Content: item.Content, Metadata: meta, Source: memory.SourceReview,
		})
		if err != nil {
			e.logger.Warn("move personal memory failed", "memory_id", item.ID, "error", err)
			continue
		}
		if _, err := e.memory.Supersede(ctx, ownerID, item.ID, memory.Scope{Type: "team", ID: teamID},
			saved.Memory.ID, "moved to the person's own memory"); err != nil {
			// A pinned group memory stays where the user put it.
			mark(item, "kept")
			continue
		}
		mark(item, "moved")
		moved++
	}
	if moved > 0 {
		e.logger.Info("moved personal facts out of group memory", "team_id", teamID, "moved", moved)
	}
}

// personalCheckBot is the group member whose model judges the memories: the
// group's lead if it can, otherwise any member with memory on.
func (e *Engine) personalCheckBot(ctx context.Context, team store.Team) (store.Bot, bool) {
	rows, err := e.db.ListTeamBots(ctx, team.ID)
	if err != nil {
		return store.Bot{}, false
	}
	lead := parseTeamConfig(team.Config).PrimaryBotID
	var fallback *store.Bot
	for i := range rows {
		bot := rows[i].Bot
		if !bot.MemoryEnabled || bot.ProviderID == nil {
			continue
		}
		if bot.ID.String() == lead {
			return bot, true
		}
		if fallback == nil {
			fallback = &rows[i].Bot
		}
	}
	if fallback == nil {
		return store.Bot{}, false
	}
	return *fallback, true
}

const personalCheckSystem = "你在整理一个群聊的群记忆。群记忆应当只放群里共同的约定、项目事实和分工；" +
	"关于某一个人自己的事实（他的偏好、习惯、身体状况、联系方式、个人经历、私人计划等）应该放回那个人自己的记忆。" +
	"逐条判断下面的群记忆，找出属于个人事实的条目。拿不准的当作群记忆。" +
	"只输出 JSON：{\"personal\": [条目编号, ...]}，没有就输出 {\"personal\": []}。"

// parsePersonalIndexes reads the model's verdict; ok is false when the reply
// holds no verdict at all.
func parsePersonalIndexes(reply string, count int) (map[int]bool, bool) {
	start := strings.Index(reply, "{")
	end := strings.LastIndex(reply, "}")
	if start < 0 || end <= start {
		return nil, false
	}
	var verdict struct {
		Personal []int `json:"personal"`
	}
	if err := json.Unmarshal([]byte(reply[start:end+1]), &verdict); err != nil || verdict.Personal == nil {
		return nil, false
	}
	out := make(map[int]bool, len(verdict.Personal))
	for _, index := range verdict.Personal {
		if index >= 1 && index <= count {
			out[index] = true
		}
	}
	return out, true
}
