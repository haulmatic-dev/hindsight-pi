package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	// The guard reads the epic's dependents from Metadata["dependent_ids"]
	// (populated by `br show` parsing in production).
	for _, dep := range childDeps {
		if dep == epicID {
			epic.Metadata = map[string]string{"dependent_ids": childID}
		}
	}
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

func TestPersistClosedEpics_RefusesOnStoreGetError(t *testing.T) {
	o, mock := setupEpicGuardOverseer(t)
	epic, child := epicAndChild("proj-epic1", "proj-task1", types.StatusClosed, []string{"proj-epic1"})
	mock.addBead(epic)
	mock.addBead(child)
	mock.getErr = errListBoom

	o.dag.AddNode(epic)
	closed := o.dag.CloseReadyEpics()
	o.persistClosedEpics(closed)

	if len(mock.closeCalls) != 0 {
		t.Fatalf("store read failure must refuse close; got %v", mock.closeCalls)
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

// TestPersistClosedEpics_BrOpenChildrenRefusal_ClassifiedAsRefusal verifies
// the srv-jobfile master-epic fix: when compile-time epic-child dependency
// links failed (br single-parent constraint), neither the DAG nor the
// Dependencies-based store scan can see the open children, so CloseReadyEpics
// proposes the close and the epic_close_guard passes vacuously. The last line
// of defense is br's own open-children refusal (closeBeadOnce refuses to
// force-close epics). The overseer must classify that error as an
// epic_close_refused event with reason open_children and throttle re-checks,
// not log it as a generic persistence failure.
func TestPersistClosedEpics_BrOpenChildrenRefusal_ClassifiedAsRefusal(t *testing.T) {
	o, mock := setupEpicGuardOverseer(t)
	epic, child := epicAndChild("proj-epic1", "proj-task1", types.StatusOpen, nil) // link FAILED: no dep on epic
	mock.addBead(epic)
	mock.addBead(child)
	mock.closeErr = errors.New("refusing to force-close epic proj-epic1: br reports open children (Warning: Skipped proj-epic: parent issue has 33/33 open children (use --force to close anyway))")

	eventsDir := t.TempDir()
	o.eventSink = NewEventSink(eventsDir)

	o.dag.AddNode(epic)
	closed := o.dag.CloseReadyEpics()
	if len(closed) != 1 {
		t.Fatalf("expected CloseReadyEpics to propose epic1 (children invisible), got %v", closed)
	}

	o.persistClosedEpics(closed)

	if !o.epicCloseGuard.recentlyRefused("proj-epic1", time.Now()) {
		t.Fatal("br open-children refusal must be recorded as a refusal (throttled)")
	}
	// Event stream must carry the typed refusal.
	data, err := os.ReadDir(eventsDir)
	if err != nil || len(data) == 0 {
		t.Fatalf("expected event files, got %v (%v)", data, err)
	}
	found := false
	for _, f := range data {
		b, err := os.ReadFile(filepath.Join(eventsDir, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `"epic_close_refused"`) && strings.Contains(string(b), `"open_children"`) {
			found = true
		}
	}
	if !found {
		t.Fatal("expected epic_close_refused/open_children event on the stream")
	}
}

// TestFindProjectPathForBead_NamespacedID is the hydra-ysaz regression: bead
// IDs are user-namespaced ("ajantha-srv-smart-engine-1u3") while project.Name
// is not ("srv-smart-engine"); the old prefix-only match never fired and epic
// auto-close persistence silently no-opped forever (185x "Could not find
// project path for epic" in one campaign).
func TestFindProjectPathForBead_NamespacedID(t *testing.T) {
	o, mock := setupEpicGuardOverseer(t)
	o.projects = []types.Project{{Name: "srv-smart-engine", Path: filepath.Join(t.TempDir(), "srv-smart-engine"), Type: types.RepoTypeSingle}}
	epic := types.Bead{
		ID:      "ajantha-srv-smart-engine-1u3",
		Project: "srv-smart-engine", // set from the store context during ListBeads
		Status:  types.StatusOpen,
		Type:    "epic",
	}
	mock.addBead(epic)
	o.dag.AddNode(epic)

	path, _ := o.findProjectPathForBead("ajantha-srv-smart-engine-1u3")
	if path == "" {
		t.Fatal("namespaced epic ID must resolve to its project path via the DAG bead record")
	}

	closed := o.dag.CloseReadyEpics()
	o.persistClosedEpics(closed)
	if len(mock.closeCalls) != 1 {
		t.Fatalf("epic with no dependents must close, got %v", mock.closeCalls)
	}
}

// TestFindProjectPathForBead_NamespacedIDSegmentFallback covers ghost nodes
// and any bead whose DAG record lacks a Project: the exact-segment fallback
// ("-<name>-" inside the ID) must resolve, and must not substring-match a
// shorter project name.
func TestFindProjectPathForBead_NamespacedIDSegmentFallback(t *testing.T) {
	o, _ := setupEpicGuardOverseer(t)
	o.projects = []types.Project{
		{Name: "engine", Path: "/wrong", Type: types.RepoTypeSingle},
		{Name: "srv-smart-engine", Path: "/right", Type: types.RepoTypeSingle},
	}
	// No DAG node at all: pure ID matching.
	path, _ := o.findProjectPathForBead("ajantha-srv-smart-engine-1u3")
	if path != "/right" {
		t.Fatalf("segment fallback resolved %q, want /right", path)
	}
}
