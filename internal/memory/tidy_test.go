package memory

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func noSimilarity(a, b tidyRow) float64 { return 0 }

func tidyFixture(n int) []tidyRow {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]tidyRow, n)
	for i := range rows {
		rows[i] = tidyRow{
			ID: uuid.New(), Content: fmt.Sprintf("distinct fact number %d", i),
			UpdatedAt: base.Add(time.Duration(i) * time.Hour),
		}
	}
	return rows
}

func TestPlanTidyKeepsNewestExactDuplicate(t *testing.T) {
	rows := tidyFixture(10)
	rows[2].Content = "User prefers dark mode"
	rows[7].Content = "user prefers  DARK mode."
	plan := planTidy(rows, noSimilarity, tidySimilarity, tidyMaxFraction)
	if len(plan) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	if plan[0].ID != rows[2].ID || plan[0].KeptID != rows[7].ID || plan[0].Reason != tidyReasonExact {
		t.Fatalf("the older copy must be archived and the newest kept: %+v", plan[0])
	}
}

func TestPlanTidyNearDuplicateUsesSimilarity(t *testing.T) {
	rows := tidyFixture(10)
	similarity := func(a, b tidyRow) float64 {
		pair := map[uuid.UUID]bool{rows[1].ID: true, rows[4].ID: true}
		if pair[a.ID] && pair[b.ID] {
			return 0.97
		}
		return 0.94
	}
	plan := planTidy(rows, similarity, tidySimilarity, tidyMaxFraction)
	if len(plan) != 1 || plan[0].ID != rows[1].ID || plan[0].KeptID != rows[4].ID {
		t.Fatalf("plan = %+v", plan)
	}
	if plan[0].Reason != tidyReasonNear || plan[0].Similarity != 0.97 {
		t.Fatalf("reason/similarity wrong: %+v", plan[0])
	}
}

func TestPlanTidyNeverArchivesPinned(t *testing.T) {
	rows := tidyFixture(10)
	for _, i := range []int{0, 3, 9} {
		rows[i].Content = "same fact"
	}
	rows[0].Pinned = true
	rows[3].Pinned = true
	plan := planTidy(rows, noSimilarity, tidySimilarity, 1)
	if len(plan) != 1 || plan[0].ID != rows[9].ID {
		t.Fatalf("only the unpinned copy may be archived, even though it is newest: %+v", plan)
	}
	if plan[0].KeptID != rows[3].ID {
		t.Fatalf("the newest pinned copy must be the keeper: %+v", plan[0])
	}

	allPinned := tidyFixture(4)
	for i := range allPinned {
		allPinned[i].Content = "same fact"
		allPinned[i].Pinned = true
	}
	if plan := planTidy(allPinned, noSimilarity, tidySimilarity, 1); len(plan) != 0 {
		t.Fatalf("pinned rows must never be touched: %+v", plan)
	}
}

func TestPlanTidyCapsChangesPerRun(t *testing.T) {
	rows := tidyFixture(30)
	for i := 0; i < 20; i++ {
		rows[i].Content = "same fact"
	}
	plan := planTidy(rows, noSimilarity, tidySimilarity, tidyMaxFraction)
	if len(plan) != 6 {
		t.Fatalf("20%% of 30 rows is 6, got %d", len(plan))
	}
	for _, entry := range plan {
		if entry.KeptID != rows[19].ID {
			t.Fatalf("the newest copy must be kept: %+v", entry)
		}
		if entry.ID == rows[19].ID {
			t.Fatal("the keeper was archived")
		}
	}

	small := tidyFixture(3)
	small[0].Content, small[1].Content, small[2].Content = "x fact", "x fact", "x fact"
	if plan := planTidy(small, noSimilarity, tidySimilarity, tidyMaxFraction); len(plan) != 1 {
		t.Fatalf("small scopes may change one row per run, got %d", len(plan))
	}
}

func TestPlanTidyNothingToDo(t *testing.T) {
	if plan := planTidy(tidyFixture(25), noSimilarity, tidySimilarity, tidyMaxFraction); len(plan) != 0 {
		t.Fatalf("distinct rows must be left alone: %+v", plan)
	}
	if plan := planTidy(tidyFixture(1), noSimilarity, tidySimilarity, tidyMaxFraction); plan != nil {
		t.Fatal("a single row cannot have duplicates")
	}
}

func TestVectorSimilarity(t *testing.T) {
	a := tidyRow{Model: localModel, Vector: Embed("用户喜欢喝无糖的冰美式咖啡")}
	b := tidyRow{Model: localModel, Vector: Embed("用户喜欢喝无糖的冰美式咖啡。")}
	if score := vectorSimilarity(a, b); score < tidySimilarity {
		t.Fatalf("near-identical text scored %.3f", score)
	}
	other := tidyRow{Model: "remote:text-embedding-3-small", Vector: a.Vector}
	if score := vectorSimilarity(a, other); score != 0 {
		t.Fatalf("vectors from different spaces must not be compared, got %.3f", score)
	}
	if score := vectorSimilarity(a, tidyRow{Model: localModel}); score != 0 {
		t.Fatalf("a row without a vector must score 0, got %.3f", score)
	}
}
