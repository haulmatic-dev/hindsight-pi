package core

import (
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Unverifiable-scar exit (quarantine reconciler, RED phase — inert stub).
//
// GREEN obligations (pinned by internal/core/quarantine_unverifiable_test.go):
//
//  1. Only a no-command outcome (the gate reports the member declares no
//     verification command at all) increments the member's ran-false streak;
//     a runner fault (gate faulted executing a declared command) and a real
//     red run NEVER increment.
//  2. Three consecutive no-command outcomes stamp the member with
//     closed-failed-unverifiable (closed-failed itself stays — fail-closed,
//     the quarantine holds) and emit exactly one quarantine_unverifiable
//     event; the stamped member is thereafter skipped in the `who` selection
//     so other tainted members keep getting re-validated.
//  3. The streak map (RanFalseStreaks on QuarantinedChainEntry) round-trips
//     through the quarantined-chains.json registry file, so it survives a
//     daemon restart; a per-tick sweep (run from reconcileEntryLocked)
//     resets streaks for members no longer carrying closed-failed.
//  4. An operator removing the closed-failed-unverifiable label resets the
//     member's streak and re-enables re-verdicting for it (the `who`
//     selection no longer skips it).
//
// Wiring (GREEN, implemented): reconcileEntryLocked consults the classified
// gate when one is set (SetRevalidationClassified), feeds every outcome
// through noteRevalidateOutcome, runs sweepUnverifiableStreaks per tick, and
// routes `who` through revalidateTargetFor. The revalidationCooldown only
// bounds real reruns (Ran==true attempts); a no-command outcome executes no
// gate budget and is re-evaluated every tick (else the streak could never
// reach three).

// closedFailedUnverifiableLabel marks a member whose verdict cannot be
// overturned or re-proven by any bounded attempt: no verification command is
// declared, so three bounded re-checks (full ticks) produced no evidence.
const closedFailedUnverifiableLabel = "closed-failed-unverifiable"

// quarantineUnverifiableEventType — one operator-visible event per stamping.
const quarantineUnverifiableEventType = "quarantine_unverifiable"

// unverifiableStreakMax — bound: three consecutive no-command outcomes.
const unverifiableStreakMax = 3

// unverifiableImplemented flips to true in the GREEN phase; the contract
// tests in quarantine_unverifiable_test.go skip loudly until it is set
// (UNVERIFIABLE_ENFORCE=1 fails them instead — the established RED gate).
var unverifiableImplemented = true

// RevalidateVerdict classifies one quarantine-revalidation gate outcome.
//   - Passed=true, Ran=true: green rerun — scrub closed-failed (existing M1).
//   - Passed=false, Ran=true: real red run — verdict stands, no streak.
//   - Ran=false, NoCommand=false: runner fault (declared command existed but
//     execution faulted) — retryable, never streaked.
//   - Ran=false, NoCommand=true: the member declares no verification command
//     at all — the bounded-attempt counter feeds the unverifiable-scar exit.
type RevalidateVerdict struct {
	Passed    bool
	Ran       bool
	NoCommand bool
}

func requireUnverifiableImpl(t *testing.T) {
	t.Helper()
	if unverifiableImplemented {
		return
	}
	if os.Getenv("UNVERIFIABLE_ENFORCE") == "1" {
		t.Fatal("RED: unverifiable-scar exit contract not yet implemented (unverifiableImplemented=false)")
	}
	t.Skip("RED: unverifiable-scar exit contract not yet implemented (set UNVERIFIABLE_ENFORCE=1 to fail)")
}

// SetRevalidationClassified wires a run of the revalidation gate that also
// reports whether the member's verification command is missing at all
// (RevalidateVerdict). All label ops required; nil gate disables the
// classified path (the legacy (passed, ran) gate keeps working).

func (r *QuarantineReconciler) SetRevalidationClassified(gate func(bead *types.Bead, projectPath, worktreePath string) RevalidateVerdict, remove, add func(projectPath string, beadID types.BeadID, labels []string) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.classifiedGate = gate
	r.classifiedRemove = remove
	r.classifiedAdd = add
	if unverifiableImplemented {
		log.Printf("[QuarantineReconciler] classified revalidation wired")
	}
}

// noteRevalidateOutcome records one classified outcome against the member's
// ran-false streak on the entry. Returns true when this outcome triggered
// the unverifiable-scar stamping (third consecutive no-command outcome):
// the stamp adds closed-failed-unverifiable (never removes closed-failed —
// the quarantine holds), emits one quarantine_unverifiable event, and
// resets the member's streak.
func (r *QuarantineReconciler) noteRevalidateOutcome(e *QuarantinedChainEntry, beadID string, v RevalidateVerdict) bool {
	if r.memberScarPresent(e, beadID) {
		return false // already verdicted — re-stamp is a no-op, no event
	}
	if !v.NoCommand {
		// Runner fault (declared command existed, execution faulted) and real
		// red runs never increment: only the absence of any declared command is
		// proof the verdict can never be re-proven (AC1/discovery A1).
		if v.Ran && v.Passed && e.RanFalseStreaks != nil {
			delete(e.RanFalseStreaks, beadID) // green rerun scrubs the streak too
		}
		return false
	}
	if e.RanFalseStreaks == nil {
		e.RanFalseStreaks = make(map[string]int)
	}
	e.RanFalseStreaks[beadID]++
	if e.RanFalseStreaks[beadID] < unverifiableStreakMax {
		return false
	}
	// Stamp (discovery A3/A8): the label op must succeed or the attempt does
	// not count (fail-open — an infrastructure label fault must not decide the
	// bead's verifiability). closed-failed itself stays: fail-closed.
	if r.classifiedAdd != nil {
		if err := r.classifiedAdd(e.ProjectPath, types.BeadID(beadID), []string{closedFailedUnverifiableLabel}); err != nil {
			log.Printf("[QuarantineReconciler] %s: unverifiable stamp (add label) failed on %s — streak not advanced: %v", e.Key, beadID, err)
			e.RanFalseStreaks[beadID]--
			return false
		}
	}
	// Anchor the verdict to the recorded store state so the per-tick sweep can
	// tell an operator removal (store rewritten afterwards) from a not-yet
	// mirrored label.
	if e.UnverifiableAt == nil {
		e.UnverifiableAt = make(map[string]time.Time)
	}
	e.UnverifiableAt[beadID] = r.issuesModTime(e)
	e.RanFalseStreaks[beadID] = 0 // the label carries the verdict now; fresh reign if removed
	log.Printf("[QuarantineReconciler] %s: member %s unverifiable (3 consecutive no-command re-checks) — stamped %s; quarantine holds", e.Key, beadID, closedFailedUnverifiableLabel)
	if r.events != nil {
		r.events.EmitRepo(e.ProjectPath, Event{Type: quarantineUnverifiableEventType, Project: e.RepoName,
			Bead: beadID, Reason: "no_command_streak",
			Fields: map[string]any{"attempts": unverifiableStreakMax, "branch": e.Branch}})
	}
	return true
}

// memberScarPresent reports whether the member already carries the
// unverifiable verdict, either from an earlier stamp (ledger) or from the
// recorded store (an operator re-applied it or a previous daemon run left it).
// scarLabels reports whether the label set carries the closed-failed scar
// (the member is tainted) and/or the unverifiable verdict label (already
// stamped). Shared by the sweep, the `who` selection and the stamp anchor.
func scarLabels(labels []string) (hasFailed, hasScar bool) {
	for _, l := range labels {
		switch l {
		case closedFailedLabel:
			hasFailed = true
		case closedFailedUnverifiableLabel:
			hasScar = true
		}
	}
	return hasFailed, hasScar
}

func (r *QuarantineReconciler) memberScarPresent(e *QuarantinedChainEntry, beadID string) bool {
	if !e.UnverifiableAt[beadID].IsZero() {
		return true
	}
	if b := r.reader.FindByID(beadID); b != nil {
		_, hasScar := scarLabels(b.Labels)
		if hasScar {
			return true
		}
	}
	return false
}

// issuesModTime returns the store's modification time (zero when unknown) —
// the sweep's anchor for telling "scar removed by an operator" apart from
// "scar not yet mirrored into the recorded store".
func (r *QuarantineReconciler) issuesModTime(e *QuarantinedChainEntry) time.Time {
	if st, err := os.Stat(filepath.Join(e.ProjectPath, ".beads", "issues.jsonl")); err == nil {
		return st.ModTime()
	}
	return time.Time{}
}

// sweepUnverifiableStreaks is the per-tick reset sweep (run from
// reconcileEntryLocked): a member's streak is dropped when the member no
// longer carries closed-failed (a green revalidation scrubbed it) or no
// longer carries closed-failed-unverifiable (an operator removed the scar —
// re-verdict is permitted again). beadLabels must return the member's
// current labels.
func (r *QuarantineReconciler) sweepUnverifiableStreaks(e *QuarantinedChainEntry, beadLabels func(beadID string) []string) {
	if len(e.RanFalseStreaks) == 0 && len(e.UnverifiableAt) == 0 {
		return
	}
	storeMod := r.issuesModTime(e)
	seen := make(map[string]bool, len(e.MemberIDs))
	for _, id := range e.MemberIDs {
		seen[id] = true
		hasFailed, hasScar := scarLabels(beadLabels(id))
		if !hasFailed {
			// A green scrub (post-close sweep / M1) removed closed-failed — the
			// member's verdict is overturned and its bounded reign is over.
			delete(e.RanFalseStreaks, id)
			delete(e.UnverifiableAt, id)
			continue
		}
		if at, ok := e.UnverifiableAt[id]; ok && !hasScar && !storeMod.IsZero() && storeMod.After(at) {
			// The operator removed the scar (the store was rewritten after the
			// stamp and no longer carries the label) — clear the verdict so the
			// `who` selection re-verdicts the member.
			delete(e.UnverifiableAt, id)
			log.Printf("[QuarantineReconciler] %s: %s operator removed %s — re-verdict re-enabled", e.Key, id, closedFailedUnverifiableLabel)
		}
	}
	for id := range e.RanFalseStreaks {
		if !seen[id] {
			delete(e.RanFalseStreaks, id)
		}
	}
	for id := range e.UnverifiableAt {
		if !seen[id] {
			delete(e.UnverifiableAt, id)
		}
	}
}

// revalidateTargetFor picks the member to re-validate this tick: the first
// member carrying closed-failed that is NOT stamped closed-failed-unverifiable
// (stamped members are skipped in the `who` selection so their siblings keep
// getting re-validated). Returns "" when no re-validated-able member remains
// (stamped members' verdicts simply stand for the tick).
func revalidateTargetFor(reader *BeadFileReader, members []string, isUnverifiable func(beadID string) bool) string {
	for _, id := range members {
		bead := reader.FindByID(id)
		if bead == nil {
			continue
		}
		hasFailed, hasScar := scarLabels(bead.Labels)
		if !hasFailed || hasScar {
			continue
		}
		if isUnverifiable != nil && isUnverifiable(id) {
			continue
		}
		return id
	}
	return ""
}
