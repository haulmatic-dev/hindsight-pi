package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/internal/pan"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

func setupSatisfiedOverseer(t *testing.T, bead types.Bead, gateCmd string) (*Overseer, *mockBeadsClient) {
	t.Helper()

	projectPath := t.TempDir()
	cfg := DefaultConfig()
	cfg.Projects = []ProjectConfig{
		{
			Name: "test-project",
			Path: projectPath,
			Type: "single",
			Gates: &GateSet{
				DaemonOwned: true,
				Test:        GateSpec{Cmd: gateCmd, Parser: "exit-code"},
			},
		},
	}

	mockBeads := newMockBeadsClient()
	mockBeads.addBead(bead)

	closer := NewBeadCloser(mockBeads, nil, nil, nil, cfg)
	closer.SetGateRunner(NewGateRunner(t.TempDir()))
	closer.SetEventSink(NewEventSink(t.TempDir()))

	o := NewOverseer(cfg)
	o.beadsClient = mockBeads
	o.beadCloser = closer
	o.completed = make(map[string]bool)
	o.dag = NewDAG()
	if err := o.dag.AddNode(bead); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	return o, mockBeads
}

func TestBuildCloseSatisfiedGreenClosesBead(t *testing.T) {
	bead := types.Bead{
		ID:          "test-project-abc",
		Title:       "already done",
		Description: "Files: src/foo.test.js",
		Status:      types.StatusInProgress,
		Type:        "test",
	}
	o, mockBeads := setupSatisfiedOverseer(t, bead, "exit 0 <scope>")

	handler := BuildCloseSatisfied(o)
	if err := handler("test-project-abc"); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	calls := mockBeads.getCloseCalls()
	if len(calls) != 1 {
		t.Fatalf("CloseBead calls = %d, want 1", len(calls))
	}
	if calls[0].reason != "already-satisfied (Pan)" {
		t.Errorf("reason = %q, want already-satisfied (Pan)", calls[0].reason)
	}

	if got := mockBeads.beads["test-project-abc"].Labels; len(got) != 1 || got[0] != "closed-satisfied" {
		t.Errorf("labels = %v, want [closed-satisfied]", got)
	}

	if !o.completed["test-project-abc"] {
		t.Errorf("o.completed not set")
	}
}

func TestBuildCloseSatisfiedRedGateErrors(t *testing.T) {
	bead := types.Bead{
		ID:          "test-project-abc",
		Title:       "already done",
		Description: "Files: src/foo.test.js",
		Status:      types.StatusInProgress,
		Type:        "test",
	}
	o, mockBeads := setupSatisfiedOverseer(t, bead, "exit 1 <scope>")

	handler := BuildCloseSatisfied(o)
	if err := handler("test-project-abc"); err == nil {
		t.Fatal("expected error for red gate")
	}
	if len(mockBeads.getCloseCalls()) != 0 {
		t.Errorf("CloseBead called %d times on red gate", len(mockBeads.getCloseCalls()))
	}
}

func TestBuildCloseSatisfiedMissingScopedGatesErrors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Projects = []ProjectConfig{
		{
			Name: "test-project",
			Path: "/test/test-project",
			Type: "single",
			Gates: &GateSet{
				DaemonOwned: true,
				Test:        GateSpec{Cmd: "npx jest", Parser: "exit-code"}, // no <scope>
			},
		},
	}
	mockBeads := newMockBeadsClient()
	bead := types.Bead{
		ID:          "test-project-abc",
		Title:       "already done",
		Description: "Files: src/foo.test.js",
		Status:      types.StatusInProgress,
		Type:        "test",
	}
	mockBeads.addBead(bead)

	closer := NewBeadCloser(mockBeads, nil, nil, nil, cfg)
	closer.SetGateRunner(NewGateRunner(t.TempDir()))

	o := NewOverseer(cfg)
	o.beadsClient = mockBeads
	o.beadCloser = closer
	o.completed = make(map[string]bool)

	handler := BuildCloseSatisfied(o)
	if err := handler("test-project-abc"); err == nil {
		t.Fatal("expected error for missing scoped gates")
	}
}

func TestBuildCloseSatisfiedNotDaemonOwnedErrors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Projects = []ProjectConfig{
		{
			Name:  "test-project",
			Path:  "/test/test-project",
			Type:  "single",
			Gates: &GateSet{DaemonOwned: false},
		},
	}
	mockBeads := newMockBeadsClient()
	mockBeads.addBead(types.Bead{
		ID:          "test-project-abc",
		Title:       "already done",
		Description: "Files: src/foo.test.js",
		Status:      types.StatusInProgress,
		Type:        "test",
	})

	o := NewOverseer(cfg)
	o.beadsClient = mockBeads
	o.beadCloser = NewBeadCloser(mockBeads, nil, nil, nil, cfg)
	o.completed = make(map[string]bool)

	handler := BuildCloseSatisfied(o)
	if err := handler("test-project-abc"); err == nil {
		t.Fatal("expected error when not daemon_owned")
	}
}

func TestBuildCloseSatisfiedTerminalBeadSkipped(t *testing.T) {
	bead := types.Bead{
		ID:          "test-project-abc",
		Title:       "already done",
		Description: "Files: src/foo.test.js",
		Status:      types.StatusCompleted,
		Type:        "test",
	}
	o, mockBeads := setupSatisfiedOverseer(t, bead, "exit 0 <scope>")

	handler := BuildCloseSatisfied(o)
	if err := handler("test-project-abc"); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if len(mockBeads.getCloseCalls()) != 0 {
		t.Errorf("CloseBead called %d times for terminal bead", len(mockBeads.getCloseCalls()))
	}
}

