package pan

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// Budget-as-signal (2026-08-22 design correction): the old design killed
// workers at the token ceiling — which would have killed ux5 (9M) and acx
// (11.7M), beads that LANDED. Pan said 'continue' on cc0d6 minutes before
// Charon killed it. Now: crossing the budget makes the worker a judgment
// candidate; a fresh continue verdict is a stay of execution; only the hard
// backstop (2x) with no recent continue kills.

func budgetWorker(beadID string, tokens int64) registry.WorkerRecord {
	return registry.WorkerRecord{
		BeadID: beadID, State: registry.StateWorking, TokensSoFar: tokens,
		LastHeartbeat: time.Now(), StartedAt: time.Now().Add(-time.Hour),
	}
}

func TestCandidates_TokenBudgetExceeded(t *testing.T) {
	st := &fakeStore{workers: []registry.WorkerRecord{budgetWorker("proj-big1", 8_000_000)}}
	s := New(Config{Enabled: true, TokenBudget: 5_000_000}, st,
		func(context.Context, Config, string) (string, error) { return "[]", nil })

	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	found := false
	for _, c := range cands {
		if c.Record.BeadID == "proj-big1" {
			found = true
			if !strings.Contains(c.Reason, "token budget") {
				t.Errorf("reason %q must name the token budget", c.Reason)
			}
		}
	}
	if !found {
		t.Error("worker over token budget must surface as a judgment candidate")
	}
}

func TestCandidates_TokenBudgetDisabledOrUnder(t *testing.T) {
	st := &fakeStore{workers: []registry.WorkerRecord{budgetWorker("proj-big2", 900_000_000)}}
	s := New(Config{Enabled: true, TokenBudget: 0}, st,
		func(context.Context, Config, string) (string, error) { return "[]", nil })
	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	for _, c := range cands {
		if c.Record.BeadID == "proj-big2" {
			t.Error("budget 0 (disabled) must never surface token candidates")
		}
	}
}

func TestContinuedRecently_StayOfExecution(t *testing.T) {
	s := New(Config{Enabled: true}, &fakeStore{},
		func(context.Context, Config, string) (string, error) { return "[]", nil })
	if s.ContinuedRecently("proj-x", 5*time.Minute) {
		t.Error("no verdict recorded — must be false")
	}
	s.recordVerdict("proj-x", registry.CmdContinue)
	if !s.ContinuedRecently("proj-x", 5*time.Minute) {
		t.Error("fresh continue verdict must report true")
	}
	if s.ContinuedRecently("proj-x", time.Nanosecond) {
		t.Error("stale window must report false")
	}
	s.recordVerdict("proj-y", registry.CmdAbandonWithSummary)
	if s.ContinuedRecently("proj-y", 5*time.Minute) {
		t.Error("terminal verdicts must not count as continue")
	}
}
