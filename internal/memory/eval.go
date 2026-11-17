package memory

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/store"
)

// The evaluation set measures recall and duplicate detection on realistic
// memories. It ships with the binary so `superbot memory-eval` can run it
// against the configured embedding service.
//
//go:embed testdata/recall_eval.json
var evalFixture []byte

// EvalSet is a memory pool, queries labeled against it, and memory pairs.
type EvalSet struct {
	Memories []EvalMemory `json:"memories"`
	Queries  []EvalQuery  `json:"queries"`
	Pairs    []EvalPair   `json:"pairs"`
}

type EvalMemory struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// EvalQuery lists the memories a query must recall (Relevant) and the ones it
// may recall without that counting as noise (Acceptable).
type EvalQuery struct {
	ID         string   `json:"id"`
	Text       string   `json:"text"`
	Relevant   []string `json:"relevant"`
	Acceptable []string `json:"acceptable"`
}

// EvalPair is two memory texts: a "duplicate" restates one fact, an "update"
// changes a fact about the same subject, a "sibling" is a different fact of
// the same shape, "unrelated" share nothing.
type EvalPair struct {
	Kind string `json:"kind"`
	A    string `json:"a"`
	B    string `json:"b"`
}

// DefaultEvalSet returns the bundled evaluation set.
func DefaultEvalSet() (EvalSet, error) {
	var set EvalSet
	if err := json.Unmarshal(evalFixture, &set); err != nil {
		return EvalSet{}, err
	}
	return set, nil
}

// EvalScores are the similarities an embedding space gives the set: per query
// one score per memory (in set order) and the query's background (mean
// similarity to the calibration passages), and one score per pair (passage
// against passage, as the write-time checks compare them).
type EvalScores struct {
	Space       string               `json:"space"`
	Queries     map[string][]float64 `json:"queries"`
	Backgrounds map[string]float64   `json:"backgrounds"`
	Pairs       []float64            `json:"pairs"`
}

// EmbedFunc returns the unit vector of text as the space embeds a query
// (query true) or a stored memory.
type EmbedFunc func(ctx context.Context, text string, query bool) ([]float32, error)

// ComputeEvalScores embeds the whole set with embed.
func ComputeEvalScores(ctx context.Context, set EvalSet, spaceName string, embed EmbedFunc) (EvalScores, error) {
	scores := EvalScores{
		Space:       spaceName,
		Queries:     make(map[string][]float64, len(set.Queries)),
		Backgrounds: make(map[string]float64, len(set.Queries)),
	}
	passage := func(text string) ([]float32, error) { return embed(ctx, text, false) }
	memories := make([][]float32, len(set.Memories))
	for i, item := range set.Memories {
		vector, err := passage(item.Content)
		if err != nil {
			return EvalScores{}, fmt.Errorf("embed memory %s: %w", item.ID, err)
		}
		memories[i] = vector
	}
	refs := make([][]float32, len(calibrationPassages))
	for i, text := range calibrationPassages {
		vector, err := passage(text)
		if err != nil {
			return EvalScores{}, fmt.Errorf("embed calibration passage: %w", err)
		}
		refs[i] = vector
	}
	for _, query := range set.Queries {
		vector, err := embed(ctx, query.Text, true)
		if err != nil {
			return EvalScores{}, fmt.Errorf("embed query %s: %w", query.ID, err)
		}
		row := make([]float64, len(memories))
		for i, memory := range memories {
			row[i] = round4(dot(vector, memory))
		}
		scores.Queries[query.ID] = row
		scores.Backgrounds[query.ID] = round4(background(vector, refs))
	}
	for _, pair := range set.Pairs {
		a, err := passage(pair.A)
		if err != nil {
			return EvalScores{}, err
		}
		b, err := passage(pair.B)
		if err != nil {
			return EvalScores{}, err
		}
		scores.Pairs = append(scores.Pairs, round4(dot(a, b)))
	}
	return scores, nil
}

func round4(value float64) float64 { return math.Round(value*10000) / 10000 }

// EvalEmbedder is the embed function of a Service, for ComputeEvalScores.
func (s *Service) EvalEmbedder() EmbedFunc {
	return func(ctx context.Context, text string, query bool) ([]float32, error) {
		kind := kindPassage
		if query {
			kind = kindQuery
		}
		return s.embedVector(ctx, text, kind, backgroundEmbedTimeout)
	}
}

// Distribution summarizes a list of scores.
type Distribution struct {
	N                       int
	Min, P10, P50, P90, P99 float64
	Max                     float64
}

