package core

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// fakeSweepBeads is a minimal BeadsClient for sweeper tests.
type fakeSweepBeads struct {
	BeadsClient
	beads []types.Bead
	added map[string][]string
}

func (f *fakeSweepBeads) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	return f.beads, nil
}

func (f *fakeSweepBeads) UpdateBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	f.added[beadID.String()] = labels
	return nil
}

func newFakeSweepBeads(beads []types.Bead) *fakeSweepBeads {
	return &fakeSweepBeads{beads: beads, added: map[string][]string{}}
}

func TestIntegrationSweepSurfacesStalePark(t *testing.T) {
	fake := newFakeSweepBeads([]types.Bead{{
		ID:        "demo-5ob8",
		Status:    types.StatusBlocked,
		Labels:    []string{integrationExhaustedLabel},
		UpdatedAt: time.Now().Add(-72 * time.Hour),
	}})
	s := NewIntegrationExhaustedSweeper(fake, ttlProjects, 48*time.Hour, nil)
	s.scan()

	if got := fake.added["demo-5ob8"]; len(got) != 1 || got[0] != needsHumanLabel {
		t.Fatalf("expected %q added, got %v", needsHumanLabel, got)
	}
}

func TestIntegrationSweepSkipsFreshInProgressTerminalAndFlagged(t *testing.T) {
	fake := newFakeSweepBeads([]types.Bead{
		{ID: "demo-fresh", Status: types.StatusOpen, Labels: []string{integrationExhaustedLabel}, UpdatedAt: time.Now().Add(-1 * time.Hour)},
		{ID: "demo-wip", Status: types.StatusInProgress, Labels: []string{integrationExhaustedLabel}, UpdatedAt: time.Now().Add(-72 * time.Hour)},
		{ID: "demo-closed", Status: types.StatusClosed, Labels: []string{integrationExhaustedLabel}, UpdatedAt: time.Now().Add(-72 * time.Hour)},
		{ID: "demo-flagged", Status: types.StatusOpen, Labels: []string{integrationExhaustedLabel, needsHumanLabel}, UpdatedAt: time.Now().Add(-72 * time.Hour)},
	})
	s := NewIntegrationExhaustedSweeper(fake, ttlProjects, 48*time.Hour, nil)
	s.scan()

	if len(fake.added) != 0 {
		t.Fatalf("expected no label writes, got %v", fake.added)
	}
}

func TestIntegrationSweepDisabledAtZero(t *testing.T) {
	fake := newFakeSweepBeads([]types.Bead{{
		ID: "demo-5ob8", Status: types.StatusOpen, Labels: []string{integrationExhaustedLabel},
		UpdatedAt: time.Now().Add(-720 * time.Hour),
	}})
	s := NewIntegrationExhaustedSweeper(fake, ttlProjects, 0, nil)
	s.scan() // manual call should still be inert-safe, but Start must no-op
	s.Start()
	s.Stop()
	if len(fake.added) != 0 {
		t.Fatalf("expected no label writes when disabled, got %v", fake.added)
	}
}
