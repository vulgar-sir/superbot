package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"superbot/internal/store"
)

const (
	dims       = 1024
	localModel = "local-hash-1024"

	// MinSimilarityEnv overrides the recall threshold of whichever embedding
	// space is active (a cosine similarity in [0,1]).
	MinSimilarityEnv = "SUPERBOT_MEMORY_MIN_SIMILARITY"

	// Real embedding models put loosely related text around 0.3-0.5 and
	// on-topic text above ~0.55.
	remoteMinSimilarity = 0.55
	// The E5 family compresses every score into a narrow high band.
	// Measured on the deployed multilingual-e5-large with Chinese memories:
	// unrelated pairs 0.797-0.872, related pairs 0.888-0.934. At 0.55 every
	// memory was recalled for every question.
	e5MinSimilarity = 0.88
	// recallMargin drops hits that trail the best one by more than this,
	// so a weakly related memory does not ride along with a strong match.
	recallMargin = 0.05
	// The local hash embedder is lexical: it only measures shared words and
	// character bigrams, so its scale is much lower. Unrelated sentences score
	// 0.00-0.10 (hash collisions, shared function words), sentences sharing a
	// real term score from ~0.15 (short Chinese) to 0.3-0.6 (English). 0.12
	// sits just above the noise floor; see TestLocalHashThreshold.
	localMinSimilarity = 0.12
)

// Service stores memory vectors in pgvector. Without an embedding provider it
// falls back to a deterministic local hash so recall still works; when a
// provider is configured every vector carries its model name and searches only
// compare vectors from the same space (mixed spaces would silently rank noise).
type Service struct {
	db      *store.DB
	apiKey  string
	baseURL string
	model   string
	client  *http.Client

	// minSimilarity < 0 means "use the default of the active vector space".
	minSimilarity float64
}

func NewService(db *store.DB, apiKey, baseURL, model string) *Service {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "text-embedding-3-small"
	}
	service := &Service{
		db: db, apiKey: strings.TrimSpace(apiKey), baseURL: baseURL, model: model,
		client:        &http.Client{Timeout: 30 * time.Second},
		minSimilarity: -1,
	}
	if value, ok := parseSimilarity(os.Getenv(MinSimilarityEnv)); ok {
		service.minSimilarity = value
	}
	return service
}

func parseSimilarity(raw string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || value < 0 || value > 1 {
		return 0, false
	}
	return value, true
}

// SetMinSimilarity overrides the recall threshold; a negative value restores
// the default of the active embedding space.
func (s *Service) SetMinSimilarity(value float64) {
	if value > 1 {
		value = 1
	}
	s.minSimilarity = value
}

// MinSimilarity is the cosine similarity a memory must reach to be recalled.
func (s *Service) MinSimilarity() float64 {
	if s.minSimilarity >= 0 {
		return s.minSimilarity
	}
	return defaultMinSimilarity(s.ModelName())
}

func defaultMinSimilarity(modelName string) float64 {
	if strings.HasPrefix(modelName, "local-hash-") {
		return localMinSimilarity
	}
	if strings.Contains(strings.ToLower(modelName), "e5") {
		return e5MinSimilarity
	}
	return remoteMinSimilarity
}

func (s *Service) Configured() bool { return s.apiKey != "" }

// ModelName is the value stored in memories.embedding_model for new vectors.
func (s *Service) ModelName() string {
	if s.Configured() {
		return "remote:" + s.model
	}
	return localModel
}

// EmbedText returns the pgvector literal for text in the current vector space.
func (s *Service) EmbedText(ctx context.Context, text string) (string, error) {
	if !s.Configured() {
		return VectorText(text), nil
	}
	vector, err := s.embedRemote(ctx, text)
	if err != nil {
		return "", err
	}
	return vectorLiteral(vector), nil
}

type embeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (s *Service) embedRemote(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(embeddingRequest{Model: s.model, Input: text})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+s.apiKey)
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding API %s: %s", response.Status, strings.TrimSpace(string(raw)))
	}
	var parsed embeddingResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, errors.New(parsed.Error.Message)
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, errors.New("embedding API returned no vector")
	}
	vector := parsed.Data[0].Embedding
	if len(vector) != dims {
		return nil, fmt.Errorf("embedding dimension %d does not match the memory column (%d)", len(vector), dims)
	}
	return normalize(vector), nil
}

// Save stores a manually entered memory. It goes through the same secret
// check and duplicate detection as SaveWith.
func (s *Service) Save(ctx context.Context, ownerID uuid.UUID, scopeType string, scopeID uuid.UUID, content string, metadata json.RawMessage) (store.Memory, error) {
	result, err := s.SaveWith(ctx, SaveInput{
		OwnerID: ownerID, ScopeType: scopeType, ScopeID: scopeID, Content: content, Metadata: metadata,
	})
	return result.Memory, err
}

