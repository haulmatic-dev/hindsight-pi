package core

import (
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func setupTestProjectResolver(t *testing.T, projectName, projectPath string) *ProjectResolver {
	t.Helper()
	return NewProjectResolver([]types.Project{
		{Name: projectName, Path: projectPath, Type: types.RepoTypeSingle},
	})
}

func TestBeadCloser_CloseBlockedOnFailed_CreatesFixBead(t *testing.T) {
	mockBeads := newMockBeadsClient()
	bead := types.Bead{
		ID:       "test-bead-1",
		Title:    "GREEN: stub feature",
		Project:  "test-bead",
		Priority: 1,
		Status:   types.StatusInProgress,
	}
	mockBeads.addBead(bead)

	cfg := DefaultConfig()
	cfg.Validation.CloseBlockedOnFailed = true

	projectPath := t.TempDir()
	// BeadID "test-bead-1".Project() returns "test-bead" (the part
	// before the last "-" when no "/" is present). Register the
	// project under that name.
	resolver := setupTestProjectResolver(t, "test-bead", projectPath)

	closer := NewBeadCloser(
		mockBeads,
		nil, // intelligence hub not needed for this test
		resolver,
		NewValidationService(cfg, NewRetryManager(5*time.Second, 5*time.Minute), mockBeads),
		cfg,
	)

	// BeadID.Project() parses out the project prefix; we keep the
	// BeadID value short so the mock's lookup matches the addBead
	// key. The Project field on the bead itself drives resolution.
	worker := &types.Worker{BeadID: types.BeadID("test-bead-1")}

	if err := closer.Close(worker, false, 1); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Confirm CloseBead was called with reason=failed.
	calls := mockBeads.closeCalls
	if len(calls) != 1 || calls[0].reason != "failed" {
		t.Fatalf("expected 1 close call with reason=failed, got %+v", calls)
	}

	// Confirm a fix bead was created.
	if len(mockBeads.beads) < 2 {
		t.Fatalf("expected a fix bead to be created, only have %d beads", len(mockBeads.beads))
	}
	var fixBead *types.Bead
	for id, b := range mockBeads.beads {
		if strings.Contains(id, "fix") {
			b := b
			fixBead = &b
			break
		}
	}
	if fixBead == nil {
		t.Fatalf("no fix bead found in mockBeads; have %+v", mockBeads.beads)
	}
	if fixBead.Type != "fix" {
		t.Errorf("fix bead type = %q, want \"fix\"", fixBead.Type)
	}
	hasAutoFix := false
	for _, l := range fixBead.Labels {
		if l == "auto-fix" {
			hasAutoFix = true
		}
	}
	if !hasAutoFix {
		t.Errorf("fix bead missing auto-fix label: %v", fixBead.Labels)
	}
}

func TestBeadCloser_CloseBlockedOnFailed_Disabled(t *testing.T) {
	mockBeads := newMockBeadsClient()
	bead := types.Bead{
		ID:       "test-bead-2",
		Title:    "GREEN: feature",
		Project:  "test-bead",
		Priority: 1,
		Status:   types.StatusInProgress,
	}
	mockBeads.addBead(bead)

	cfg := DefaultConfig()
	cfg.Validation.CloseBlockedOnFailed = false

	projectPath := t.TempDir()
	resolver := setupTestProjectResolver(t, "test-bead", projectPath)

	closer := NewBeadCloser(
		mockBeads,
		nil,
		resolver,
		NewValidationService(cfg, NewRetryManager(5*time.Second, 5*time.Minute), mockBeads),
		cfg,
	)

	worker := &types.Worker{BeadID: types.BeadID("test-bead-2")}
	if err := closer.Close(worker, false, 1); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// No fix bead should be created.
	if len(mockBeads.beads) != 1 {
		t.Errorf("expected only the original bead; got %d beads (fix bead created with flag off)", len(mockBeads.beads))
	}
}

func TestBeadCloser_CreateFixBeadWithNilBead(t *testing.T) {
	// When the bead lookup raced with the daemon lock and returned
	// nil, the fix-bead creation must still work using the worker
	// bead ID as the fallback.
	mockBeads := newMockBeadsClient()
	cfg := DefaultConfig()
	cfg.Validation.CloseBlockedOnFailed = true
	resolver := setupTestProjectResolver(t, "test-bead", t.TempDir())

	closer := NewBeadCloser(
		mockBeads,
		nil,
		resolver,
		NewValidationService(cfg, NewRetryManager(5*time.Second, 5*time.Minute), mockBeads),
		cfg,
	)

	// Pass nil for bead (simulates GetBead returning nil due to
	// daemon lock); the function must still create a fix bead
	// using workerBeadID as the source of truth.
	if err := closer.createFixBeadForFailedClose(nil, "/tmp/test", "", "test-bead-original-id"); err != nil {
		t.Fatalf("createFixBeadForFailedClose with nil bead: %v", err)
	}
	found := false
	for id := range mockBeads.beads {
		if id != "test-bead-original-id" {
			found = true
			t.Logf("created fix bead: %s", id)
		}
	}
	if !found {
		t.Errorf("expected a fix bead to be created with workerBeadID, none found")
	}
}

func TestBeadCloser_ShouldRunTDDValidation_Label(t *testing.T) {
	cfg := DefaultConfig()
	closer := NewBeadCloser(nil, nil, nil, nil, cfg)

	cases := []struct {
		name     string
		bead     types.Bead
		expected bool
	}{
		{"tdd-red label", types.Bead{ID: "x", Labels: []string{"tdd-red"}}, true},
		{"tdd-green label", types.Bead{ID: "x", Labels: []string{"tdd-green"}}, true},
		{"tdd-refactor label", types.Bead{ID: "x", Labels: []string{"tdd-refactor"}}, true},
		{"tdd-syntax label", types.Bead{ID: "x", Labels: []string{"tdd-syntax"}}, true},
		{"unrelated labels", types.Bead{ID: "x", Labels: []string{"fr", "p1"}}, false},
		{"legacy Type=refactor", types.Bead{ID: "x", Type: "refactor"}, true},
		{"neither", types.Bead{ID: "x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := closer.shouldRunTDDValidation(&tc.bead); got != tc.expected {
				t.Errorf("shouldRunTDDValidation() = %v, want %v", got, tc.expected)
			}
		})
	}
}