func distribution(values []float64) Distribution {
	if len(values) == 0 {
		return Distribution{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	at := func(p float64) float64 {
		return sorted[int(math.Round(p*float64(len(sorted)-1)))]
	}
	return Distribution{
		N: len(sorted), Min: sorted[0], P10: at(0.10), P50: at(0.50),
		P90: at(0.90), P99: at(0.99), Max: sorted[len(sorted)-1],
	}
}

func (d Distribution) String() string {
	if d.N == 0 {
		return "n=0"
	}
	return fmt.Sprintf("n=%d min=%.3f p10=%.3f p50=%.3f p90=%.3f p99=%.3f max=%.3f",
		d.N, d.Min, d.P10, d.P50, d.P90, d.P99, d.Max)
}

// EvalReport is the outcome of the recall pipeline and the write-time
// thresholds on one set of scores.
type EvalReport struct {
	Space      string
	Calibrated bool
	// Relevant and Irrelevant are the scores of labeled pairs relative to the
	// query's recall threshold (0 is the cut-off).
	Relevant, Irrelevant Distribution
	TP, FP, FN           int
	Precision, Recall    float64
	// NoisyNegatives counts queries without a relevant memory that still
	// recalled something they should not.
	NoisyNegatives, Negatives int
	Misses                    []string
	FalseHits                 []string

	Pairs map[string]Distribution
	// MergeErrors are update or sibling pairs at or above MergeMin, i.e.
	// different facts the unattended merge would fold into one.
	MergeErrors int
	// DuplicatesMerged are duplicate pairs the unattended merge catches.
	DuplicatesMerged, Duplicates int
	// UpdatesReached are update pairs at or above RelatedMin, i.e. outdated
	// facts the conflict check gets to see.
	UpdatesReached, Updates int
	// UnrelatedReached are unrelated pairs at or above RelatedMin.
	UnrelatedReached int
}

func (r EvalReport) F1() float64 {
	if r.Precision+r.Recall == 0 {
		return 0
	}
	return 2 * r.Precision * r.Recall / (r.Precision + r.Recall)
}

func (r EvalReport) String() string {
	var b strings.Builder
	mode := "absolute threshold"
	if r.Calibrated {
		mode = "calibrated threshold"
	}
	fmt.Fprintf(&b, "space %s (%s)\n", r.Space, mode)
	fmt.Fprintf(&b, "  relevant   (score - threshold): %s\n", r.Relevant)
	fmt.Fprintf(&b, "  irrelevant (score - threshold): %s\n", r.Irrelevant)
	fmt.Fprintf(&b, "  recall pipeline: precision=%.3f recall=%.3f F1=%.3f (tp=%d fp=%d fn=%d), noisy negative queries %d/%d\n",
		r.Precision, r.Recall, r.F1(), r.TP, r.FP, r.FN, r.NoisyNegatives, r.Negatives)
	for _, kind := range []string{"duplicate", "update", "sibling", "unrelated"} {
		fmt.Fprintf(&b, "  pairs %-9s %s\n", kind, r.Pairs[kind])
	}
	fmt.Fprintf(&b, "  unattended merge: %d/%d duplicates merged, %d different facts merged (must be 0)\n",
		r.DuplicatesMerged, r.Duplicates, r.MergeErrors)
	fmt.Fprintf(&b, "  conflict check reaches %d/%d updates, %d unrelated pairs\n",
		r.UpdatesReached, r.Updates, r.UnrelatedReached)
	if len(r.Misses) > 0 {
		fmt.Fprintf(&b, "  missed: %s\n", strings.Join(r.Misses, "; "))
	}
	if len(r.FalseHits) > 0 {
		fmt.Fprintf(&b, "  noise: %s\n", strings.Join(r.FalseHits, "; "))
	}
	return b.String()
}

// EvaluateScores runs the production recall ranking (threshold, margin,
// limit) and the write-time thresholds of space on precomputed scores.
func EvaluateScores(set EvalSet, scores EvalScores, space embeddingSpace, calibrated bool) EvalReport {
	report := EvalReport{Space: scores.Space, Calibrated: calibrated, Pairs: map[string]Distribution{}}
	ids := make([]uuid.UUID, len(set.Memories))
	for i := range set.Memories {
		ids[i] = uuid.NewSHA1(uuid.NameSpaceOID, []byte(set.Memories[i].ID))
	}
	params := rankParams{margin: space.Margin}
	var relevantScores, irrelevantScores []float64
	for _, query := range set.Queries {
		row := scores.Queries[query.ID]
		if len(row) != len(set.Memories) {
			continue
		}
		threshold := space.RecallMin
		if calibrated {
			threshold = max(space.RecallFloor, scores.Backgrounds[query.ID]+space.RecallDelta)
		}
		relevant := make(map[string]bool, len(query.Relevant))
		for _, id := range query.Relevant {
			relevant[id] = true
		}
		acceptable := make(map[string]bool, len(query.Acceptable))
		for _, id := range query.Acceptable {
			acceptable[id] = true
		}
		var hits []scoredMemory
		for i, score := range row {
			memoryID := set.Memories[i].ID
			switch {
			case relevant[memoryID]:
				relevantScores = append(relevantScores, score-threshold)
			case !acceptable[memoryID]:
				irrelevantScores = append(irrelevantScores, score-threshold)
			}
			if score >= threshold {
				hits = append(hits, scoredMemory{
					memory:     store.Memory{ID: ids[i], Content: memoryID},
					similarity: score,
				})
			}
		}
		recalled := map[string]bool{}
		noisy := false
		for _, item := range rankRecall(hits, 6, params) {
			memoryID := item.Content
			recalled[memoryID] = true
			switch {
			case relevant[memoryID]:
				report.TP++
			case !acceptable[memoryID]:
				report.FP++
				noisy = true
				report.FalseHits = append(report.FalseHits, query.ID+"→"+memoryID)
			}
		}
		for _, id := range query.Relevant {
			if !recalled[id] {
				report.FN++
				report.Misses = append(report.Misses, query.ID+"→"+id)
			}
		}
		if len(query.Relevant) == 0 {
			report.Negatives++
			if noisy {
				report.NoisyNegatives++
			}
		}
	}
	report.Relevant = distribution(relevantScores)
	report.Irrelevant = distribution(irrelevantScores)
	if report.TP+report.FP > 0 {
		report.Precision = float64(report.TP) / float64(report.TP+report.FP)
	} else {
		report.Precision = 1
	}
	if report.TP+report.FN > 0 {
		report.Recall = float64(report.TP) / float64(report.TP+report.FN)
	}

	byKind := map[string][]float64{}
	for i, pair := range set.Pairs {
		if i >= len(scores.Pairs) {
			break
		}
		score := scores.Pairs[i]
		byKind[pair.Kind] = append(byKind[pair.Kind], score)
		merged := score >= space.MergeMin && sameNumbers(pair.A, pair.B)
		switch pair.Kind {
		case "duplicate":
			report.Duplicates++
			if merged {
				report.DuplicatesMerged++
			}
		case "update", "sibling":
			if merged {
				report.MergeErrors++
			}
			if pair.Kind == "update" {
				report.Updates++
				if score >= space.RelatedMin {
					report.UpdatesReached++
				}
			}
		case "unrelated":
			if score >= space.RelatedMin {
				report.UnrelatedReached++
			}
		}
	}
	for kind, values := range byKind {
		report.Pairs[kind] = distribution(values)
	}
	return report
}

// RunEval embeds the bundled set with the configured embedding service and
// evaluates the active space. It is what `superbot memory-eval` runs.
func (s *Service) RunEval(ctx context.Context) (EvalReport, EvalScores, error) {
	set, err := DefaultEvalSet()
	if err != nil {
		return EvalReport{}, EvalScores{}, err
	}
	scores, err := ComputeEvalScores(ctx, set, s.ModelName(), s.EvalEmbedder())
	if err != nil {
		return EvalReport{}, EvalScores{}, err
	}
	return EvaluateScores(set, scores, s.space, s.calibrated()), scores, nil
}

// EvalFloor is the quality an embedding space must keep on the bundled set.
type EvalFloor struct {
	Precision, Recall float64
	NoisyNegatives    int
	// MinUpdatesReached is the share of update pairs the conflict check
	// must see.
	MinUpdatesReached float64
}

// EvalFloorFor returns the measured floor of a space, or false when the space
// has never been measured (generic remote models).
func EvalFloorFor(space string) (EvalFloor, bool) {
	switch {
	case space == localModel:
		return localEvalFloor, true
	case strings.HasSuffix(space, e5SpaceVersion):
		return e5EvalFloor, true
	}
	return EvalFloor{}, false
}

// Check reports what in r falls below the floor.
func (f EvalFloor) Check(r EvalReport) []string {
	var problems []string
	if r.Precision < f.Precision {
		problems = append(problems, fmt.Sprintf("precision %.3f < %.3f", r.Precision, f.Precision))
	}
	if r.Recall < f.Recall {
		problems = append(problems, fmt.Sprintf("recall %.3f < %.3f", r.Recall, f.Recall))
	}
	if r.NoisyNegatives > f.NoisyNegatives {
		problems = append(problems, fmt.Sprintf("%d noisy negative queries > %d", r.NoisyNegatives, f.NoisyNegatives))
	}
	if r.MergeErrors > 0 {
		problems = append(problems, fmt.Sprintf("%d different facts would be merged without judgment", r.MergeErrors))
	}
	if r.Updates > 0 && float64(r.UpdatesReached)/float64(r.Updates) < f.MinUpdatesReached {
		problems = append(problems, fmt.Sprintf("conflict check reaches only %d/%d updates", r.UpdatesReached, r.Updates))
	}
	return problems
}
