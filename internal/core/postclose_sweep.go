package core

import (
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// PostCloseSweeper re-verifies recently closed beads by re-running their
// recorded verification command in the project's default-branch checkout.
//
// Contract (pinned by postclose_sweep_test.go, RED phase — behavior lands in
// the GREEN phase):
//   - Only beads with StatusClosed, metadata["test_command"] set, and
//     UpdatedAt within 24h are considered.
//   - Flake guard: a failing command is re-run once; a passing re-run means
//     flake → `postclose-flake` label, no reopen.
//   - Reopen path: both failing-run outputs are attached as a comment, the
//     bead is reopened (StatusOpen) and labeled `postclose-reopened`.
//   - Bounded auto-reopen: a bead already carrying `postclose-reopened` that
//     fails again gets `needs-human` added instead of a second reopen.
//
// This file currently provides only the type, constructor and lifecycle
// hooks so the pinned tests compile (the daemon's vet gate); scan() is
// intentionally inert until the GREEN phase implements the sweep behavior.
type PostCloseSweeper struct {
	beadsClient BeadsClient
	getProjects func() []types.Project
	events      *EventSink // nil-safe
	runCommand  func(projectPath, command string) (output string, err error)
	addComment  func(projectPath string, beadID types.BeadID, body string) error
	stopChan    chan struct{}
}

// NewPostCloseSweeper constructs the sweeper.
func NewPostCloseSweeper(
	beadsClient BeadsClient,
	getProjects func() []types.Project,
	events *EventSink,
	runCommand func(projectPath, command string) (output string, err error),
	addComment func(projectPath string, beadID types.BeadID, body string) error,
) *PostCloseSweeper {
	return &PostCloseSweeper{
		beadsClient: beadsClient,
		getProjects: getProjects,
		events:      events,
		runCommand:  runCommand,
		addComment:  addComment,
		stopChan:    make(chan struct{}),
	}
}

// Start launches the scan loop (GREEN phase wires the periodic cadence).
func (s *PostCloseSweeper) Start() {}

// Stop halts the scan loop.
func (s *PostCloseSweeper) Stop() {
	if s == nil {
		return
	}
	select {
	case <-s.stopChan:
	default:
		close(s.stopChan)
	}
}

// scan performs one sweep pass. Inert in the RED scaffold: the pinned tests
// exercise the behavior contract and fail until the GREEN phase implements
// the reopen / flake-guard logic here.
func (s *PostCloseSweeper) scan() {}
