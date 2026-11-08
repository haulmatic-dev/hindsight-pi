package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// FR-006 regression suite: the close path adopted the shared resolver
// (internal/beadfiles.Resolve) with one-directional sibling-test expansion.
// One regression test per behavior-changed caller, asserting its new verdict.

func TestBeadfilesAdoption_SiblingTestExpansion(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "impl gains sibling test",
			in:   []string{"internal/core/service_starter.go"},
			want: []string{"internal/core/service_starter.go", "internal/core/service_starter_test.go"},
		},
		{
			name: "test-only declaration gains no impl scope",
			in:   []string{"internal/core/service_starter_test.go"},
			want: []string{"internal/core/service_starter_test.go"},
		},
		{
			name: "non-go impl unexpanded",
			in:   []string{"src/x/a.py"},
			want: []string{"src/x/a.py"},
		},
		{
			name: "js test file unexpanded",
			in:   []string{"src/x/a.test.js"},
			want: []string{"src/x/a.test.js"},
		},
		{
			name: "duplicates collapsed, sibling once",
			in:   []string{"a.go", "a.go", "b.go"},
			want: []string{"a.go", "a_test.go", "b.go", "b_test.go"},
		},
		{
			name: "nil input",
			in:   nil,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := siblingTestExpansion(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("siblingTestExpansion(%v) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("siblingTestExpansion(%v) = %v, want %v", tc.in, got, tc.want)
				}
			}
		})
	}
}

func TestBeadfilesAdoption_ResolveAdopted(t *testing.T) {
	// The bead-local parser is gone: resolution now goes through the shared
	// internal/beadfiles resolver, including its :symbol normalization.
	bead := &types.Bead{Metadata: map[string]string{"files": "internal/core/service_starter.go:Start"}}
	got := beadTargetFiles(bead)
	if len(got) != 2 || got[0] != "internal/core/service_starter.go" || got[1] != "internal/core/service_starter_test.go" {
		t.Fatalf("beadTargetFiles = %v, want clean impl + sibling test", got)
	}
	if beadTargetFiles(nil) != nil {
		t.Fatal("nil bead must resolve to nil")
	}
	if got := beadTargetFiles(&types.Bead{Description: "Files: TBD"}); len(got) != 0 {
		t.Fatalf("TBD-only declaration = %v, want empty", got)
	}
}

// Caller 1: hasTargetChanges scope match (scope_gate.go) — the close-path
// diff gate. A RED bead whose worker wrote only the sibling test file now
// counts as in-scope instead of empty-diff.
func TestBeadfilesAdoption_ScopeGate_SiblingTestInScope(t *testing.T) {
	bead := &types.Bead{Metadata: map[string]string{"files": "internal/core/service_starter.go"}}
	targets := beadTargetFiles(bead)
	changed := []string{"internal/core/service_starter_test.go"}
	if !hasTargetChanges(changed, targets) {
		t.Fatal("sibling-test-only diff must be in scope (no empty-diff reject)")
	}
	// A bead declaring only test files gains no impl scope: an impl-only
	// diff stays empty-diff and belongs to the GREEN bead.
	testOnly := beadTargetFiles(&types.Bead{Metadata: map[string]string{"files": "internal/core/service_starter_test.go"}})
	if len(testOnly) != 1 || testOnly[0] != "internal/core/service_starter_test.go" {
		t.Fatalf("test-only declaration expanded: %v", testOnly)
	}
	if hasTargetChanges([]string{"internal/core/service_starter.go"}, testOnly) {
		t.Fatal("impl-only diff against test-only declaration must stay out of scope")
	}
}

// Caller 2: prespawn_probe.go — the pre-spawn probe scopes gates with
// targets[0]; the adopted resolver's :symbol normalization must reach it and
// the sibling must not hijack the first slot.
func TestBeadfilesAdoption_PrespawnProbe_CleanScopeProbes(t *testing.T) {
	r := NewGateRunner(t.TempDir())
	gates := &GateSet{
		DaemonOwned: true,
		Test:        GateSpec{Cmd: "test <scope> = internal/core/service_starter.go", Parser: "exit-code"},
	}
	bead := &types.Bead{ID: "proj-bp1", Project: "proj",
		Metadata: map[string]string{"files": "internal/core/service_starter.go:Start"}}
	pass_, _ := PrespawnProbePasses(r, gates, bead, t.TempDir())
	if !pass_ {
		t.Error("probe must scope gates with the clean resolved path (symbol suffix stripped), not fail")
	}
}

