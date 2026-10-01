package pan

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

func TestPrunableWedgeKey(t *testing.T) {
	cases := []struct {
		reason string
		wantOK bool
	}{
		{"BLOCKED: retry exhausted after 4 attempts — stale worktree registration for webapp-xw3", true},
		{"BLOCKED: stale spawn registration blocking respawn", true},
		{"BLOCKED: escalation waiting for human", false},
		{"BLOCKED: integration conflict against base branch", false},
		{"PUSH-PENDING: publisher still pending", false},
		{"", false},
	}
	for _, tc := range cases {
		_, ok := prunableWedgeKey(tc.reason)
		if ok != tc.wantOK {
			t.Errorf("prunableWedgeKey(%q) ok=%v, want %v", tc.reason, ok, tc.wantOK)
		}
	}
}

func TestPrunableWedgeSweepBatchPrune(t *testing.T) {
	// PROJECT-WEDGE shape: 5 beads share "retry exhausted after 4 attempts"
	// over the same stale worktree registration.
	reason := "BLOCKED: retry exhausted after 4 attempts — stale worktree registration"
	var pruned []string
	s := New(Config{Enabled: false}, &fakeStore{}, nil)
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"prune-worktree-registration": func(beadID string) error {
			pruned = append(pruned, beadID)
			return nil
		},
	})
	s.SetProjectScope(func(beadID string) string { return "/proj/webapp" })

	var cands []Candidate
	for _, id := range []string{"b1", "b2", "b3", "b4", "b5"} {
		cands = append(cands, Candidate{Record: registry.WorkerRecord{BeadID: id}, Reason: reason})
	}
	report := s.PrunableWedgeSweep(cands)

	// Batch mode: ONE prune invocation for the whole cohort, not five.
	if len(pruned) != 1 || pruned[0] != "b1" {
		t.Fatalf("pruned %v, want one invocation on representative b1", pruned)
	}
	if len(report.Successes) != 1 || report.Successes[0] != "retry exhausted after 4 attempts — stale worktree registration" {
		t.Fatalf("successes %v, want cohort key", report.Successes)
	}
	if len(report.Cleared) != 5 {
		t.Fatalf("cleared %d beads, want 5", len(report.Cleared))
	}
	if len(report.Residuals) != 0 {
		t.Fatalf("residuals %v, want none", report.Residuals)
	}
}

func TestPrunableWedgeSweepBelowThresholdResidual(t *testing.T) {
	reason := "BLOCKED: stale worktree registration"
	s := New(Config{Enabled: false}, &fakeStore{}, nil)
	var pruned []string
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"prune-worktree-registration": func(beadID string) error {
			pruned = append(pruned, beadID)
			return nil
		},
	})
	cands := []Candidate{
		{Record: registry.WorkerRecord{BeadID: "solo"}, Reason: reason},
	}
	report := s.PrunableWedgeSweep(cands)
	if len(pruned) != 0 {
		t.Fatalf("pruned %v, want none below threshold", pruned)
	}
	detail, ok := report.Residuals["stale worktree registration"]
	if !ok {
		t.Fatalf("want residual for cohort key, got %v", report.Residuals)
	}
	if !strings.Contains(detail, "below sweep threshold") {
		t.Errorf("residual detail %q missing threshold explanation", detail)
	}
}

func TestPrunableWedgeSweepRepairFailureResidual(t *testing.T) {
	reason := "BLOCKED: retry exhausted after 4 attempts — stale worktree registration"
	s := New(Config{Enabled: false}, &fakeStore{}, nil)
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"prune-worktree-registration": func(beadID string) error {
			return fmt.Errorf("prune failed: git worktree prune exited 1")
		},
	})
	s.SetProjectScope(func(beadID string) string { return "/proj/x" })
	cands := []Candidate{
		{Record: registry.WorkerRecord{BeadID: "b1"}, Reason: reason},
		{Record: registry.WorkerRecord{BeadID: "b2"}, Reason: reason},
	}
	report := s.PrunableWedgeSweep(cands)
	if len(report.Successes) != 0 {
		t.Fatalf("successes %v, want none", report.Successes)
	}
	detail, ok := report.Residuals["retry exhausted after 4 attempts — stale worktree registration"]
	if !ok || !strings.Contains(detail, `"error"`) {
		t.Fatalf("residual %q want explicit error status", detail)
	}
}

func TestPrunableWedgeSweepNeverPrunesLiveWorkerBead(t *testing.T) {
	reason := "BLOCKED: stale worktree registration"
	store := &fakeStore{workers: []registry.WorkerRecord{hungWorker("b1")}}
	s := New(Config{Enabled: false}, store, nil)
	var pruned []string
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"prune-worktree-registration": func(beadID string) error {
			pruned = append(pruned, beadID)
			return nil
		},
	})
	s.SetProjectScope(func(beadID string) string { return "/proj/x" })
	cands := []Candidate{
		{Record: registry.WorkerRecord{BeadID: "b1"}, Reason: reason},
		{Record: registry.WorkerRecord{BeadID: "b2"}, Reason: reason},
	}
	report := s.PrunableWedgeSweep(cands)
	// Safety gate must refuse: live worker row on the representative.
	if len(pruned) != 0 {
		t.Fatalf("pruned %v, want refusal on live worker", pruned)
	}
	if _, ok := report.Residuals["stale worktree registration"]; !ok {
		t.Fatalf("want residual divergence, got %v", report.Residuals)
	}
}

func TestTickRunsPrunableSweepBeforeJudgment(t *testing.T) {
	reason := "BLOCKED: retry exhausted after 4 attempts — stale worktree registration"
	var pruned []string
	store := &fakeStore{}
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[]`, nil
	}
	s := New(Config{Enabled: true}, store, runner)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{
			{BeadID: "w1", Reason: reason},
			{BeadID: "w2", Reason: reason},
		}
	})
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"prune-worktree-registration": func(beadID string) error {
			pruned = append(pruned, beadID)
			return nil
		},
	})
	s.SetProjectScope(func(beadID string) string { return "/proj/x" })
	s.Tick(context.Background())
	if len(pruned) != 1 {
		t.Fatalf("sweep did not batch-prune during Tick: %v", pruned)
	}
}