type scoredMemory struct {
	memory     store.Memory
	similarity float64
}

func (s *Service) searchScored(ctx context.Context, ownerID uuid.UUID, scope Scope, vector string, limit int32) ([]scoredMemory, error) {
	rows, err := s.db.SearchMemoriesScored(ctx, store.SearchMemoriesScoredParams{
		OwnerID: ownerID, ScopeType: scope.Type, ScopeID: scope.ID,
		Embedding: vector, EmbeddingModel: s.ModelName(),
		MinSimilarity: s.MinSimilarity(), MaxResults: limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]scoredMemory, 0, len(rows))
	for _, row := range rows {
		row.Memory.Embedding = nil
		out = append(out, scoredMemory{memory: row.Memory, similarity: row.Similarity})
	}
	return out, nil
}

// Search returns the memories of one scope that clear the relevance
// threshold, most similar first.
func (s *Service) Search(ctx context.Context, ownerID uuid.UUID, scopeType string, scopeID uuid.UUID, query string, limit int32) ([]store.Memory, error) {
	if limit <= 0 || limit > 50 {
		limit = 6
	}
	vector, err := s.EmbedText(ctx, query)
	if err != nil {
		return nil, err
	}
	scored, err := s.searchScored(ctx, ownerID, Scope{Type: scopeType, ID: scopeID}, vector, limit)
	if err != nil {
		return nil, err
	}
	return rankRecall(scored, int(limit)), nil
}

// rankRecall merges per-scope hits: one entry per memory, most similar first.
func rankRecall(scored []scoredMemory, limit int) []store.Memory {
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].similarity > scored[j].similarity })
	seen := make(map[uuid.UUID]bool, len(scored))
	out := make([]store.Memory, 0, min(limit, len(scored)))
	for _, item := range scored {
		if len(out) >= limit || item.similarity < scored[0].similarity-recallMargin {
			break
		}
		if seen[item.memory.ID] {
			continue
		}
		seen[item.memory.ID] = true
		out = append(out, item.memory)
	}
	return out
}

func recallScopes(botID uuid.UUID, teamID, userID *uuid.UUID) []Scope {
	scopes := []Scope{{Type: "bot", ID: botID}}
	if userID != nil {
		scopes = append(scopes, Scope{Type: "user", ID: *userID})
	}
	if teamID != nil {
		scopes = append(scopes, Scope{Type: "team", ID: *teamID})
	}
	return scopes
}

const (
	keywordFallbackLimit = 3
	// pg_trgm's own default cut-off; the former 0.05 matched almost anything.
	keywordMinSimilarity = 0.3
	keywordQueryRunes    = 200
)

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// keywordQuery prepares a user message for the trigram/ILIKE fallback. It
// reports false for text too short to be a meaningful keyword.
func keywordQuery(query string) (text, pattern string, ok bool) {
	runes := []rune(strings.TrimSpace(query))
	if len(runes) < 2 {
		return "", "", false
	}
	if len(runes) > keywordQueryRunes {
		runes = runes[:keywordQueryRunes]
	}
	text = string(runes)
	return text, likeEscaper.Replace(text), true
}

// Recall returns the memories relevant to query across the bot, user and
// team scopes. Only hits above the similarity threshold are returned; when
// the vector path finds none, a small keyword match is tried instead so an
// exact term still recalls a memory the embedding missed.
func (s *Service) Recall(ctx context.Context, ownerID, botID uuid.UUID, teamID *uuid.UUID, userID *uuid.UUID, query string, limit int32) []store.Memory {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	if limit <= 0 || limit > 50 {
		limit = 6
	}
	scopes := recallScopes(botID, teamID, userID)
	if vector, err := s.EmbedText(ctx, query); err == nil {
		var scored []scoredMemory
		for _, scope := range scopes {
			if items, err := s.searchScored(ctx, ownerID, scope, vector, limit); err == nil {
				scored = append(scored, items...)
			}
		}
		if out := rankRecall(scored, int(limit)); len(out) > 0 {
			return out
		}
	}
	text, pattern, ok := keywordQuery(query)
	if !ok {
		return nil
	}
	out := make([]store.Memory, 0, keywordFallbackLimit)
	seen := make(map[uuid.UUID]bool)
	for _, scope := range scopes {
		items, err := s.db.SearchMemories(ctx, store.SearchMemoriesParams{
			OwnerID: ownerID, ScopeType: scope.Type, ScopeID: scope.ID,
			Pattern: pattern, Query: text,
			MinSimilarity: keywordMinSimilarity, MaxResults: keywordFallbackLimit,
		})
		if err != nil {
			continue
		}
		for _, item := range items {
			if len(out) >= min(keywordFallbackLimit, int(limit)) {
				return out
			}
			if !seen[item.ID] {
				seen[item.ID] = true
				item.Embedding = nil
				out = append(out, item)
			}
		}
	}
	return out
}

