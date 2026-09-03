package pan

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

// fakeStore records enqueued commands and serves canned workers.
type fakeStore struct {
	workers  []registry.WorkerRecord
	enqueued []struct {
		bead    string
		kind    registry.CommandKind
		payload string
	}
	pending []registry.Command
	listErr error
}

func (f *fakeStore) ListWorkers(states ...registry.WorkerState) ([]registry.WorkerRecord, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if len(states) == 0 {
		var out []registry.WorkerRecord
		for _, w := range f.workers {
			if w.State != registry.StateCompleted {
				out = append(out, w)
			}
		}
		return out, nil
	}
	want := map[registry.WorkerState]bool{}
	for _, s := range states {
		want[s] = true
	}
	var out []registry.WorkerRecord
	for _, w := range f.workers {
		if want[w.State] {
			out = append(out, w)
		}
	}
	return out, nil
}

func (f *fakeStore) PendingCommands(beadID string) ([]registry.Command, error) {
	var out []registry.Command
	for _, c := range f.pending {
		if c.BeadID == beadID {
			out = append(out, c)
		}
	}
	for _, e := range f.enqueued {
		if e.bead == beadID {
			out = append(out, registry.Command{BeadID: e.bead, Kind: e.kind, Payload: e.payload})
		}
	}
	return out, nil
}

func (f *fakeStore) EnqueueCommand(beadID string, kind registry.CommandKind, payload string) error {
	f.enqueued = append(f.enqueued, struct {
		bead    string
		kind    registry.CommandKind
		payload string
	}{beadID, kind, payload})
	return nil
}

func parkedOverdue(beadID string) registry.WorkerRecord {
	return registry.WorkerRecord{
		BeadID:        beadID,
		State:         registry.StateParked,
		ParkSince:     time.Now().Add(-30 * time.Minute),
		ParkWakeAt:    time.Now().Add(-20 * time.Minute),
		ParkReason:    "provider-capacity: 403 concurrent request limit",
		LastHeartbeat: time.Now(),
	}
}

func hungWorker(beadID string) registry.WorkerRecord {
	return registry.WorkerRecord{
		BeadID:        beadID,
		State:         registry.StateWorking,
		LastHeartbeat: time.Now().Add(-30 * time.Minute),
	}
}

func TestCandidatesFilters(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{
		parkedOverdue("overdue"),
		hungWorker("hung"),
		{BeadID: "fresh-park", State: registry.StateParked,
			ParkWakeAt: time.Now().Add(30 * time.Minute), LastHeartbeat: time.Now()},
		{BeadID: "healthy", State: registry.StateWorking, LastHeartbeat: time.Now()},
		{BeadID: "nudge-capped", State: registry.StateWorking,
			LastHeartbeat: time.Now(), NudgeCount: 3},
	}}

	s := New(Config{Enabled: true}, store, nil)
	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	got := map[string]bool{}
	for _, c := range cands {
		got[c.Record.BeadID] = true
	}
	for _, want := range []string{"overdue", "hung", "nudge-capped"} {
		if !got[want] {
			t.Errorf("candidate %s missing (got %v)", want, got)
		}
	}
	for _, unwanted := range []string{"fresh-park", "healthy"} {
		if got[unwanted] {
			t.Errorf("%s should not be a candidate", unwanted)
		}
	}
}

func TestTickEnqueuesParsedDecisions(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{parkedOverdue("bead-1")}}
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		if !strings.Contains(prompt, "bead-1") {
			t.Errorf("prompt missing candidate: %s", prompt)
		}
		return `Some prose first.
[{"bead_id": "bead-1", "action": "continue", "message": "provider answering again"},
 {"bead_id": "bead-1", "action": "explode", "message": "unknown action, must be rejected"},
 {"bead_id": "", "action": "steer", "message": "empty id, must be rejected"}]`, nil
	}

	s := New(Config{Enabled: true}, store, runner)
	s.Tick(context.Background())

	if len(store.enqueued) != 1 {
		t.Fatalf("enqueued %d commands, want 1: %+v", len(store.enqueued), store.enqueued)
	}
	if store.enqueued[0].kind != registry.CmdContinue || store.enqueued[0].bead != "bead-1" {
		t.Errorf("wrong command: %+v", store.enqueued[0])
	}
}

func TestTickFailOpenOnPiFailure(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{parkedOverdue("bead-1")}}
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return "", fmt.Errorf("provider unreachable")
	}

	s := New(Config{Enabled: true}, store, runner)
	s.Tick(context.Background()) // must not panic

	if len(store.enqueued) != 0 {
		t.Errorf("commands enqueued despite pi failure: %+v", store.enqueued)
	}
}

