package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

type botServiceTokenDTO struct {
	ID         uuid.UUID  `json:"id"`
	BotID      uuid.UUID  `json:"bot_id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"token_prefix"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func toBotServiceTokenDTO(token store.BotServiceToken) botServiceTokenDTO {
	return botServiceTokenDTO{
		ID: token.ID, BotID: token.BotID, Name: token.Name, Prefix: token.TokenPrefix,
		LastUsedAt: token.LastUsedAt, CreatedAt: token.CreatedAt,
	}
}

func hashServiceToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Server) handleListBotServiceTokens(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	role, _ := s.accountRole(r.Context(), accountID, userFrom(r).ID)
	if role != "owner" && role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "only account owners can manage service tokens")
		return
	}
	tokens, err := s.db.ListBotServiceTokens(r.Context(), store.ListBotServiceTokensParams{
		AccountID: accountID, BotID: botID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list service tokens")
		return
	}
	out := make([]botServiceTokenDTO, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, toBotServiceTokenDTO(token))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

func (s *Server) handleCreateBotServiceToken(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	role, _ := s.accountRole(r.Context(), accountID, userFrom(r).ID)
	if role != "owner" && role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "only account owners can manage service tokens")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req)
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create token")
		return
	}
	plaintext := "sbx_" + hex.EncodeToString(buffer)
	createdBy := userFrom(r).ID
	token, err := s.db.CreateBotServiceToken(r.Context(), store.CreateBotServiceTokenParams{
		AccountID: accountID, BotID: botID, Name: strings.TrimSpace(req.Name),
		TokenHash: hashServiceToken(plaintext), TokenPrefix: plaintext[:12],
		CreatedBy: &createdBy,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create token")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token": toBotServiceTokenDTO(token),
		// Shown once; only the hash is stored server-side.
		"secret": plaintext,
	})
}

func (s *Server) handleRevokeBotServiceToken(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	tokenID, ok := parseUUIDParam(w, r, "tokenID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	role, _ := s.accountRole(r.Context(), accountID, userFrom(r).ID)
	if role != "owner" && role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "only account owners can manage service tokens")
		return
	}
	n, err := s.db.RevokeBotServiceToken(r.Context(), store.RevokeBotServiceTokenParams{
		ID: tokenID, AccountID: accountID, BotID: botID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not revoke token")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "token not found")
		return
	}
	writeNoContent(w)
}

// handleServiceBotMessage lets an external system (a channel relay or an
// automation) post a message as the user into a Team Bot conversation. The
// service token is the credential; it is hashed at rest and revocable.
func (s *Server) handleServiceBotMessage(w http.ResponseWriter, r *http.Request) {
	secret := strings.TrimSpace(chi.URLParam(r, "token"))
	if !strings.HasPrefix(secret, "sbx_") {
		writeError(w, http.StatusUnauthorized, "invalid_token", "invalid service token")
		return
	}
	token, err := s.db.GetBotServiceTokenByHash(r.Context(), hashServiceToken(secret))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "invalid service token")
		return
	}
	var req struct {
		Content        string     `json:"content"`
		ConversationID *uuid.UUID `json:"conversation_id"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "content is required")
		return
	}
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: token.BotID, OwnerID: token.AccountID})
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "bot no longer exists")
		return
	}
	userID, err := s.serviceAccountUser(r, token.AccountID, bot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "account has no owner user")
		return
	}
	conversationID := uuid.Nil
	if req.ConversationID != nil {
		conversationID = *req.ConversationID
	} else {
		conv, convErr := s.db.GetDirectConversationByBot(r.Context(), store.GetDirectConversationByBotParams{
			CreatedBy: token.AccountID, DirectBotID: &bot.ID,
		})
		if convErr == nil {
			conversationID = conv.ID
		} else if errors.Is(convErr, pgx.ErrNoRows) {
			created, createErr := s.chat.CreateDirectConversation(r.Context(), token.AccountID, userID, bot.ID, bot.Name)
			if createErr != nil {
				writeError(w, http.StatusInternalServerError, "internal", "could not open conversation")
				return
			}
			conversationID = created.ID
		} else {
			writeError(w, http.StatusInternalServerError, "internal", "could not resolve conversation")
			return
		}
	}
	metadata, _ := json.Marshal(map[string]any{"source": "service_token", "token_id": token.ID.String()})
	result, err := s.chat.SendUserMessage(
		r.Context(), token.AccountID, userID, conversationID,
		req.Content, "", nil, nil, metadata,
	)
	if err != nil {
		writeChatError(w, err, "could not send message")
		return
	}
	_ = s.db.TouchBotServiceToken(r.Context(), token.ID)
	payload := map[string]any{
		"conversation_id": conversationID,
		"message_id":      result.Message.ID,
	}
	if result.Cycle != nil {
		payload["cycle_id"] = result.Cycle.ID
	}
	writeJSON(w, http.StatusCreated, payload)
}

// serviceAccountUser picks the user whose DM receives service messages. Team
// Bots normally carry their creator; fall back to the account owner.
func (s *Server) serviceAccountUser(r *http.Request, accountID uuid.UUID, bot store.Bot) (uuid.UUID, error) {
	if bot.CreatedByUserID != nil {
		return *bot.CreatedByUserID, nil
	}
	members, err := s.db.ListAccountMembers(r.Context(), accountID)
	if err != nil {
		return uuid.Nil, err
	}
	for _, member := range members {
		if member.Role == "owner" {
			return member.ID, nil
		}
	}
	if len(members) > 0 {
		return members[0].ID, nil
	}
	return uuid.Nil, errors.New("no account member")
}
