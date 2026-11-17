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
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

const (
	dims = 1024

	// MinSimilarityEnv overrides the recall threshold with one absolute
	// cosine similarity in [0,1]; it also turns off per-query calibration.
	MinSimilarityEnv = "SUPERBOT_MEMORY_MIN_SIMILARITY"
	// RecallTimeoutEnv is how long (milliseconds) the embedding of a recall
	// query may take before the turn goes on without vector recall.
	RecallTimeoutEnv = "SUPERBOT_MEMORY_RECALL_TIMEOUT_MS"

	defaultRecallTimeout = 2 * time.Second
	// A save may wait longer than a recall: it is one call per tool use, and
	// a vector stored now saves a background pass later.
	saveEmbedTimeout       = 5 * time.Second
	backgroundEmbedTimeout = 30 * time.Second
	// recencyHalfLife is the age at which a memory keeps half its recency
	// bonus when hits are ranked.
	recencyHalfLife = 90 * 24 * time.Hour
	// calibrationRetry is how long a failed load of the calibration vectors
	// waits before it is tried again.
	calibrationRetry = time.Minute
)

// Service stores memory vectors in pgvector. Without an embedding provider it
// falls back to a deterministic local hash so recall still works; when a
// provider is configured every vector carries its embedding space name and
// searches only compare vectors from the same space (mixed spaces would
// silently rank noise).
type Service struct {
	db      *store.DB
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
	space   embeddingSpace

	// minSimilarity < 0 means "use the measured default of the space".
	minSimilarity float64
	recallTimeout time.Duration
	breaker       *breaker
	calibration   calibrationCache
	reembed       reembedState
	now           func() time.Time
}