func TestTickFailOpenOnStoreFailure(t *testing.T) {
	store := &fakeStore{listErr: fmt.Errorf("database locked")}
	called := false
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		called = true
		return "", nil
	}

	s := New(Config{Enabled: true}, store, runner)
	s.Tick(context.Background())

	if called {
		t.Errorf("pi called despite store failure")
	}
}

func TestTickNoCandidatesSkipsPi(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{
		{BeadID: "healthy", State: registry.StateWorking, LastHeartbeat: time.Now()},
	}}
	called := false
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		called = true
		return "", nil
	}

	s := New(Config{Enabled: true}, store, runner)
	s.Tick(context.Background())

	if called {
		t.Errorf("pi called with zero candidates")
	}
}

func TestParseDecisionsToleratesFencesAndProse(t *testing.T) {
	out := "Here you go:\n```json\n[{\"bead_id\": \"b1\", \"action\": \"steer\", \"message\": \"hi\"}]\n```\nDone."
	ds, err := ParseDecisions(out)
	if err != nil {
		t.Fatalf("ParseDecisions: %v", err)
	}
	if len(ds) != 1 || ds[0].BeadID != "b1" || ds[0].Action != "steer" {
		t.Errorf("wrong parse: %+v", ds)
	}

	if _, err := ParseDecisions("no json at all"); err == nil {
		t.Errorf("expected error for missing array")
	}
}

func TestCandidatesZeroDiffMarkerAppendsReason(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{{
		BeadID: "over-budget-zero", State: registry.StateWorking,
		LastHeartbeat: time.Now(), TokensSoFar: 200, TokensFresh: 200,
	}}}

	s := New(Config{Enabled: true, TokenBudget: 100}, store, nil)
	s.SetZeroDiff(func(beadID string) (bool, bool) {
		return beadID == "over-budget-zero", true
	})
	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want 1", len(cands))
	}
	if !strings.Contains(cands[0].Reason, "zero diff on target files") {
		t.Errorf("reason missing zero-diff marker: %q", cands[0].Reason)
	}
}

func TestCandidatesZeroDiffUnknownNoMarker(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{{
		BeadID: "over-budget-unknown", State: registry.StateWorking,
		LastHeartbeat: time.Now(), TokensSoFar: 200, TokensFresh: 200,
	}}}

	s := New(Config{Enabled: true, TokenBudget: 100}, store, nil)
	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want 1", len(cands))
	}
	if strings.Contains(cands[0].Reason, "zero diff") {
		t.Errorf("reason should not contain zero-diff marker when unknown: %q", cands[0].Reason)
	}
}

func TestTickCloseSatisfiedCallsHandler(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{hungWorker("bead-1")}}
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "bead-1", "action": "close_satisfied", "message": "already green"}]`, nil
	}

	var handled []string
	s := New(Config{Enabled: true}, store, runner)
	s.SetOnCloseSatisfied(func(beadID string) error {
		handled = append(handled, beadID)
		return nil
	})
	s.Tick(context.Background())

	if len(handled) != 1 || handled[0] != "bead-1" {
		t.Fatalf("handler called %v, want [bead-1]", handled)
	}
	if len(store.enqueued) != 0 {
		t.Errorf("close_satisfied enqueued a worker command: %+v", store.enqueued)
	}
}

func TestTickCloseSatisfiedNilHandlerIgnored(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{hungWorker("bead-1")}}
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "bead-1", "action": "close_satisfied", "message": "already green"}]`, nil
	}

	s := New(Config{Enabled: true}, store, runner)
	s.Tick(context.Background()) // must not panic

	if len(store.enqueued) != 0 {
		t.Errorf("close_satisfied enqueued a worker command: %+v", store.enqueued)
	}
}

func TestTickCloseSatisfiedHandlerErrorDoesNotEnqueue(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{hungWorker("bead-1")}}
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "bead-1", "action": "close_satisfied", "message": "already green"}]`, nil
	}

	s := New(Config{Enabled: true}, store, runner)
	s.SetOnCloseSatisfied(func(beadID string) error {
		return fmt.Errorf("gate red")
	})
	s.Tick(context.Background())

	if len(store.enqueued) != 0 {
		t.Errorf("close_satisfied enqueued a worker command after handler error: %+v", store.enqueued)
	}
}

func TestTickCloseSatisfiedAbstainRecordsContinue(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{hungWorker("bead-1")}}
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "bead-1", "action": "close_satisfied", "message": "coverage threshold missing"}]`, nil
	}

	s := New(Config{Enabled: true}, store, runner)
	s.SetOnCloseSatisfied(func(beadID string) error {
		return ErrSatisfiedAbstain
	})
	s.Tick(context.Background())

	if len(store.enqueued) != 1 {
		t.Fatalf("expected one enqueued command, got %+v", store.enqueued)
	}
	if store.enqueued[0].kind != registry.CmdContinue {
		t.Errorf("expected continue verdict, got %s", store.enqueued[0].kind)
	}
}