// Caller 3: target_overlap.go — a live worker declaring an impl file now
// also holds its sibling test, so a candidate declaring only that test file
// conflicts instead of spawning concurrently against the same unit.
func TestBeadfilesAdoption_TargetOverlap_SiblingTestConflicts(t *testing.T) {
	mock := newMockBeadsClient()
	mock.beads["proj-2"] = types.Bead{ID: types.BeadID("proj-2"),
		Metadata: map[string]string{"files": "internal/core/service_starter.go"}}
	o := &Overseer{
		projects:    []types.Project{{Name: "proj", Path: "/tmp/proj"}},
		beadsClient: mock,
		activeWorkers: map[string]*types.Worker{
			"proj-2": {BeadID: "proj-2"},
		},
	}
	bead := &types.Bead{ID: types.BeadID("proj-1"),
		Metadata: map[string]string{"files": "internal/core/service_starter_test.go"}}
	id, conflict := o.targetScopeConflict(bead)
	if !conflict || id != "proj-2" {
		t.Errorf("targetScopeConflict = (%q, %v), want (proj-2, true): sibling test of a live worker's target must conflict", id, conflict)
	}
}

// Caller 4: satisfied_close.go — scoped gates run with the clean resolved
// scope, so a symbol-suffixed declaration no longer produces a mismatched
// gate scope.
func TestBeadfilesAdoption_SatisfiedClose_CleanScopeCloses(t *testing.T) {
	bead := types.Bead{
		ID:          "test-project-abc",
		Title:       "already done",
		Description: "Files: internal/core/service_starter.go:Start",
		Status:      types.StatusInProgress,
		Type:        "test",
	}
	o, mockBeads := setupSatisfiedOverseer(t, bead, "test <scope> = internal/core/service_starter.go")

	handler := BuildCloseSatisfied(o)
	if err := handler("test-project-abc"); err != nil {
		t.Fatalf("satisfied close must proceed with clean scope, got: %v", err)
	}
	if len(mockBeads.getCloseCalls()) != 1 {
		t.Fatalf("CloseBead calls = %d, want 1", len(mockBeads.getCloseCalls()))
	}
}

// Caller 5: fix_audit.go — the closure-audit re-verify gate is scoped with
// the clean resolved path, so a symbol-suffixed declaration verifies instead
// of contradicting.
func TestBeadfilesAdoption_FixAudit_CleanScopeVerifies(t *testing.T) {
	orig := types.Bead{
		ID:          "test-project-abc",
		Title:       "original work",
		Description: "Files: internal/core/service_starter.go:Start",
		Status:      types.StatusClosed,
		Type:        "implement",
	}
	o := setupFixAuditOverseer(t, orig, "test <scope> = internal/core/service_starter.go")

	ver := BuildFixVerifier(o)("fx-1", "test-project-abc")
	if ver.Status != "verified" {
		t.Errorf("status = %q (%s), want verified with clean scope", ver.Status, ver.Detail)
	}
}

// Caller 6: integration_retest.go — retest gates scope with the clean
// resolved path.
func TestBeadfilesAdoption_IntegrationRetest_CleanScopePasses(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Projects = []ProjectConfig{{
		Name: "proj",
		Path: t.TempDir(),
		Type: "single",
		Gates: &GateSet{
			DaemonOwned: true,
			Test:        GateSpec{Cmd: "test <scope> = internal/core/service_starter.go", Parser: "exit-code"},
		},
	}}
	s := NewIntegrationRetestStage(IntegrationRetestConfig{Enabled: true}, cfg, nil, nil, NewGateRunner(t.TempDir()), t.TempDir(), nil)
	bead := &types.Bead{ID: "proj-b1", Project: "proj",
		Metadata: map[string]string{"files": "internal/core/service_starter.go:Start"}}
	if err := s.runGates("proj", bead, t.TempDir(), t.TempDir()); err != nil {
		t.Fatalf("retest gates must pass with the clean resolved scope, got: %v", err)
	}
}

// Caller 7: gate_close.go — daemon close gates scope with the clean
// resolved path.
func TestBeadfilesAdoption_GateClose_CleanScopePasses(t *testing.T) {
	wt := t.TempDir()
	closer, _ := gateTestCloser(t, gateTestConfig(true, "test <scope> = internal/core/service_starter.go"), wt)
	bead := &types.Bead{ID: "proj-b2", Project: "proj", Status: types.StatusInProgress,
		Metadata: map[string]string{"files": "internal/core/service_starter.go:Start"}}
	if err := closer.runDaemonGates(bead, "proj", t.TempDir(), "", wt); err != nil {
		t.Fatalf("daemon gates must pass with the clean resolved scope, got: %v", err)
	}
}