// TestBeadCloser_NeverClosesHumanGates reproduces the 2026-08-06 live
// incident: the daemon's close path closed srv-smart-engine-56z (Type
// "approval") with verdict "accepted" after a worker review — human gates
// must require an explicit human `br close`. Spawn-side filtering (PR
// approval-bead-gate) is not enough; the close path needs the same guard.
func TestBeadCloser_NeverClosesHumanGates(t *testing.T) {
	for _, gateType := range []string{"approval", "approval-gate"} {
		t.Run(gateType, func(t *testing.T) {
			mockBeads := newMockBeadsClient()
			mockBeads.addBead(types.Bead{
				ID:       "test-bead-1",
				Title:    "APPROVE: plan",
				Project:  "test-bead",
				Type:     gateType,
				Priority: 1,
				Status:   types.StatusOpen,
			})

			cfg := DefaultConfig()
			projectPath := t.TempDir()
			resolver := setupTestProjectResolver(t, "test-bead", projectPath)
			closer := NewBeadCloser(
				mockBeads,
				nil,
				resolver,
				NewValidationService(cfg, NewRetryManager(5*time.Second, 5*time.Minute), mockBeads),
				cfg,
			)

			worker := &types.Worker{BeadID: types.BeadID("test-bead-1")}
			if err := closer.Close(worker, true, 0); err != nil {
				t.Fatalf("Close() error = %v", err)
			}

			if len(mockBeads.closeCalls) != 0 {
				t.Fatalf("human gate (type %s) must never be system-closed, got close calls: %+v", gateType, mockBeads.closeCalls)
			}
		})
	}
}
