package core

import (
	"fmt"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestTargetsOverlap(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"identical", []string{"internal/core/a.go"}, []string{"internal/core/a.go"}, true},
		{"disjoint", []string{"internal/core/a.go"}, []string{"internal/core/b.go"}, false},
		{"empty candidate", nil, []string{"internal/core/a.go"}, false},
		{"empty other", []string{"internal/core/a.go"}, nil, false},
		{"symbol suffix stripped", []string{"internal/core/a.go:Do()"}, []string{"internal/core/a.go"}, true},
		{"symbol both sides", []string{"internal/core/a.go:Do()"}, []string{"internal/core/a.go:Undo()"}, true},
		{"multi-file one match", []string{"x.go", "shared/y.go"}, []string{"shared/y.go", "z.go"}, true},
		{"dirty paths cleaned", []string{"./internal/core/a.go"}, []string{"internal/core/a.go"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := targetsOverlap(tc.a, tc.b); got != tc.want {
				t.Errorf("targetsOverlap(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestTargetScopeConflict_NoDeclaredTargets(t *testing.T) {
	o := &Overseer{}
	bead := &types.Bead{ID: types.BeadID("proj-1")}
	if id, conflict := o.targetScopeConflict(bead); conflict {
		t.Errorf("bead without targets must never conflict, got holder %q", id)
	}
}

func TestTargetScopeConflict_MatchesOverlappingActiveBead(t *testing.T) {
	mock := newMockBeadsClient()
	mock.beads["proj-2"] = types.Bead{ID: types.BeadID("proj-2"), Metadata: map[string]string{"files": "internal/core/a.go"}}
	o := &Overseer{
		projects:    []types.Project{{Name: "proj", Path: "/tmp/proj"}},
		beadsClient: mock,
		activeWorkers: map[string]*types.Worker{
			"proj-2": {BeadID: "proj-2"},
		},
	}
	bead := &types.Bead{ID: types.BeadID("proj-1"), Metadata: map[string]string{"files": "internal/core/a.go"}}
	id, conflict := o.targetScopeConflict(bead)
	if !conflict || id != "proj-2" {
		t.Errorf("targetScopeConflict = (%q, %v), want (proj-2, true)", id, conflict)
	}
}

func TestTargetScopeConflict_FailOpenOnStoreReadError(t *testing.T) {
	mock := newMockBeadsClient()
	mock.getErr = fmt.Errorf("store down")
	o := &Overseer{
		projects:    []types.Project{{Name: "proj", Path: "/tmp/proj"}},
		beadsClient: mock,
		activeWorkers: map[string]*types.Worker{
			"proj-2": {BeadID: "proj-2"},
		},
	}
	bead := &types.Bead{ID: types.BeadID("proj-1"), Metadata: map[string]string{"files": "internal/core/a.go"}}
	if _, conflict := o.targetScopeConflict(bead); conflict {
		t.Error("store read failure must fail open (no conflict)")
	}
}
