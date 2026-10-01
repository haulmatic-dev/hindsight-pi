package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

var errListBoom = errors.New("list boom")

// setupEpicGuardOverseer builds a minimal Overseer with one project whose
// store is backed by the shared mockBeadsClient.
func setupEpicGuardOverseer(t *testing.T) (*Overseer, *mockBeadsClient) {
	t.Helper()
	projectPath := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Projects = []ProjectConfig{{Name: "proj", Path: projectPath, Type: "single"}}
	mock := newMockBeadsClient()
	o := &Overseer{
		config:      cfg,
		projects:    []types.Project{{Name: "proj", Path: projectPath, Type: types.RepoTypeSingle}},
		beadsClient: mock,
		completed:   map[string]bool{},
		dag:         NewDAG(),
	}
	return o, mock
}

func epicAndChild(epicID, childID string, childStatus types.BeadStatus, childDeps []string) (types.Bead, types.Bead) {
	epic := types.Bead{ID: types.BeadID(epicID), Project: "proj", Status: types.StatusOpen, Type: "epic"}
	child := types.Bead{ID: types.BeadID(childID), Project: "proj", Status: childStatus, Type: "task", Dependencies: toBeadIDs(childDeps)}
	return epic, child
}

func TestPersistClosedEpics_RefusesWhenOpenChildInStore(t *testing.T) {
	o, mock := setupEpicGuardOverseer(t)
	epic, child := epicAndChild("proj-epic1", "proj-task1", types.StatusOpen, []string{"proj-epic1"})
	mock.addBead(epic)
	mock.addBead(child)

	// DAG sees the epic with zero children (the fmsfe failure mode: child
	// invisible to the DAG build), so CloseReadyEpics proposes the close.
	o.dag.AddNode(epic)
	closed := o.dag.CloseReadyEpics()
	if len(closed) != 1 {
		t.Fatalf("expected CloseReadyEpics to propose epic1, got %v", closed)
	}

	o.persistClosedEpics(closed)

	if len(mock.closeCalls) != 0 {
		t.Fatalf("epic close must be refused while a store child is open; got closeCalls=%v", mock.closeCalls)
	}
	if !o.epicCloseGuard.recentlyRefused("proj-epic1", time.Now()) {
		t.Fatal("refusal must be recorded")
	}
}

func TestPersistClosedEpics_ClosesWhenStoreConfirmsNoOpenDependents(t *testing.T) {
	o, mock := setupEpicGuardOverseer(t)
	epic, child := epicAndChild("proj-epic1", "proj-task1", types.StatusClosed, []string{"proj-epic1"})
	mock.addBead(epic)
	mock.addBead(child)

	o.dag.AddNode(epic)
	closed := o.dag.CloseReadyEpics()
	if len(closed) != 1 {
		t.Fatalf("expected proposal, got %v", closed)
	}

	o.persistClosedEpics(closed)

	if len(mock.closeCalls) != 1 {
		t.Fatalf("expected exactly one close, got %v", mock.closeCalls)
	}
	if mock.closeCalls[0].beadID.String() != "proj-epic1" {
		t.Fatalf("wrong bead closed: %v", mock.closeCalls[0].beadID)
	}
}

func TestPersistClosedEpics_RefusesOnStoreListError(t *testing.T) {
	o, mock := setupEpicGuardOverseer(t)
	epic, child := epicAndChild("proj-epic1", "proj-task1", types.StatusClosed, []string{"proj-epic1"})
	mock.addBead(epic)
	mock.addBead(child)
	mock.listErr = errListBoom

	o.dag.AddNode(epic)
	closed := o.dag.CloseReadyEpics()
	o.persistClosedEpics(closed)

	if len(mock.closeCalls) != 0 {
		t.Fatalf("listing failure must refuse close; got %v", mock.closeCalls)
	}
}

func TestPersistClosedEpics_SkipsProjectPathResolutionWhenRefused(t *testing.T) {
	// Regression guard: an epic that cannot be resolved to a project path must
	// NOT be closed (this skip was what previously masked the bug, and must
	// remain a refusal, not a close).
	o, mock := setupEpicGuardOverseer(t)
	// Epic's prefix matches no configured project ("other-epic1" vs "proj").
	epic := types.Bead{ID: "other-epic1", Project: "other", Status: types.StatusOpen, Type: "epic"}
	mock.addBead(epic)
	o.dag.AddNode(epic)
	closed := o.dag.CloseReadyEpics()
	if len(closed) != 1 {
		t.Fatalf("expected proposal, got %v", closed)
	}
	o.persistClosedEpics(closed)
	if len(mock.closeCalls) != 0 {
		t.Fatalf("unresolvable epic must not be closed; got %v", mock.closeCalls)
	}
}

func TestPersistClosedEpics_RefusalThrottledWithinTTL(t *testing.T) {
	o, mock := setupEpicGuardOverseer(t)
	epic, child := epicAndChild("proj-epic1", "proj-task1", types.StatusOpen, []string{"proj-epic1"})
	mock.addBead(epic)
	mock.addBead(child)
	o.dag.AddNode(epic)

	closed := o.dag.CloseReadyEpics()
	o.persistClosedEpics(closed)
	o.epicCloseGuard.refusals["proj-epic1"] = time.Now() // ensure within TTL
	o.persistClosedEpics(closed)                         // re-proposed on next build
	// No panic, still refused:
	if len(mock.closeCalls) != 0 {
		t.Fatalf("close must stay refused within TTL; got %v", mock.closeCalls)
	}
}
