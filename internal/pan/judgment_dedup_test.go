package pan

// Judgment dedup (2026-09-15): Pan re-judged the same unchanged candidate
// set every 60s tick — measured ~65 pi calls/hour, 19M fresh input tokens
// overnight on MiniMax, ~7x what all bead work combined burned. A candidate
// set whose identity (IDs + states + reasons) is unchanged cannot produce a
// different verdict for 15 minutes; skip the pi call. Any state change
// re-judges immediately.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Two ticks with an identical candidate set must produce exactly one runner
// invocation; the second tick is skipped.
func TestJudgmentDedupSkipsUnchangedCandidates(t *testing.T) {
	store := &fakeStore{}
	calls := 0
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		calls++
		return `[{"bead_id": "blocked-1", "action": "repair:reset-retries", "message": ""}]`, nil
	}
	s := New(Config{Enabled: true}, store, runner)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "blocked-1", Reason: "retry exhausted", Since: time.Now()}}
	})
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"reset-retries": func(beadID string) error { return nil },
	})

	s.Tick(context.Background())
	s.Tick(context.Background())
	s.Tick(context.Background())

	if calls != 1 {
		t.Errorf("runner calls = %d, want 1 (unchanged candidate set must not re-judge)", calls)
	}
}

// Any change to a candidate's state or reason re-judges immediately.
func TestJudgmentDedupRejudgesOnChange(t *testing.T) {
	store := &fakeStore{}
	calls := 0
	reason := "retry exhausted"
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		calls++
		return `[{"bead_id": "blocked-1", "action": "repair:reset-retries", "message": ""}]`, nil
	}
	s := New(Config{Enabled: true}, store, runner)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "blocked-1", Reason: reason, Since: time.Now()}}
	})
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"reset-retries": func(beadID string) error { return nil },
	})

	s.Tick(context.Background())
	reason = "spawn failures: worktree add failed"
	s.Tick(context.Background())

	if calls != 2 {
		t.Errorf("runner calls = %d, want 2 (changed reason must re-judge)", calls)
	}
}

// The re-judge valve: an unchanged set is re-judged after the interval even
// with no state change, so a permanently stuck candidate is never blind.
func TestJudgmentDedupRejudgeValve(t *testing.T) {
	store := &fakeStore{}
	calls := 0
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		calls++
		return `[{"bead_id": "blocked-1", "action": "repair:reset-retries", "message": ""}]`, nil
	}
	s := New(Config{Enabled: true}, store, runner)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "blocked-1", Reason: "retry exhausted", Since: time.Now()}}
	})
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"reset-retries": func(beadID string) error { return nil },
	})

	s.Tick(context.Background())
	// Backdate the last judgment beyond the valve window.
	s.dedupMu.Lock()
	s.lastJudgedAt = time.Now().Add(-reJudgeInterval - time.Minute)
	s.dedupMu.Unlock()
	s.Tick(context.Background())

	if calls != 2 {
		t.Errorf("runner calls = %d, want 2 (re-judge valve must fire)", calls)
	}
}

var _ = strings.Contains
