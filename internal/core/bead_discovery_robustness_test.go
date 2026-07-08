package core

import (
	"errors"
	"sync"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestFindFromBR_SkipsFailingProject verifies per-project isolation: one
// project whose bead listing fails (e.g. a parent dir with no .beads, or a
// transient br error) must not abort discovery for every other project.
// Previously the first failure returned an error and the whole tick aborted,
// so a single misconfigured project starved all others of scheduling.
func TestFindFromBR_SkipsFailingProject(t *testing.T) {
	mock := newMockBeadsClient()
	mock.listErrByPath = map[string]error{
		"/projects/bad": errors.New("br list --json failed: exit status 2"),
	}
	mock.beads["good-1"] = types.Bead{
		ID:      "good-1",
		Project: "good",
		Type:    "task",
		Status:  types.StatusOpen,
	}

	projects := []types.Project{
		{Name: "bad", Path: "/projects/bad"},
		{Name: "good", Path: "/projects/good"},
	}
	var mu sync.RWMutex
	d := NewBeadDiscovery(mock, nil, projects, map[string]bool{}, nil, &mu)

	beads, err := d.findFromBR(map[string]bool{})
	if err != nil {
		t.Fatalf("expected no error when one project fails (it should be skipped), got: %v", err)
	}
	if len(beads) != 1 || beads[0].ID != "good-1" {
		t.Fatalf("expected the healthy project's bead, got %d beads: %+v", len(beads), beads)
	}
}
