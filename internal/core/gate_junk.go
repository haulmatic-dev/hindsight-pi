package core

import "github.com/haulmatic-dev/hydra/pkg/types"

// FR-003 Close-time junk gate — RED-phase inert stub.
//
// Contract to be implemented by GREEN (docs/PRDs/clean-hands-prd-20261029.md
// FR-003): gateJunkArtifacts evaluates the close-time changed set (committed
// base...HEAD unioned with staged), auto-drops junk artifacts exactly once
// (staged-only junk unstaged without a commit; committed junk removed from
// the index with a single removal commit while leaving the file on disk),
// and — when junk still persists in the changed set after the drop pass —
// rejects the close with the distinct reason/label "junk-artifacts"
// (never overloaded onto the existing off-scope warn ladder, which stays
// warn-only). The ignore-injection from FR-001 makes re-staging impossible
// in new worktrees; the drop-once path covers legacy/staged files.
//
// This stub wires the gate into runCloseHardGates as one inert call line so
// the package compiles for the lint gate, with zero behavior: it always
// passes. RED tests are gated on gateJunkRequireImpl per the established
// GATE_DEPS_ENFORCE pattern; GATE_JUNK_ENFORCE=1 fails them instead, which
// is how RED provability is demonstrated for the missing contract.

const (
	// junkArtifactsReason is the intentionally distinct rejection reason and
	// durable label for junk artifacts that persist after the one-time drop.
	junkArtifactsReason = "junk-artifacts"
	// junkDropStartTime is the typed event emitted when the gate auto-drops
	// junk artifacts (staged and/or committed paths) and the close proceeds.
	junkDropEventType = "close_gate_junk_dropped"
	// maxJunkPathsInEvent caps how many junk paths a single drop/rejection
	// event carries verbatim.
	maxJunkPathsInEvent = 50
)

// gateJunkImplemented gates the RED-phase contract tests. GREEN must flip it
// to true alongside the real drop/reject implementation in this file.
const gateJunkImplemented = false

// gateJunkArtifacts is the FR-003 close-time junk gate. RED stub: inert
// fail-open, no drop, no rejection, no event.
func (c *BeadCloser) gateJunkArtifacts(bead *types.Bead, projectPath, repoName, worktreePath string) error {
	_, _, _, _ = bead, projectPath, repoName, worktreePath
	return nil
}
