package goals

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"superbot/internal/store"
)

// Roster is who takes part in a team conversation's goal and who leads it.
type Roster struct {
	Members []Member
	LeadID  uuid.UUID
}

func (r Roster) IsLead(botID uuid.UUID) bool { return r.LeadID == botID }

// LoadRoster resolves the members of a team conversation. The lead is the
// team's configured primary bot, else the account's primary bot when it is a
// member, else the first member — the same order group chats use to pick who
// answers an unaddressed message.
func LoadRoster(ctx context.Context, db *store.DB, conv store.Conversation) (Roster, error) {
	if conv.TeamID == nil {
		return Roster{}, fmt.Errorf("conversation is not a team conversation")
	}
	team, err := db.GetTeam(ctx, store.GetTeamParams{ID: *conv.TeamID, OwnerID: conv.CreatedBy})
	if err != nil {
		return Roster{}, fmt.Errorf("load team: %w", err)
	}
	teamBots, err := db.ListTeamBots(ctx, team.ID)
	if err != nil {
		return Roster{}, fmt.Errorf("load team bots: %w", err)
	}
	if len(teamBots) == 0 {
		return Roster{}, fmt.Errorf("team has no bots")
	}
	roster := Roster{Members: make([]Member, 0, len(teamBots))}
	ids := make(map[string]uuid.UUID, len(teamBots))
	for _, tb := range teamBots {
		roster.Members = append(roster.Members, Member{ID: tb.Bot.ID.String(), Name: tb.Bot.Name})
		ids[tb.Bot.ID.String()] = tb.Bot.ID
	}

	var cfg struct {
		PrimaryBotID string `json:"primary_bot_id"`
	}
	_ = json.Unmarshal(team.Config, &cfg)
	if id, ok := ids[cfg.PrimaryBotID]; ok {
		roster.LeadID = id
		return roster, nil
	}
	if owner, err := db.GetUserByID(ctx, conv.CreatedBy); err == nil && owner.PrimaryBotID != nil {
		if id, ok := ids[owner.PrimaryBotID.String()]; ok {
			roster.LeadID = id
			return roster, nil
		}
	}
	roster.LeadID = teamBots[0].Bot.ID
	return roster, nil
}