// calibrationCache holds the unit vectors of calibrationPassages for the
// active space. They are loaded once, off the recall path.
type calibrationCache struct {
	mu       sync.Mutex
	vectors  [][]float32
	loading  bool
	failedAt time.Time
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
	apiKey = strings.TrimSpace(apiKey)
	service := &Service{
		db: db, apiKey: apiKey, baseURL: baseURL, model: model,
		// Every call carries its own deadline; this only bounds a call made
		// with a context that has none.
		client:        &http.Client{Timeout: time.Minute},
		space:         spaceFor(apiKey != "", model),
		minSimilarity: -1,
		recallTimeout: defaultRecallTimeout,
		breaker:       newBreaker(),
		now:           time.Now,
	}
	if value, ok := parseSimilarity(os.Getenv(MinSimilarityEnv)); ok {
		service.minSimilarity = value
	}
	if ms, err := strconv.Atoi(strings.TrimSpace(os.Getenv(RecallTimeoutEnv))); err == nil && ms > 0 {
		service.recallTimeout = time.Duration(ms) * time.Millisecond
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

// MinSimilarity is the absolute cosine similarity a memory must reach to be
// recalled when per-query calibration is not in use.
func (s *Service) MinSimilarity() float64 {
	if s.minSimilarity >= 0 {
		return s.minSimilarity
	}
	return s.space.RecallMin
}

func (s *Service) calibrated() bool {
	return s.minSimilarity < 0 && s.space.RecallDelta > 0
}

func (s *Service) Configured() bool { return s.apiKey != "" }

// ModelName is the value stored in memories.embedding_model for new vectors.
// It names the embedding space, including how inputs are prefixed.
func (s *Service) ModelName() string { return s.space.Name }

// EmbeddingAvailable reports whether the embedding service is believed up.
func (s *Service) EmbeddingAvailable() bool {
	return !s.Configured() || s.breaker.Allow()
}

type inputKind int

const (
	kindPassage inputKind = iota
	kindQuery
)

func (s *Service) prefixed(text string, kind inputKind) string {
	if kind == kindQuery {
		return s.space.QueryPrefix + text
	}
	return s.space.PassagePrefix + text
}

// EmbedText returns the pgvector literal of a memory text (a passage) in the
// current vector space.
func (s *Service) EmbedText(ctx context.Context, text string) (string, error) {
	vector, err := s.embedVector(ctx, text, kindPassage, saveEmbedTimeout)
	if err != nil {
		return "", err
	}
	return vectorLiteral(vector), nil
}

// embedVector returns the unit vector of text in the active space.
func (s *Service) embedVector(ctx context.Context, text string, kind inputKind, timeout time.Duration) ([]float32, error) {
	if !s.Configured() {
		return Embed(text), nil
	}
	vectors, err := s.embedRemote(ctx, []string{s.prefixed(text, kind)}, timeout)
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

// embedRemote calls the provider through the circuit breaker. Only failures
// that say something about the service count against it: a caller that gave
// up, or a request the provider rejected as invalid, does not.
func (s *Service) embedRemote(ctx context.Context, inputs []string, timeout time.Duration) ([][]float32, error) {
	if !s.breaker.Allow() {
		return nil, ErrEmbeddingUnavailable
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	vectors, err := s.requestEmbeddings(callCtx, inputs)
	switch {
	case err == nil:
		s.breaker.Success()
	case ctx.Err() != nil:
	case isOutage(err):
		s.breaker.Failure()
	}
	return vectors, err
}

type embeddingRequest struct {
	Model string `json:"model"`
	// Input is a string for one text and an array for a batch, as the
	// OpenAI embeddings API accepts both.
	Input any `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// apiStatusError is a non-2xx answer of the embedding API.
type apiStatusError struct {
	status int
	text   string
}

func (e *apiStatusError) Error() string {
	return fmt.Sprintf("embedding API %d %s: %s", e.status, http.StatusText(e.status), e.text)
}

// isOutage separates "the service is down or overloaded" from "this request
// was refused" (a 4xx other than timeouts and rate limits).
func isOutage(err error) bool {
	var status *apiStatusError
	if errors.As(err, &status) {
		return status.status >= 500 || status.status == http.StatusTooManyRequests || status.status == http.StatusRequestTimeout
	}
	return true
}

func (s *Service) requestEmbeddings(ctx context.Context, inputs []string) ([][]float32, error) {
	payload := embeddingRequest{Model: s.model, Input: inputs}
	if len(inputs) == 1 {
		payload.Input = inputs[0]
	}
	body, _ := json.Marshal(payload)
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
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 64<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &apiStatusError{status: response.StatusCode, text: strings.TrimSpace(string(raw))}
	}
	var parsed embeddingResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, errors.New(parsed.Error.Message)
	}
	if len(parsed.Data) != len(inputs) {
		if len(parsed.Data) == 0 {
			return nil, errors.New("embedding API returned no vector")
		}
		return nil, fmt.Errorf("embedding API returned %d vectors for %d inputs", len(parsed.Data), len(inputs))
	}
	out := make([][]float32, len(inputs))
	for i, item := range parsed.Data {
		index := i
		if len(inputs) > 1 && item.Index >= 0 && item.Index < len(inputs) {
			index = item.Index
		}
		if len(item.Embedding) != dims {
			return nil, fmt.Errorf("embedding dimension %d does not match the memory column (%d)", len(item.Embedding), dims)
		}
		out[index] = normalize(item.Embedding)
	}
	for _, vector := range out {
		if vector == nil {
			return nil, errors.New("embedding API returned an incomplete batch")
		}
	}
	return out, nil
}

// WarmCalibration loads the calibration vectors now. The worker calls it from
// its maintenance pass so the first recall does not run uncalibrated.
func (s *Service) WarmCalibration(ctx context.Context) error {
	if !s.Configured() || s.space.RecallDelta <= 0 {
		return nil
	}
	s.calibration.mu.Lock()
	if s.calibration.vectors != nil || s.calibration.loading {
		s.calibration.mu.Unlock()
		return nil
	}
	s.calibration.loading = true
	s.calibration.mu.Unlock()

	inputs := make([]string, len(calibrationPassages))
	for i, text := range calibrationPassages {
		inputs[i] = s.prefixed(text, kindPassage)
	}
	vectors, err := s.embedRemote(ctx, inputs, backgroundEmbedTimeout)

	s.calibration.mu.Lock()
	defer s.calibration.mu.Unlock()
	s.calibration.loading = false
	if err != nil {
		s.calibration.failedAt = s.now()
		return err
	}
	s.calibration.vectors = vectors
	return nil
}

// calibrationVectors returns the loaded calibration vectors, or nil while
// they are not available; in that case it starts loading them in the
// background (at most once a minute after a failure).
func (s *Service) calibrationVectors() [][]float32 {
	s.calibration.mu.Lock()
	vectors := s.calibration.vectors
	start := vectors == nil && !s.calibration.loading &&
		(s.calibration.failedAt.IsZero() || s.now().Sub(s.calibration.failedAt) >= calibrationRetry)
	s.calibration.mu.Unlock()
	if start && s.breaker.Allow() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), backgroundEmbedTimeout)
			defer cancel()
			_ = s.WarmCalibration(ctx)
		}()
	}
	return vectors
}

// recallThreshold is the similarity a memory needs to be recalled for this
// query vector: the query's background plus the space's delta when
// calibration is available, the absolute threshold otherwise.
func (s *Service) recallThreshold(query []float32) float64 {
	if !s.calibrated() {
		return s.MinSimilarity()
	}
	refs := s.calibrationVectors()
	if refs == nil {
		return s.space.RecallMin
	}
	return max(s.space.RecallFloor, background(query, refs)+s.space.RecallDelta)
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

func (s *Service) searchScored(ctx context.Context, q *store.Queries, ownerID uuid.UUID, scope Scope, vector string, minSimilarity float64, limit int32) ([]scoredMemory, error) {
	rows, err := q.SearchMemoriesScored(ctx, store.SearchMemoriesScoredParams{
		OwnerID: ownerID, ScopeType: scope.Type, ScopeID: scope.ID,
		Embedding: vector, EmbeddingModel: s.ModelName(),
		MinSimilarity: minSimilarity, MaxResults: limit,
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
// threshold, best first.
func (s *Service) Search(ctx context.Context, ownerID uuid.UUID, scopeType string, scopeID uuid.UUID, query string, limit int32) ([]store.Memory, error) {
	if limit <= 0 || limit > 50 {
		limit = 6
	}
	vector, err := s.embedVector(ctx, query, kindQuery, s.recallTimeout)
	if err != nil {
		return nil, err
	}
	scored, err := s.searchScored(ctx, s.db.Queries, ownerID, Scope{Type: scopeType, ID: scopeID}, vectorLiteral(vector), s.recallThreshold(vector), limit)
	if err != nil {
		return nil, err
	}
	return rankRecall(scored, int(limit), s.rankParams()), nil
}

// rankParams carries what ranking needs besides the similarities.
type rankParams struct {
	margin       float64
	pinnedBonus  float64
	recencyBonus float64
	now          time.Time
}

func (s *Service) rankParams() rankParams {
	return rankParams{
		margin: s.space.Margin, pinnedBonus: s.space.PinnedBonus,
		recencyBonus: s.space.RecencyBonus, now: s.now(),
	}
}

// rankScore adds the pinned and recency bonuses to a similarity. A memory the
// user pinned, or one written or confirmed recently, wins a near tie.
func (p rankParams) rankScore(item scoredMemory) float64 {
	score := item.similarity
	if item.memory.Pinned {
		score += p.pinnedBonus
	}
	if p.recencyBonus > 0 && !item.memory.UpdatedAt.IsZero() {
		age := p.now.Sub(item.memory.UpdatedAt)
		if age < 0 {
			age = 0
		}
		score += p.recencyBonus * math.Pow(0.5, float64(age)/float64(recencyHalfLife))
	}
	return score
}

// rankRecall merges per-scope hits: one entry per memory, best first, and
// drops hits that trail the best one by more than the margin.
func rankRecall(scored []scoredMemory, limit int, params rankParams) []store.Memory {
	type ranked struct {
		item  scoredMemory
		score float64
	}
	list := make([]ranked, 0, len(scored))
	for _, item := range scored {
		list = append(list, ranked{item: item, score: params.rankScore(item)})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].score > list[j].score })
	seen := make(map[uuid.UUID]bool, len(list))
	out := make([]store.Memory, 0, min(limit, len(list)))
	for _, entry := range list {
		if len(out) >= limit || entry.score < list[0].score-params.margin {
			break
		}
		if seen[entry.item.memory.ID] {
			continue
		}
		seen[entry.item.memory.ID] = true
		out = append(out, entry.item.memory)
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
// the vector path finds none (or the embedding service is slow or down, which
// costs at most the short recall timeout and then nothing while the breaker
// is open), a small keyword match is tried instead so an exact term still
// recalls a memory.
func (s *Service) Recall(ctx context.Context, ownerID, botID uuid.UUID, teamID *uuid.UUID, userID *uuid.UUID, query string, limit int32) []store.Memory {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	if limit <= 0 || limit > 50 {
		limit = 6
	}
	scopes := recallScopes(botID, teamID, userID)
	if vector, err := s.embedVector(ctx, query, kindQuery, s.recallTimeout); err == nil {
		literal := vectorLiteral(vector)
		threshold := s.recallThreshold(vector)
		var scored []scoredMemory
		for _, scope := range scopes {
			if items, err := s.searchScored(ctx, s.db.Queries, ownerID, scope, literal, threshold, limit); err == nil {
				scored = append(scored, items...)
			}
		}
		if out := rankRecall(scored, int(limit), s.rankParams()); len(out) > 0 {
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

// UpdateInScope edits a memory of one scope. The old vector is dropped with
// the old text; the new one is stored now, or by the background re-embed if
// the embedding service is unavailable (pending is then true).
func (s *Service) UpdateInScope(ctx context.Context, ownerID, memoryID uuid.UUID, scope Scope, content string) (item store.Memory, pending bool, err error) {
	content, err = validateContent(content)
	if err != nil {
		return store.Memory{}, false, err
	}
	item, err = s.db.UpdateScopedMemory(ctx, store.UpdateScopedMemoryParams{
		ID: memoryID, OwnerID: ownerID, ScopeType: scope.Type, ScopeID: scope.ID, Content: content,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Memory{}, false, ErrNotFound
	}
	if err != nil {
		return store.Memory{}, false, err
	}
	item.Embedding = nil
	vector, embedErr := s.embedVector(ctx, content, kindPassage, saveEmbedTimeout)
	if embedErr != nil {
		return item, true, nil
	}
	if _, err := s.db.SetMemoryEmbeddingForContent(ctx, store.SetMemoryEmbeddingForContentParams{
		ID: item.ID, OwnerID: ownerID, Content: content,
		Embedding: vectorLiteral(vector), EmbeddingModel: s.ModelName(),
	}); err != nil {
		return item, true, nil
	}
	item.EmbeddingModel = s.ModelName()
	return item, false, nil
}

// DeleteInScope removes a memory of one scope for good.
func (s *Service) DeleteInScope(ctx context.Context, ownerID, memoryID uuid.UUID, scope Scope) error {
	n, err := s.db.DeleteScopedMemory(ctx, store.DeleteScopedMemoryParams{
		ID: memoryID, OwnerID: ownerID, ScopeType: scope.Type, ScopeID: scope.ID,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUser edits a user-scoped memory and refreshes its embedding.
func (s *Service) UpdateUser(ctx context.Context, ownerID, memoryID, userID uuid.UUID, content string) (store.Memory, error) {
	item, _, err := s.UpdateInScope(ctx, ownerID, memoryID, Scope{Type: "user", ID: userID}, content)
	return item, err
}

// DeleteUser removes a user-scoped memory.
func (s *Service) DeleteUser(ctx context.Context, ownerID, memoryID, userID uuid.UUID) error {
	return s.DeleteInScope(ctx, ownerID, memoryID, Scope{Type: "user", ID: userID})
}

// Update edits a bot-scoped memory and refreshes its embedding so search keeps
// matching the new text.
func (s *Service) Update(ctx context.Context, ownerID, memoryID, botID uuid.UUID, content string) (store.Memory, error) {
	item, _, err := s.UpdateInScope(ctx, ownerID, memoryID, Scope{Type: "bot", ID: botID}, content)
	return item, err
}

// Delete removes a bot-scoped memory.
func (s *Service) Delete(ctx context.Context, ownerID, memoryID, botID uuid.UUID) error {
	return s.DeleteInScope(ctx, ownerID, memoryID, Scope{Type: "bot", ID: botID})
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