// UpdateUser edits a user-scoped memory and refreshes its embedding.
func (s *Service) UpdateUser(ctx context.Context, ownerID, memoryID, userID uuid.UUID, content string) (store.Memory, error) {
	content, err := validateContent(content)
	if err != nil {
		return store.Memory{}, err
	}
	item, err := s.db.UpdateUserMemory(ctx, store.UpdateUserMemoryParams{
		ID: memoryID, OwnerID: ownerID, ScopeID: userID, Content: content,
	})
	if err != nil {
		return store.Memory{}, err
	}
	vector, err := s.EmbedText(ctx, content)
	if err != nil {
		return item, err
	}
	if err := s.db.SetMemoryEmbedding(ctx, store.SetMemoryEmbeddingParams{
		ID: item.ID, OwnerID: ownerID, Embedding: vector, EmbeddingModel: s.ModelName(),
	}); err != nil {
		return item, err
	}
	item.Embedding = nil
	return item, nil
}

// DeleteUser removes a user-scoped memory.
func (s *Service) DeleteUser(ctx context.Context, ownerID, memoryID, userID uuid.UUID) error {
	n, err := s.db.DeleteUserMemory(ctx, store.DeleteUserMemoryParams{
		ID: memoryID, OwnerID: ownerID, ScopeID: userID,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("memory not found")
	}
	return nil
}

// Update edits a bot-scoped memory and refreshes its embedding so search keeps
// matching the new text.
func (s *Service) Update(ctx context.Context, ownerID, memoryID, botID uuid.UUID, content string) (store.Memory, error) {
	content, err := validateContent(content)
	if err != nil {
		return store.Memory{}, err
	}
	item, err := s.db.UpdateBotMemory(ctx, store.UpdateBotMemoryParams{
		ID: memoryID, OwnerID: ownerID, ScopeID: botID, Content: content,
	})
	if err != nil {
		return store.Memory{}, err
	}
	vector, err := s.EmbedText(ctx, content)
	if err != nil {
		return item, err
	}
	if err := s.db.SetMemoryEmbedding(ctx, store.SetMemoryEmbeddingParams{
		ID: item.ID, OwnerID: ownerID, Embedding: vector, EmbeddingModel: s.ModelName(),
	}); err != nil {
		return item, err
	}
	item.Embedding = nil
	return item, nil
}

// Delete removes a bot-scoped memory.
func (s *Service) Delete(ctx context.Context, ownerID, memoryID, botID uuid.UUID) error {
	n, err := s.db.DeleteBotMemory(ctx, store.DeleteBotMemoryParams{
		ID: memoryID, OwnerID: ownerID, ScopeID: botID,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("memory not found")
	}
	return nil
}

// VectorText returns a pgvector text literal like [0.1,0.2,...].
func VectorText(text string) string {
	return vectorLiteral(Embed(text))
}

func vectorLiteral(vector []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, value := range vector {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%.6f", value)
	}
	b.WriteByte(']')
	return b.String()
}

func Embed(text string) []float32 {
	vector := make([]float32, dims)
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, token := range tokenize(lower) {
		add(vector, token, 1)
	}
	runes := []rune(lower)
	for i := 0; i+1 < len(runes); i++ {
		if unicode.IsSpace(runes[i]) || unicode.IsSpace(runes[i+1]) {
			continue
		}
		add(vector, string(runes[i:i+2]), 0.5)
	}
	var norm float64
	for _, value := range vector {
		norm += float64(value * value)
	}
	if norm == 0 {
		vector[0] = 1
		return vector
	}
	scale := float32(1 / math.Sqrt(norm))
	for i := range vector {
		vector[i] *= scale
	}
	return vector
}

func tokenize(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func add(vector []float32, token string, weight float32) {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(token))
	value := hash.Sum64()
	index := int(value % dims)
	sign := float32(1)
	if value&(1<<63) != 0 {
		sign = -1
	}
	vector[index] += sign * weight
}

// normalize scales a vector to unit length so cosine distance is stable even
// when a provider returns unnormalized embeddings.
func normalize(vector []float32) []float32 {
	var norm float64
	for _, value := range vector {
		norm += float64(value * value)
	}
	if norm == 0 {
		out := make([]float32, len(vector))
		out[0] = 1
		return out
	}
	scale := float32(1 / math.Sqrt(norm))
	out := make([]float32, len(vector))
	for i, value := range vector {
		out[i] = value * scale
	}
	return out
}
