package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

type fakeChainCompletionDetector struct {
	chains []registeredChain
}

type registeredChain struct {
	root types.BeadID
	ids  []types.BeadID
}

func (m *fakeChainCompletionDetector) RegisterChain(root types.BeadID, ids []types.BeadID, projectPath string) {
	m.chains = append(m.chains, registeredChain{root: root, ids: ids})
}

type fakeBeadFileReader struct {
	beads []types.Bead
}

func (r *fakeBeadFileReader) ReadFile(path string) ([]types.Bead, error) {
	return r.beads, nil
}

func TestChainRegistrarSkipsGateRoots(t *testing.T) {
	detector := &fakeChainCompletionDetector{}
	reader := &fakeBeadFileReader{
		beads: []types.Bead{
			{ID: "gate-1", Type: "approval-gate", Dependencies: nil},
			{ID: "epic-1", Type: "epic", Dependencies: []types.BeadID{"gate-1"}},
			{ID: "impl-1", Type: "implement", Dependencies: []types.BeadID{"epic-1"}},
			{ID: "test-1", Type: "test", Dependencies: []types.BeadID{"impl-1"}},
		},
	}

	registrar := NewChainRegistrar(detector, reader)
	registrar.RegisterFromProjects([]types.Project{{Name: "p", Path: "/tmp/p"}})

	if len(detector.chains) != 1 {
		t.Fatalf("expected 1 chain, got %d", len(detector.chains))
	}
	chain := detector.chains[0]
	if chain.root != "epic-1" {
		t.Errorf("chain root = %q, want epic-1", chain.root)
	}
	for _, id := range chain.ids {
		if id == "gate-1" {
			t.Error("gate should not appear in the registered chain")
		}
	}
}

func TestChainRegistrarGateRootWithNoWorkNoChain(t *testing.T) {
	detector := &fakeChainCompletionDetector{}
	reader := &fakeBeadFileReader{
		beads: []types.Bead{
			{ID: "gate-1", Type: "approval-gate", Dependencies: nil},
			{ID: "story-1", Type: "story", Dependencies: []types.BeadID{"gate-1"}},
			// story has no dependents -> no chain to register
		},
	}

	registrar := NewChainRegistrar(detector, reader)
	registrar.RegisterFromProjects([]types.Project{{Name: "p", Path: "/tmp/p"}})

	if len(detector.chains) != 0 {
		t.Fatalf("expected 0 chains when gate descendants have no dependents, got %d", len(detector.chains))
	}
}

func TestChainRegistrarNonGateRootStillWorks(t *testing.T) {
	detector := &fakeChainCompletionDetector{}
	reader := &fakeBeadFileReader{
		beads: []types.Bead{
			{ID: "epic-1", Type: "epic", Dependencies: nil},
			{ID: "impl-1", Type: "implement", Dependencies: []types.BeadID{"epic-1"}},
		},
	}

	registrar := NewChainRegistrar(detector, reader)
	registrar.RegisterFromProjects([]types.Project{{Name: "p", Path: "/tmp/p"}})

	if len(detector.chains) != 1 {
		t.Fatalf("expected 1 chain, got %d", len(detector.chains))
	}
	if detector.chains[0].root != "epic-1" {
		t.Errorf("chain root = %q, want epic-1", detector.chains[0].root)
	}
}
