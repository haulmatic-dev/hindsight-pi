package core

import (
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func setupAbandonOverseer(t *testing.T, bead types.Bead) (*Overseer, *mockBeadsClient) {
	t.Helper()

	projectPath := t.TempDir()
	cfg := DefaultConfig()
	cfg.Validation.CloseBlockedOnFailed = true
	cfg.Projects = []ProjectConfig{
		{Name: "test-project", Path: projectPath, Type: "single"},
	}

	mockBeads := newMockBeadsClient()
	mockBeads.addBead(bead)

	resolver := NewProjectResolver([]types.Project{{Name: "test-project", Path: projectPath}})
	closer := NewBeadCloser(mockBeads, nil, resolver, nil, cfg)

	o := &Overseer{
		config:      cfg,
		beadsClient: mockBeads,
		beadCloser:  closer,
		dag:         NewDAG(),
	}
	if err := o.dag.AddNode(bead); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	return o, mockBeads
}

func TestBuildAbandonWithSummaryClosesAsFailed(t *testing.T) {
	bead := types.Bead{
		ID:       "test-project-abc",
		Title:    "GREEN: stub feature",
		Project:  "test-project",
		Priority: 1,
		Status:   types.StatusInProgress,
		Type:     "task",
	}
	o, mockBeads := setupAbandonOverseer(t, bead)

	handler := BuildAbandonWithSummary(o)
	if err := handler("test-project-abc"); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	calls := mockBeads.getCloseCalls()
	if len(calls) != 1 {
		t.Fatalf("CloseBead calls = %d, want 1", len(calls))
	}
	if calls[0].reason != "failed" {
		t.Errorf("reason = %q, want failed", calls[0].reason)
	}

	status, err := o.dag.GetNodeStatus("test-project-abc")
	if err != nil {
		t.Fatalf("GetNodeStatus: %v", err)
	}
	if status != types.StatusFailed {
		t.Errorf("DAG status = %q, want failed", status)
	}

	if got := mockBeads.beads["test-project-abc"].Status; got != types.StatusFailed {
		t.Errorf("bead status = %q, want failed", got)
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
		t.Fatalf("expected a fix bead to be created")
	}
	if fixBead.Type != "fix" {
		t.Errorf("fix bead type = %q, want fix", fixBead.Type)
	}
}

func TestBuildAbandonWithSummaryTerminalNoOp(t *testing.T) {
	bead := types.Bead{
		ID:       "test-project-abc",
		Title:    "GREEN: stub feature",
		Project:  "test-project",
		Priority: 1,
		Status:   types.StatusClosed,
		Type:     "task",
	}
	o, mockBeads := setupAbandonOverseer(t, bead)

	handler := BuildAbandonWithSummary(o)
	if err := handler("test-project-abc"); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	calls := mockBeads.getCloseCalls()
	if len(calls) != 0 {
		t.Errorf("CloseBead calls = %d, want 0 for terminal bead", len(calls))
	}
}