func TestNilDepsNoPanic(t *testing.T) {
	// A freshly-built shepherd with nil deps must survive a no-op tick.
	s := New(Config{Enabled: false}, nil, nil)
	s.SetZeroDiff(nil)
	s.SetOnCloseSatisfied(nil)
	s.Tick(context.Background())
}

// Pan queued 511 identical abandon-with-summary commands for one dead bead
// (srv-smart-engine-g1m) because a dead worker never consumes its queue and
// Pan re-decided the same verdict every tick. Before enqueueing, Pan must
// skip any decision whose kind already sits unconsumed in the worker's queue.
func TestTickDeduplicatesPendingCommands(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{hungWorker("bead-dead")}}
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "bead-dead", "action": "abandon-with-summary", "message": "dead worker"}]`, nil
	}

	s := New(Config{Enabled: true}, store, runner)

	// Tick twice with the same decision: the second must be a no-op.
	s.Tick(context.Background())
	s.Tick(context.Background())

	if len(store.enqueued) != 1 {
		t.Fatalf("enqueued %d commands across 2 ticks, want 1 (dedup): %+v", len(store.enqueued), store.enqueued)
	}
}

func TestBuildPromptListsRepairWhitelist(t *testing.T) {
	prompt := BuildPrompt(nil)
	for _, name := range []string{"repair-worktree", "reap-placeholder", "reset-retries"} {
		if !strings.Contains(prompt, name) {
			t.Errorf("prompt missing whitelisted repair name %q", name)
		}
	}
	want := "No other repair:<name> strings exist"
	if !strings.Contains(prompt, want) {
		t.Errorf("prompt missing explicit no-other-names clause")
	}
}

func TestCandidatesBlockedBeads(t *testing.T) {
	store := &fakeStore{}
	s := New(Config{Enabled: true}, store, nil)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{
			{BeadID: "blocked-1", Reason: "retry exhausted after 5 attempts", Since: time.Now().Add(-5 * time.Minute)},
		}
	})

	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want 1: %+v", len(cands), cands)
	}
	if cands[0].Record.BeadID != "blocked-1" {
		t.Errorf("candidate bead ID = %q, want blocked-1", cands[0].Record.BeadID)
	}
	if !strings.HasPrefix(cands[0].Reason, "BLOCKED: ") {
		t.Errorf("reason missing BLOCKED: prefix: %q", cands[0].Reason)
	}
}

func TestCandidatesBlockedBeadsDedupLiveWorker(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{hungWorker("live-1")}}
	s := New(Config{Enabled: true}, store, nil)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "live-1", Reason: "retry exhausted"}}
	})

	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want 1: %+v", len(cands), cands)
	}
	if cands[0].Record.BeadID != "live-1" {
		t.Errorf("candidate bead ID = %q, want live-1", cands[0].Record.BeadID)
	}
	if strings.HasPrefix(cands[0].Reason, "BLOCKED: ") {
		t.Errorf("live worker was reported as blocked: %q", cands[0].Reason)
	}
}

func TestCandidatesBlockedBeadsNilFunc(t *testing.T) {
	store := &fakeStore{}
	s := New(Config{Enabled: true}, store, nil)
	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 0 {
		t.Errorf("nil BlockedBeads produced %d candidate(s): %+v", len(cands), cands)
	}
}

// Regression (2026-08-30, srv-smart-engine-v4bpl): a stale spawn-failure record
// from a superseded attempt stayed in BlockedBeads; when a healthy new worker
// spawned, Candidates only marked beads live if the WORKER itself had a
// candidacy reason, so the stale BLOCKED record survived and Pan abandoned a
// 2-minute-old worker. Any live worker row must suppress blocked candidacy.
func TestCandidates_BlockedBeadSuppressedByAnyLiveWorker(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{
		// healthy young worker: no heartbeat issue, no budget, no nudges
		{BeadID: "srv-smart-engine-v4bpl", State: registry.StateWorking,
			StartedAt: time.Now(), LastHeartbeat: time.Now()},
	}}
	s := New(Config{Enabled: true, NudgeCap: 3, HungAfter: 5 * time.Minute}, store, nil)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{
			{BeadID: "srv-smart-engine-v4bpl", Reason: "spawn failed: worker stale", Since: time.Now().Add(-90 * time.Minute)},
			{BeadID: "no-worker-bead", Reason: "retry exhausted", Since: time.Now().Add(-time.Hour)},
		}
	})
	cands, err := s.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	got := map[string]bool{}
	for _, c := range cands {
		got[c.Record.BeadID] = true
	}
	if got["srv-smart-engine-v4bpl"] {
		t.Error("blocked candidacy for a bead with a live worker row must be suppressed")
	}
	if !got["no-worker-bead"] {
		t.Error("blocked candidacy for a bead with NO worker row must remain")
	}
}

// Regression (srv-smart-engine-iz5z7 2026-08-30): a workerless bead that Pan
// judges as abandon-with-summary must be closed daemon-side. Enqueuing the
// command for a dead worker produces a stable deadlock — no one consumes it.
func TestTickAbandonWorkerlessExecutesDirectly(t *testing.T) {
	store := &fakeStore{}
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "bead-dead", "action": "abandon-with-summary", "message": "workerless"}]`, nil
	}

	var handled []string
	s := New(Config{Enabled: true, Interval: time.Second}, store, runner)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "bead-dead", Reason: "retry exhausted"}}
	})
	s.SetOnAbandonWithSummary(func(beadID string) error {
		handled = append(handled, beadID)
		return nil
	})
	s.Tick(context.Background())

	if len(handled) != 1 || handled[0] != "bead-dead" {
		t.Fatalf("direct abandon handler called %v, want [bead-dead]", handled)
	}
	if len(store.enqueued) != 0 {
		t.Errorf("workerless abandon enqueued a worker command: %+v", store.enqueued)
	}
}