func TestBuildCloseSatisfiedGetBeadFails(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Projects = []ProjectConfig{
		{
			Name: "test-project",
			Path: "/test/test-project",
			Type: "single",
			Gates: &GateSet{
				DaemonOwned: true,
				Test:        GateSpec{Cmd: "npx jest <scope>", Parser: "exit-code"},
			},
		},
	}
	mockBeads := newMockBeadsClient()
	mockBeads.getErr = fmt.Errorf("beadstore unreachable")

	o := NewOverseer(cfg)
	o.beadsClient = mockBeads
	o.completed = make(map[string]bool)

	handler := BuildCloseSatisfied(o)
	if err := handler("test-project-abc"); err == nil {
		t.Fatal("expected error when GetBead fails")
	}
}

func TestBuildCloseSatisfiedCoverageThresholdAbstains(t *testing.T) {
	bead := types.Bead{
		ID:          "test-project-abc",
		Title:       "coverage gate",
		Description: "Acceptance: functions coverage must be >= 80%\nFiles: src/foo.test.js",
		Status:      types.StatusInProgress,
		Type:        "test",
	}
	o, mockBeads := setupSatisfiedOverseer(t, bead, "exit 0 <scope>")

	handler := BuildCloseSatisfied(o)
	err := handler("test-project-abc")
	if !errors.Is(err, pan.ErrSatisfiedAbstain) {
		t.Fatalf("expected ErrSatisfiedAbstain, got: %v", err)
	}
	if len(mockBeads.getCloseCalls()) != 0 {
		t.Errorf("CloseBead called %d times on abstain", len(mockBeads.getCloseCalls()))
	}
}

func TestBuildCloseSatisfiedAcceptanceCommandGreenCloses(t *testing.T) {
	bead := types.Bead{
		ID:          "test-project-abc",
		Title:       "coverage gate",
		Description: "Acceptance: `sh -c 'exit 0' -- --collectCoverageFrom='src/foo' --coverageThreshold='{\"global\":{\"functions\":80}}'`\nFiles: src/foo.test.js",
		Status:      types.StatusInProgress,
		Type:        "test",
	}
	o, mockBeads := setupSatisfiedOverseer(t, bead, "exit 0 <scope>")

	// Satisfy the ratchet-artifact guard: coverageThreshold is present.
	packageJSON := filepath.Join(o.config.Projects[0].Path, "package.json")
	if err := os.WriteFile(packageJSON, []byte(`{"jest":{"coverageThreshold":{"global":{"functions":80}}}}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	handler := BuildCloseSatisfied(o)
	if err := handler("test-project-abc"); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	calls := mockBeads.getCloseCalls()
	if len(calls) != 1 {
		t.Fatalf("CloseBead calls = %d, want 1", len(calls))
	}
	if calls[0].reason != "already-satisfied (Pan)" {
		t.Errorf("reason = %q, want already-satisfied (Pan)", calls[0].reason)
	}
}

func TestBuildCloseSatisfiedMissingRatchetEntryForbidden(t *testing.T) {
	bead := types.Bead{
		ID:          "test-project-abc",
		Title:       "ratchet flip",
		Description: "Flip coverageThreshold for src/modules/evaluation-framework/** to 80/80/80/80\nFiles: src/evaluation-framework/foo.js",
		Status:      types.StatusInProgress,
		Type:        "test",
	}
	o, mockBeads := setupSatisfiedOverseer(t, bead, "exit 0 <scope>")

	// package.json exists but contains no coverageThreshold entry.
	packageJSON := filepath.Join(o.config.Projects[0].Path, "package.json")
	if err := os.WriteFile(packageJSON, []byte(`{"name":"test"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	handler := BuildCloseSatisfied(o)
	err := handler("test-project-abc")
	if err == nil {
		t.Fatal("expected error for missing ratchet entry")
	}
	if !strings.Contains(err.Error(), "coverageThreshold") {
		t.Errorf("error message missing coverageThreshold hint: %v", err)
	}
	if len(mockBeads.getCloseCalls()) != 0 {
		t.Errorf("CloseBead called %d times on forbidden close", len(mockBeads.getCloseCalls()))
	}
}

func TestBuildCloseSatisfiedFailedDAGNodeCompletesCleanly(t *testing.T) {
	bead := types.Bead{
		ID:          "test-project-abc",
		Title:       "already done",
		Description: "Files: src/foo.test.js",
		Status:      types.StatusInProgress,
		Type:        "test",
	}
	o, mockBeads := setupSatisfiedOverseer(t, bead, "exit 0 <scope>")

	// Simulate a DAG-level failure without the bead itself being terminal.
	if err := o.dag.UpdateNodeStatus("test-project-abc", types.StatusFailed); err != nil {
		t.Fatalf("UpdateNodeStatus: %v", err)
	}

	handler := BuildCloseSatisfied(o)
	if err := handler("test-project-abc"); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	calls := mockBeads.getCloseCalls()
	if len(calls) != 1 {
		t.Fatalf("CloseBead calls = %d, want 1", len(calls))
	}
}
