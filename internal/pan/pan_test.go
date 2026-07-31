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
	listErr error
}

func (f *fakeStore) ListWorkers(states ...registry.WorkerState) ([]registry.WorkerRecord, error) {
	if f.listErr != nil {
		return nil, f.listErr
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
