package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// setupEscalationCloser builds a BeadCloser wired with a mock beads client,
// a validation service whose retry count can be driven to max rounds, and an
// event sink writing to a temp central dir + repo mirror.
func setupEscalationCloser(t *testing.T, projectPath string) (*BeadCloser, *mockBeadsClient, *EventSink) {
	t.Helper()

	client := newMockBeadsClient()
	cfg := &Config{}
	cfg.Validation.MaxRounds = 3

	rm := NewRetryManager(time.Millisecond, time.Millisecond)
	vs := NewValidationService(cfg, rm, client)

	sink := NewEventSink(filepath.Join(t.TempDir(), "events"))

	closer := NewBeadCloser(client, nil, nil, vs, cfg)
	closer.SetEventSink(sink)
	return closer, client, sink
}

// TestRejectClose_EscalationLabelsBeadAndEmitsEvent drives a bead to max
// close rejections and asserts the escalation is durable: the `escalated`
// label lands on the store bead, the status stays in_progress (parked, not
// reopened), and a typed bead.escalated event is emitted.
func TestRejectClose_EscalationLabelsBeadAndEmitsEvent(t *testing.T) {
	projectPath := t.TempDir()
	closer, client, sink := setupEscalationCloser(t, projectPath)

	bead := types.Bead{
		ID:       types.BeadID("proj-esc1"),
		Project:  "proj",
		Title:    "escalation test bead",
		Type:     "implement",
		Status:   types.StatusInProgress,
		Priority: 3,
	}
	client.addBead(bead)

	// Drive validation retry count to max rounds (3).
	for i := 0; i < 3; i++ {
		closer.validationService.retryManager.IncrementValidationRetry(string(bead.ID))
	}

	err := closer.rejectClose(&bead, projectPath, "repo", "empty-diff")
	if err == nil {
		t.Fatal("expected escalation error, got nil")
	}
	if !strings.Contains(err.Error(), "escalated after 3 rejections") {
		t.Fatalf("unexpected error text: %v", err)
	}

	// Durable label on the store bead.
	fresh, getErr := client.GetBead(projectPath, "repo", bead.ID)
	if getErr != nil {
		t.Fatalf("GetBead: %v", getErr)
	}
	if fresh == nil {
		t.Fatal("bead missing from mock store")
	}
	if !hasBeadLabel(fresh.Labels, escalatedLabel) {
		t.Fatalf("expected %q label on escalated bead, got labels: %v", escalatedLabel, fresh.Labels)
	}
	if !hasBeadLabel(fresh.Labels, "needs-human") {
		t.Fatalf("expected needs-human label, got labels: %v", fresh.Labels)
	}

	// Status must NOT have been flipped to open: bare in_progress is what
	// made Charon reopen the bead into the churn loop.
	if fresh.Status != types.StatusInProgress {
		t.Fatalf("escalated bead status changed to %q; want in_progress (parked)", fresh.Status)
	}

	// Typed event emitted to the central stream and the repo mirror.
	central := filepath.Join(sink.centralDir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	events, readErr := ReadEvents(central)
	if readErr != nil {
		t.Fatalf("ReadEvents(central): %v", readErr)
	}
	found := false
	for _, ev := range events {
		if ev.Type == "bead.escalated" && ev.Bead == string(bead.ID) {
			found = true
			if ev.Reason != "empty-diff" {
				t.Fatalf("event reason = %q, want %q", ev.Reason, "empty-diff")
			}
		}
	}
	if !found {
		t.Fatalf("no bead.escalated event in central stream; events: %+v", events)
	}

	repoEvents, readErr := ReadEvents(filepath.Join(projectPath, ".hydra", "events.jsonl"))
	if readErr != nil {
		t.Fatalf("ReadEvents(repo mirror): %v", readErr)
	}
	found = false
	for _, ev := range repoEvents {
		if ev.Type == "bead.escalated" && ev.Bead == string(bead.ID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no bead.escalated event in repo mirror; events: %+v", repoEvents)
	}

	// .hydra/ must be gitignored by the mirror write.
	gi, _ := os.ReadFile(filepath.Join(projectPath, ".gitignore"))
	if !strings.Contains(string(gi), ".hydra/") {
		t.Fatalf(".gitignore missing .hydra/ line: %q", gi)
	}
}

// TestRejectClose_BelowMaxRoundsReopensWithoutEscalation guards the
// non-escalation branch: a rejection below max rounds reopens the bead and
// must NOT apply the escalated label.
func TestRejectClose_BelowMaxRoundsReopensWithoutEscalation(t *testing.T) {
	projectPath := t.TempDir()
	closer, client, _ := setupEscalationCloser(t, projectPath)

	bead := types.Bead{
		ID:       types.BeadID("proj-rej1"),
		Project:  "proj",
		Title:    "reject test bead",
		Type:     "implement",
		Status:   types.StatusInProgress,
		Priority: 3,
	}
	client.addBead(bead)

	err := closer.rejectClose(&bead, projectPath, "repo", "empty-diff")
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("expected rejection error, got: %v", err)
	}

	fresh, _ := client.GetBead(projectPath, "repo", bead.ID)
	if fresh.Status != types.StatusOpen {
		t.Fatalf("status = %q, want open after below-max rejection", fresh.Status)
	}
	if hasBeadLabel(fresh.Labels, escalatedLabel) {
		t.Fatalf("escalated label applied on below-max rejection; labels: %v", fresh.Labels)
	}
}
