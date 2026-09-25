package pan

// Judgment dedup (2026-09-15): Pan re-judged the same unchanged candidate
// set every 60s tick — measured ~65 pi calls/hour, 19M fresh input tokens
// overnight on MiniMax, ~7x what all bead work combined burned. A candidate
// set whose identity (IDs + states + reasons) is unchanged cannot produce a
// different verdict for 15 minutes; skip the pi call. Any state change
// re-judges immediately.

import (
	"context"
	"fmt"
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
	// Backdate: clear the per-candidate cache so the re-judge valve fires.
	s.dedupMu.Lock()
	s.judged = map[string]time.Time{}
	s.dedupMu.Unlock()
	s.Tick(context.Background())

	if calls != 2 {
		t.Errorf("runner calls = %d, want 2 (re-judge valve must fire)", calls)
	}
}

var _ = strings.Contains

// Per-candidate cache (2026-10-04): a volatile change in ONE candidate
// (token-budget counts, digest text) must not re-judge a STABLE candidate.
// Previously the whole-set signature reset and the parked bead re-judged
// every tick, each verdict a duplicate 'continue' the queue then skipped.
func TestJudgmentDedupPerCandidateIsolation(t *testing.T) {
	store := &fakeStore{}
	parkedCalls := 0
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		if strings.Contains(prompt, "parked-1") && !strings.Contains(prompt, "noisy-1") {
			parkedCalls++
		}
		// Judge parked-1 to continue on first call, noisy-1 to reset-retries.
		if strings.Contains(prompt, "parked-1") {
			return `[{"bead_id": "parked-1", "action": "continue", "message": ""}]`, nil
		}
		return `[{"bead_id": "noisy-1", "action": "repair:reset-retries", "message": ""}]`, nil
	}
	s := New(Config{Enabled: true}, store, runner)
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"reset-retries": func(beadID string) error { return nil },
	})

	s.Tick(context.Background())

	// parked-1 alone: stable reason → judged once, then cached.
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "parked-1", Reason: "BLOCKED: retry exhausted", Since: time.Now()}}
	})
	s.Tick(context.Background())
	s.Tick(context.Background())

	// noisy-1 joins with a reason that changes every tick (volatile token
	// counts) — parked-1 must NOT be re-judged, only noisy-1.
	n := 0
	s.SetBlockedBeads(func() []BlockedBead {
		n++
		return []BlockedBead{
			{BeadID: "parked-1", Reason: "BLOCKED: retry exhausted", Since: time.Now()},
			{BeadID: "noisy-1", Reason: fmt.Sprintf("BLOCKED: token budget exceeded (fresh=%d)", n*7919), Since: time.Now()},
		}
	})
	for i := 0; i < 3; i++ {
		s.Tick(context.Background())
	}

	if parkedCalls != 1 {
		t.Errorf("parked-1 judged %d times, want 1 (stable candidate must not re-judge when another candidate churns)", parkedCalls)
	}
}
