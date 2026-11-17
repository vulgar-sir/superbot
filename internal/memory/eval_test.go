package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

const (
	recordedE5File = "testdata/recall_eval_e5.json"
	e5EvalModel    = "intfloat/multilingual-e5-large"
)

// recordedEval holds scores the deployed multilingual-e5-large gave the
// bundled set: Current with the E5 input prefixes, Legacy without them (how
// vectors were made before e5-prefix-v1). Re-record with
// SUPERBOT_MEMORY_EVAL_URL (or _VECTORS) and SUPERBOT_MEMORY_EVAL_RECORD.
type recordedEval struct {
	Model   string     `json:"model"`
	Current EvalScores `json:"current"`
	Legacy  EvalScores `json:"legacy"`
}

// legacyE5Space is how E5 was used before e5-prefix-v1: bare inputs and the
// 0.88 cut-off, 0.92 merge and 0.95 tidy thresholds.
var legacyE5Space = embeddingSpace{
	Name: "remote:" + e5EvalModel, RecallMin: 0.88, Margin: 0.05,
	RelatedMin: 0.89, MergeMin: 0.92, TidyMin: 0.95,
}

func loadRecordedE5(t *testing.T) recordedEval {
	t.Helper()
	raw, err := os.ReadFile(recordedE5File)
	if err != nil {
		t.Fatalf("read %s: %v", recordedE5File, err)
	}
	var recorded recordedEval
	if err := json.Unmarshal(raw, &recorded); err != nil {
		t.Fatalf("parse %s: %v", recordedE5File, err)
	}
	return recorded
}

func TestEvalSetIsWellFormed(t *testing.T) {
	set, err := DefaultEvalSet()
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, item := range set.Memories {
		if known[item.ID] || strings.TrimSpace(item.Content) == "" {
			t.Fatalf("memory %q is duplicated or empty", item.ID)
		}
		known[item.ID] = true
	}
	negatives := 0
	for _, query := range set.Queries {
		for _, id := range append(append([]string{}, query.Relevant...), query.Acceptable...) {
			if !known[id] {
				t.Fatalf("query %s points at unknown memory %s", query.ID, id)
			}
		}
		if len(query.Relevant) == 0 {
			negatives++
		}
	}
	if len(set.Queries) < 40 || negatives < 8 {
		t.Fatalf("the set needs at least 40 queries and 8 without a relevant memory, has %d / %d", len(set.Queries), negatives)
	}
	kinds := map[string]int{}
	for _, pair := range set.Pairs {
		kinds[pair.Kind]++
	}
	for _, kind := range []string{"duplicate", "update", "sibling", "unrelated"} {
		if kinds[kind] < 4 {
			t.Fatalf("the set needs at least 4 %s pairs, has %d", kind, kinds[kind])
		}
	}
}

// The thresholds of the E5 space must keep their quality on the scores the
// deployed model gave the set, and must beat the way E5 was used before.
func TestRecallEvalE5Recorded(t *testing.T) {
	set, err := DefaultEvalSet()
	if err != nil {
		t.Fatal(err)
	}
	recorded := loadRecordedE5(t)
	space := e5Space(recorded.Model)
	if recorded.Current.Space != space.Name {
		t.Fatalf("recorded scores are for %q but the E5 space is now %q: re-record them", recorded.Current.Space, space.Name)
	}
	current := EvaluateScores(set, recorded.Current, space, true)
	legacy := EvaluateScores(set, recorded.Legacy, legacyE5Space, false)
	t.Logf("current:\n%s", current)
	t.Logf("legacy:\n%s", legacy)
	floor, ok := EvalFloorFor(space.Name)
	if !ok {
		t.Fatal("the E5 space has no floor")
	}
	if problems := floor.Check(current); len(problems) > 0 {
		t.Fatalf("E5 recall quality regressed: %s", strings.Join(problems, "; "))
	}
	if current.F1() <= legacy.F1() {
		t.Fatalf("current F1 %.3f does not beat the legacy %.3f", current.F1(), legacy.F1())
	}
	if legacy.MergeErrors == 0 {
		t.Log("note: the legacy merge threshold no longer merges different facts on this set")
	}
}

func TestRecallEvalLocalHash(t *testing.T) {
	set, err := DefaultEvalSet()
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, "", "", "")
	scores, err := ComputeEvalScores(context.Background(), set, service.ModelName(), service.EvalEmbedder())
	if err != nil {
		t.Fatal(err)
	}
	report := EvaluateScores(set, scores, localSpace, false)
	t.Logf("\n%s", report)
	floor, _ := EvalFloorFor(localModel)
	if problems := floor.Check(report); len(problems) > 0 {
		t.Fatalf("local hash quality regressed: %s", strings.Join(problems, "; "))
	}
}

