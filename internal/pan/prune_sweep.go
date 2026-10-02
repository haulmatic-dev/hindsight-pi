package pan

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

// PrunableWedgeSweep is the deterministic, zero-token fleet-automation path
// for the stale-worktree-registration wedge class. When a cohort of BLOCKED
// beads shares a prunable reason (worktree registration / spawn residue),
// ONE project-scoped repair:prune-worktree-registration clears every member
// at once — waiting for per-bead LLM judgment or running past the
// maxConsecutiveRepairs efficacy gate is exactly the "retry exhausted
// after N attempts" failure mode this sweep prevents (2026-09-21:
// srv-telematics and PROJECT-WEDGE cohorts needed a manual prune each).
//
// Contract (operator proposal 2026-09-30):
//   - batch mode: one repair invocation per cohort key, never one per bead;
//   - maintenance-window semantics: the sweep runs during the shepherd tick,
//     before LLM judgment, only when a cohort reaches the prunable threshold;
//   - explicit logging: successes and residual divergences (beads the sweep
//     could not clear or could not attribute) are both logged and returned.
//
// The sweep is fail-open: every error is logged and reported in the report,
// never propagated. It reuses handleRepair's safety gates (live-worker
// refusal, per-project rate limit, efficacy cap) rather than bypassing them.

// prunableSweepThreshold is the minimum cohort size before the sweep acts.
// Below it the wedge is left to normal judgment flow (one bead is not a
// shared wedge).
const prunableSweepThreshold = 2

// SweepReport records what the sweep did, for logging and tests.
type SweepReport struct {
	// Successes are cohort keys whose project-scoped prune ran clean.
	Successes []string
	// Cleared is the set of bead IDs covered by a successful cohort prune.
	Cleared []string
	// Residuals map a cohort key to an explicit divergence description:
	// why the cohort (or part of it) still needs attention after the sweep.
	Residuals map[string]string
}

// prunableWedgeKey reports whether a candidate's reason describes a
// worktree-registration wedge class the prune repair can clear, returning
// the cohort grouping key. Deliberately conservative: the prune repair is
// whitelisted for worktree/registration wedges only — human escalations and
// integration wedges must NOT be swept (wrong-tool history: 2026-09-14).
func prunableWedgeKey(reason string) (string, bool) {
	if !strings.HasPrefix(reason, "BLOCKED: ") {
		return "", false
	}
	lower := strings.ToLower(reason)
	isWorktree := strings.Contains(lower, "worktree") || strings.Contains(lower, "registration") || strings.Contains(lower, "spawn")
	if !isWorktree {
		return "", false
	}
	key, ok := wedgeKey(reason)
	if !ok {
		return "", false
	}
	return key, true
}

// PrunableWedgeSweep inspects the current candidate set, groups prunable
// worktree wedges by cohort key, and invokes the project-scoped
// prune-worktree-registration repair once per cohort when the cohort
// reaches prunableSweepThreshold. Successes and residual divergences are
// logged explicitly and returned.
func (s *Shepherd) PrunableWedgeSweep(cands []Candidate) SweepReport {
	report := SweepReport{Residuals: map[string]string{}}

	// Group prunable candidates by cohort key.
	cohorts := map[string][]string{} // key -> bead IDs
	for _, c := range cands {
		key, ok := prunableWedgeKey(c.Reason)
		if !ok {
			continue
		}
		cohorts[key] = append(cohorts[key], c.Record.BeadID)
	}
	if len(cohorts) == 0 {
		return report
	}

	keys := make([]string, 0, len(cohorts))
	for k := range cohorts {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic batch order

	for _, key := range keys {
		beads := cohorts[key]
		if len(beads) < prunableSweepThreshold {
			report.Residuals[key] = fmt.Sprintf("cohort of %d below sweep threshold %d; left to normal judgment", len(beads), prunableSweepThreshold)
			continue
		}
		// Cohort representative: any bead resolves the project scope for
		// project-scoped repairs (see SetProjectScope contract).
		representative := beads[0]
		// spawn-class cohorts get recover-spawn-cohort (fmsfe-npa 2026-10-02):
		// the prune alone never cleared the in-memory spawn-failure records,
		// so the cohort re-appeared in the next digest and Pan re-filed wedge
		// beads. The cohort verb also resets the members' retry counters.
		verb := "prune-worktree-registration"
		if strings.Contains(strings.ToLower(key), "spawn") {
			verb = "recover-spawn-cohort"
		}
		status := s.runRepair(representative, verb,
			fmt.Sprintf("PRUNE-SWEEP: batch prune for cohort %q covering %d beads", key, len(beads)))
		switch status {
		case "ok":
			report.Successes = append(report.Successes, key)
			report.Cleared = append(report.Cleared, beads...)
			log.Printf("[PruneSweep] cohort %q (%d beads): prune-worktree-registration succeeded for %s — cleared in batch", key, len(beads), representative)
		default:
			// Refused, rate-limited, or failed: keep the divergence explicit.
			report.Residuals[key] = fmt.Sprintf("repair status %q on representative %s (%d beads pending)", status, representative, len(beads))
			log.Printf("[PruneSweep] cohort %q (%d beads): residual divergence — repair status %q on %s", key, len(beads), status, representative)
		}
	}
	return report
}
