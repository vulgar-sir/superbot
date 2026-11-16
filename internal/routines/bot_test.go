package routines

import (
	"testing"
	"time"

	"superbot/internal/store"
)

func TestShouldSkipFlexible(t *testing.T) {
	scheduled := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	later := func(minutes int) time.Time { return scheduled.Add(time.Duration(minutes) * time.Minute) }
	next := scheduled
	cases := []struct {
		name    string
		routine store.BotRoutine
		now     time.Time
		want    bool
	}{
		{"exact routine never skips", store.BotRoutine{Timing: "exact", Schedule: "0 9 * * *", NextRunAt: &next}, later(120), false},
		{"flexible inside grace", store.BotRoutine{Timing: "flexible", Schedule: "0 9 * * *", NextRunAt: &next}, later(10), false},
		{"flexible past grace", store.BotRoutine{Timing: "flexible", Schedule: "0 9 * * *", NextRunAt: &next}, later(16), true},
		{"flexible once always runs", store.BotRoutine{Timing: "flexible", Schedule: "once:2026-11-13T09:00:00Z", NextRunAt: &next}, later(120), false},
		{"no next run", store.BotRoutine{Timing: "flexible", Schedule: "0 9 * * *"}, later(60), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldSkipFlexible(tc.routine, tc.now); got != tc.want {
				t.Fatalf("shouldSkipFlexible() = %v, want %v", got, tc.want)
			}
		})
	}
}