// TestRecallEvalLive embeds the set with a real embedding service:
//
//	SUPERBOT_MEMORY_EVAL_URL=http://embedding:8090 [SUPERBOT_MEMORY_EVAL_MODEL=...]
//
// or replays vectors fetched earlier (a JSON object from the exact input text,
// prefix included, to its vector):
//
//	SUPERBOT_MEMORY_EVAL_VECTORS=vectors.json
//
// SUPERBOT_MEMORY_EVAL_RECORD=testdata/recall_eval_e5.json writes the scores
// TestRecallEvalE5Recorded checks.
func TestRecallEvalLive(t *testing.T) {
	url, cache := os.Getenv("SUPERBOT_MEMORY_EVAL_URL"), os.Getenv("SUPERBOT_MEMORY_EVAL_VECTORS")
	if url == "" && cache == "" {
		t.Skip("set SUPERBOT_MEMORY_EVAL_URL or SUPERBOT_MEMORY_EVAL_VECTORS to evaluate a real embedding model")
	}
	model := os.Getenv("SUPERBOT_MEMORY_EVAL_MODEL")
	if model == "" {
		model = e5EvalModel
	}
	key := os.Getenv("SUPERBOT_MEMORY_EVAL_KEY")
	if key == "" {
		key = "eval"
	}
	set, err := DefaultEvalSet()
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, key, url, model)
	embed := service.EvalEmbedder()
	legacyEmbed := func(ctx context.Context, text string, _ bool) ([]float32, error) {
		vectors, err := service.embedRemote(ctx, []string{text}, backgroundEmbedTimeout)
		if err != nil {
			return nil, err
		}
		return vectors[0], nil
	}
	if cache != "" {
		vectors := loadVectorCache(t, cache)
		embed = func(_ context.Context, text string, query bool) ([]float32, error) {
			input := service.space.PassagePrefix + text
			if query {
				input = service.space.QueryPrefix + text
			}
			return vectors.get(input)
		}
		legacyEmbed = func(_ context.Context, text string, _ bool) ([]float32, error) { return vectors.get(text) }
	}
	ctx := context.Background()
	current, err := ComputeEvalScores(ctx, set, service.ModelName(), embed)
	if err != nil {
		t.Fatal(err)
	}
	report := EvaluateScores(set, current, service.space, service.calibrated())
	t.Logf("\n%s", report)
	fmt.Println(report)

	if isE5Model(model) {
		legacy, err := ComputeEvalScores(ctx, set, legacyE5Space.Name, legacyEmbed)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("legacy (bare inputs, old thresholds):\n%s\n", EvaluateScores(set, legacy, legacyE5Space, false))
		if path := os.Getenv("SUPERBOT_MEMORY_EVAL_RECORD"); path != "" {
			if err := os.WriteFile(path, encodeRecorded(recordedEval{Model: model, Current: current, Legacy: legacy}), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if floor, ok := EvalFloorFor(service.ModelName()); ok {
		if problems := floor.Check(report); len(problems) > 0 {
			t.Fatalf("quality below the floor: %s", strings.Join(problems, "; "))
		}
	}
}

// encodeRecorded writes one line per query so the file diffs readably.
func encodeRecorded(r recordedEval) []byte {
	compact := func(value any) string {
		raw, _ := json.Marshal(value)
		return string(raw)
	}
	scores := func(s EvalScores) string {
		set, _ := DefaultEvalSet()
		lines := make([]string, 0, len(set.Queries))
		for _, query := range set.Queries {
			lines = append(lines, fmt.Sprintf("   %q: %s", query.ID, compact(s.Queries[query.ID])))
		}
		return fmt.Sprintf("{\n  \"space\": %s,\n  \"backgrounds\": %s,\n  \"pairs\": %s,\n  \"queries\": {\n%s\n  }\n }",
			compact(s.Space), compact(s.Backgrounds), compact(s.Pairs), strings.Join(lines, ",\n"))
	}
	return []byte(fmt.Sprintf("{\n \"model\": %s,\n \"current\": %s,\n \"legacy\": %s\n}\n",
		compact(r.Model), scores(r.Current), scores(r.Legacy)))
}

type vectorCache map[string][]float32

func loadVectorCache(t *testing.T, path string) vectorCache {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var vectors vectorCache
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	return vectors
}

func (c vectorCache) get(text string) ([]float32, error) {
	vector, ok := c[text]
	if !ok {
		return nil, fmt.Errorf("no cached vector for %q", text)
	}
	return normalize(vector), nil
}
