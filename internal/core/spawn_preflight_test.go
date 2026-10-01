package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// fmsfe-a2u (2026-09-30 P0): post-restart reconstructed beads carried an
// empty Type, no-opping every scheduler type filter — the approval gate was
// spawned 4x and its worker exit released the DAG. The spawn funnel must
// fail closed on unverifiable or non-executable types.

func mockOverseerForPreflight(t *testing.T) *Overseer {
	t.Helper()
	o := NewOverseer(DefaultConfig())
	o.beadsClient = newMockBeadsClient()
	o.beadAnalyzer = NewBeadAnalyzer([]types.Project{
		{Name: "proj", Path: "/tmp/proj", Type: types.RepoTypeSingle},
	})
	return o
}

func TestSpawnPreflight_EmptyTypeBlocked(t *testing.T) {
	o := mockOverseerForPreflight(t)
	bead := types.Bead{ID: "proj-abc1", Project: "proj", Status: types.StatusOpen, Title: "reconstructed"}
	err := o.spawnPreflightBead(bead, "/tmp/proj")
	if err == nil {
		t.Fatal("expected empty-Type spawn to be blocked")
	}
	var sup *SpawnSuppressedError
	if !errors.As(err, &sup) {
		t.Fatalf("expected SpawnSuppressedError, got %T: %v", err, sup)
	}
	if !strings.Contains(sup.Error(), "empty Type") {
		t.Errorf("error should mention empty Type, got: %s", sup.Error())
	}
}

func TestSpawnPreflight_ExecutableTypeAllowed(t *testing.T) {
	o := mockOverseerForPreflight(t)
	bead := types.Bead{ID: "proj-abc2", Project: "proj", Type: "task", Status: types.StatusOpen, Title: "real work"}
	if err := o.spawnPreflightBead(bead, "/tmp/proj"); err != nil {
		t.Fatalf("executable bead should pass preflight, got: %v", err)
	}
}

func TestSpawnPreflight_NonExecutableTypesBlocked(t *testing.T) {
	for _, tt := range []string{"approval-gate", "approval", "epic", "hold", "Approval-Gate", "HOLD"} {
		o := mockOverseerForPreflight(t)
		bead := types.Bead{ID: "proj-gate1", Project: "proj", Type: tt, Status: types.StatusOpen, Title: "gate"}
		err := o.spawnPreflightBead(bead, "/tmp/proj")
		if err == nil {
			t.Errorf("type %q should be blocked at spawn funnel", tt)
		}
	}
}

func TestSpawnPreflight_HoldLabelBlocked(t *testing.T) {
	o := mockOverseerForPreflight(t)
	bead := types.Bead{ID: "proj-hold1", Project: "proj", Type: "task", Status: types.StatusOpen,
		Title: "paused", Labels: []string{"hold"}}
	if err := o.spawnPreflightBead(bead, "/tmp/proj"); err == nil {
		t.Fatal("hold-labeled bead should be blocked at spawn funnel")
	}
}

// SpawnWorker stores the store-truth refresh: an empty-Type in-memory copy
// is refreshed from the store before the preflight runs.
func TestSpawnWorker_RefreshesTypeFromStore(t *testing.T) {
	o := mockOverseerForPreflight(t)
	mock := o.beadsClient.(*mockBeadsClient)
	// Store truth: an approval gate. In-memory copy lost its Type (post-restart
	// reconstruction) — without the refresh the type filters would no-op.
	mock.beads["proj-gate2"] = types.Bead{ID: "proj-gate2", Project: "proj",
		Type: "approval-gate", Status: types.StatusOpen, Title: "gate"}
	bead := types.Bead{ID: "proj-gate2", Project: "proj", Status: types.StatusOpen, Title: "gate"}
	err := o.spawnWorker(bead)
	if err == nil {
		t.Fatal("expected spawn of store-truth approval gate to be blocked")
	}
	var sup *SpawnSuppressedError
	if !errors.As(err, &sup) || !strings.Contains(sup.Error(), "non-executable") {
		t.Fatalf("expected non-executable suppression, got: %v", err)
	}
}

func TestSpawnWorker_EmptyTypeEverywhereBlocked(t *testing.T) {
	o := mockOverseerForPreflight(t)
	mock := o.beadsClient.(*mockBeadsClient)
	// Store truth is ALSO empty (fully reconstructed bead) — fail closed.
	mock.beads["proj-e1"] = types.Bead{ID: "proj-e1", Project: "proj",
		Status: types.StatusOpen, Title: "ghost"}
	err := o.spawnWorker(types.Bead{ID: "proj-e1", Project: "proj", Status: types.StatusOpen, Title: "ghost"})
	if err == nil || !strings.Contains(err.Error(), "empty Type") {
		t.Fatalf("expected empty-Type fail-closed block, got: %v", err)
	}
}

// Identity preflight: a non-daemon owner without a PAT blocks the spawn
// (auto-resumes when the PAT lands); daemon-owned projects are inert.
func TestSpawnPreflightPublishIdentity_MissingOwnerPATBlocked(t *testing.T) {
	o := mockOverseerForPreflight(t)
	o.userContexts = map[string]*UserContext{"alice": {
		Username: "alice", HomeDir: t.TempDir(),
		Config: UserConfig{Projects: []UserProject{{Name: "proj", Path: "/tmp/proj"}}},
	}}
	bead := types.Bead{ID: "proj-id1", Project: "proj", Type: "task", Status: types.StatusOpen, Title: "work"}
	err := o.spawnPreflightPublishIdentity(bead, "/tmp/proj")
	if err == nil {
		t.Fatal("expected missing owner PAT to block spawn")
	}
	if !strings.Contains(err.Error(), "identity") {
		t.Errorf("error should mention identity, got: %s", err)
	}
}

func TestSpawnPreflightPublishIdentity_OwnerPATResolves(t *testing.T) {
	home := t.TempDir()
	o := mockOverseerForPreflight(t)
	o.userContexts = map[string]*UserContext{"alice": {
		Username: "alice", HomeDir: home,
		Config: UserConfig{Projects: []UserProject{{Name: "proj", Path: "/tmp/proj"}}},
	}}
	if err := os.MkdirAll(filepath.Join(home, ".hydra"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".hydra", ".github-token"), []byte("ghp_x"), 0600); err != nil {
		t.Fatal(err)
	}
	bead := types.Bead{ID: "proj-id2", Project: "proj", Type: "task", Status: types.StatusOpen, Title: "work"}
	if err := o.spawnPreflightPublishIdentity(bead, "/tmp/proj"); err != nil {
		t.Fatalf("resolved owner PAT should not block spawn: %v", err)
	}
}

func TestSpawnPreflightPublishIdentity_DaemonOwnedInert(t *testing.T) {
	o := mockOverseerForPreflight(t)
	// No userContexts → daemon-owned → preflight must not block.
	bead := types.Bead{ID: "proj-id3", Project: "proj", Type: "task", Status: types.StatusOpen, Title: "work"}
	if err := o.spawnPreflightPublishIdentity(bead, "/tmp/proj"); err != nil {
		t.Fatalf("daemon-owned project should not block spawn: %v", err)
	}
}
