package pan

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
)

func TestTickRepairExecutesBlockedHandler(t *testing.T) {
	store := &fakeStore{}
	var handled []string
	var events []struct{}

	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "blocked-1", "action": "repair:reset-retries", "message": ""}]`, nil
	}

	s := New(Config{Enabled: true}, store, runner)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "blocked-1", Reason: "retry exhausted", Since: time.Now()}}
	})
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"reset-retries": func(beadID string) error {
			handled = append(handled, beadID)
			return nil
		},
	})
	s.SetOnRepairEvent(func(beadID, action, result, detail string) {
		events = append(events, struct{}{})
		if beadID != "blocked-1" {
			t.Errorf("event bead_id = %q, want blocked-1", beadID)
		}
		if action != "reset-retries" {
			t.Errorf("event action = %q, want reset-retries", action)
		}
		if result != "ok" {
			t.Errorf("event result = %q, want ok", result)
		}
	})

	s.Tick(context.Background())

	if len(handled) != 1 || handled[0] != "blocked-1" {
		t.Fatalf("handler called %v, want [blocked-1]", handled)
	}
	if len(events) != 1 {
		t.Fatalf("got %d repair events, want 1", len(events))
	}
	if len(store.enqueued) != 0 {
		t.Errorf("repair enqueued a worker command: %+v", store.enqueued)
	}
}

func TestTickRepairRefusesLiveWorker(t *testing.T) {
	store := &fakeStore{workers: []registry.WorkerRecord{
		hungWorker("live-1"),
	}}
	var handled []string
	var lastEvent struct {
		beadID, action, result, detail string
	}

	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "live-1", "action": "repair:reset-retries", "message": ""}]`, nil
	}

	s := New(Config{Enabled: true}, store, runner)
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"reset-retries": func(beadID string) error {
			handled = append(handled, beadID)
			return nil
		},
	})
	s.SetOnRepairEvent(func(beadID, action, result, detail string) {
		lastEvent = struct {
			beadID, action, result, detail string
		}{beadID, action, result, detail}
	})

	s.Tick(context.Background())

	if len(handled) != 0 {
		t.Fatalf("handler called despite live worker: %v", handled)
	}
	if lastEvent.result != "error" {
		t.Fatalf("event result = %q, want error", lastEvent.result)
	}
	if lastEvent.detail != "refused: bead has a live worker row" {
		t.Errorf("event detail = %q, want live-worker refusal", lastEvent.detail)
	}
}

func TestTickRepairUnknownActionRejectedAndRemembered(t *testing.T) {
	store := &fakeStore{}
	var events int

	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "blocked-1", "action": "repair:not-configured", "message": ""}]`, nil
	}

	s := New(Config{Enabled: true}, store, runner)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "blocked-1", Reason: "retry exhausted", Since: time.Now()}}
	})
	s.SetRepairHandlers(map[string]func(beadID string) error{})
	s.SetOnRepairEvent(func(beadID, action, result, detail string) {
		events++
	})

	s.Tick(context.Background())

	if events != 0 {
		t.Errorf("unknown repair action emitted %d repair events, want 0", events)
	}
	key := "blocked-1/repair:not-configured"
	if !s.rejectedActions[key] {
		t.Errorf("unknown repair action not remembered; rejectedActions = %+v", s.rejectedActions)
	}

	// Second tick with the same invented action must be a silent no-op.
	events = 0
	s.Tick(context.Background())
	if events != 0 {
		t.Errorf("second tick emitted %d repair events, want 0", events)
	}
}

func TestTickRepairRateLimit(t *testing.T) {
	store := &fakeStore{}
	calls := 0

	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "blocked-1", "action": "repair:reset-retries", "message": ""}]`, nil
	}

	s := New(Config{Enabled: true}, store, runner)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "blocked-1", Reason: "retry exhausted", Since: time.Now()}}
	})
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"reset-retries": func(beadID string) error {
			calls++
			return nil
		},
	})

	s.Tick(context.Background())
	if calls != 1 {
		t.Fatalf("first tick: handler called %d times, want 1", calls)
	}

	s.Tick(context.Background())
	if calls != 1 {
		t.Fatalf("second tick within hour: handler called %d times, want 1", calls)
	}
}

func TestTickRepairHandlerError(t *testing.T) {
	store := &fakeStore{}
	var lastEvent struct {
		beadID, action, result, detail string
	}

	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "blocked-1", "action": "repair:reset-retries", "message": ""}]`, nil
	}

	s := New(Config{Enabled: true}, store, runner)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "blocked-1", Reason: "retry exhausted", Since: time.Now()}}
	})
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"reset-retries": func(beadID string) error {
			return fmt.Errorf("reset failed")
		},
	})
	s.SetOnRepairEvent(func(beadID, action, result, detail string) {
		lastEvent = struct {
			beadID, action, result, detail string
		}{beadID, action, result, detail}
	})

	s.Tick(context.Background())

	if lastEvent.result != "error" {
		t.Fatalf("event result = %q, want error", lastEvent.result)
	}
	if lastEvent.detail != "reset failed" {
		t.Errorf("event detail = %q, want handler error", lastEvent.detail)
	}
}

// TestRepairBeadStoreWhitelisted: the bead-store corruption repair must be
// an allowed repair action and must route to a registered daemon-side
// handler (which restores .beads/issues.jsonl from a .br_history snapshot
// and unblocks the project only on verified open-count recovery).
func TestRepairBeadStoreWhitelisted(t *testing.T) {
	if !isAllowedAction("repair:repair-bead-store") {
		t.Fatal("repair:repair-bead-store not allowed by isAllowedAction")
	}

	store := &fakeStore{}
	var handled []string
	runner := func(ctx context.Context, cfg Config, prompt string) (string, error) {
		return `[{"bead_id": "proj-abc", "action": "repair:repair-bead-store", "message": ""}]`, nil
	}

	s := New(Config{Enabled: true}, store, runner)
	s.SetBlockedBeads(func() []BlockedBead {
		return []BlockedBead{{BeadID: "proj-abc", Reason: "bead store corrupt", Since: time.Now()}}
	})
	s.SetRepairHandlers(map[string]func(beadID string) error{
		"repair-bead-store": func(beadID string) error {
			handled = append(handled, beadID)
			return nil
		},
	})
	s.SetOnRepairEvent(func(beadID, action, result, detail string) {})

	s.Tick(context.Background())

	if len(handled) != 1 || handled[0] != "proj-abc" {
		t.Fatalf("handler called %v, want [proj-abc]", handled)
	}
	if len(store.enqueued) != 0 {
		t.Errorf("repair enqueued a worker command: %+v", store.enqueued)
	}
}