func TestTickAbandonWithLiveWorkerStillEnqueues(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{hungWorker("bead-live")}}
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "bead-live", "action": "abandon-with-summary", "message": "hung"}]`, nil
	}

	var handled []string
	s := New(Config{Enabled: true, Interval: time.Second}, store, runner)
	s.SetOnAbandonWithSummary(func(beadID string) error {
		handled = append(handled, beadID)
		return nil
	})
	s.Tick(context.Background())

	if len(handled) != 0 {
		t.Errorf("direct abandon handler called for live worker: %v", handled)
	}
	if len(store.enqueued) != 1 || store.enqueued[0].kind != registry.CmdAbandonWithSummary {
		t.Fatalf("live worker abandon not enqueued: %+v", store.enqueued)
	}
}

func TestTickStalePendingAbandonExecutesDirectly(t *testing.T) {
	store := &fakeStore{
		pending: []registry.Command{{
			BeadID:    "bead-stale",
			Kind:      registry.CmdAbandonWithSummary,
			Payload:   "stale",
			CreatedAt: time.Now().Add(-3 * time.Second),
		}},
	}
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "bead-stale", "action": "abandon-with-summary", "message": "still stuck"}]`, nil
	}

	var handled []string
	s := New(Config{Enabled: true, Interval: time.Second, AbandonStaleThreshold: 2 * time.Second}, store, runner)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "bead-stale", Reason: "retry exhausted"}}
	})
	s.SetOnAbandonWithSummary(func(beadID string) error {
		handled = append(handled, beadID)
		return nil
	})
	s.Tick(context.Background())

	if len(handled) != 1 || handled[0] != "bead-stale" {
		t.Fatalf("stale pending abandon not handled: %v", handled)
	}
	if len(store.enqueued) != 0 {
		t.Errorf("stale pending abandon enqueued another command: %+v", store.enqueued)
	}
}

func TestTickFreshPendingAbandonStillDeduplicates(t *testing.T) {
	store := &fakeStore{
		pending: []registry.Command{{
			BeadID:    "bead-fresh",
			Kind:      registry.CmdAbandonWithSummary,
			Payload:   "fresh",
			CreatedAt: time.Now(),
		}},
		workers: []registry.WorkerRecord{hungWorker("bead-fresh")},
	}
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "bead-fresh", "action": "abandon-with-summary", "message": "same"}]`, nil
	}

	var handled []string
	s := New(Config{Enabled: true, Interval: time.Second, AbandonStaleThreshold: 2 * time.Second}, store, runner)
	s.SetOnAbandonWithSummary(func(beadID string) error {
		handled = append(handled, beadID)
		return nil
	})
	s.Tick(context.Background())

	if len(handled) != 0 {
		t.Errorf("fresh pending abandon called direct handler: %v", handled)
	}
	if len(store.enqueued) != 0 {
		t.Errorf("fresh pending abandon enqueued a duplicate: %+v", store.enqueued)
	}
}
