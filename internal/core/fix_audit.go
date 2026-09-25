package core

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/haulmatic-dev/hydra/internal/pan"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Closure-audit duty (operator 2026-09-25, bead jx78): Pan judges open
// auto-fix beads before a worker redoes finished work. The fix bead records
// the original closure's verdict in its description; Pan must NOT trust that
// verdict alone (jx78 recorded done:true while the suites failed at load,
// 0 tests ran) — the fix verifier cheaply re-runs the original bead's scoped
// test gate in the inherited worktree, and Pan's judgment weighs
// verdict-vs-evidence.

// autoFixLabel marks beads created by the bead closer for a failed closure.
const autoFixLabel = "auto-fix"

// BuildFixBeads returns the Pan provider enumerating open auto-fix beads
// across all projects. Fail-open: unreadable stores are skipped.
func BuildFixBeads(o *Overseer) pan.FixBeadsFunc {
	return func() []pan.FixBead {
		if o.beadsClient == nil {
			return nil
		}
		var out []pan.FixBead
		seen := make(map[string]bool)
		for _, project := range o.projects {
			for _, target := range repoScanTargets(project) {
				beads, err := o.beadsClient.ListBeads(target, "")
				if err != nil {
					continue // fail open: one unreadable store must not blind the sweep
				}
				for _, b := range beads {
					if b.Status.IsTerminal() || b.Type != "fix" {
						continue
					}
					if !hasBeadLabel(b.Labels, autoFixLabel) {
						continue
					}
					if seen[string(b.ID)] {
						continue
					}
					seen[string(b.ID)] = true
					fb := pan.FixBead{
						BeadID:          string(b.ID),
						OriginalID:      fixOriginalID(b),
						CloseReason:     fixCloseReason(b.Description),
						RecordedVerdict: recordedVerdictSummary(b.Description),
					}
					out = append(out, fb)
				}
			}
		}
		return out
	}
}

// fixOriginalID resolves the original bead a fix bead was created for: the
// first dependency when present, else the title after the "Fix: " prefix.
func fixOriginalID(b types.Bead) string {
	for _, dep := range b.Dependencies {
		if dep != "" {
			return string(dep)
		}
	}
	return strings.TrimPrefix(b.Title, fixTitlePrefix)
}

// closeReasonPattern extracts the "Close reason: ..." line the bead closer
// writes into every auto-fix description.
var closeReasonPattern = regexp.MustCompile(`Close reason:\s*([^\n]+)`)

func fixCloseReason(desc string) string {
	if m := closeReasonPattern.FindStringSubmatch(desc); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return "unknown"
}

// verdictPattern finds a HYDRA_VERDICT (or HYDRA verdict) block and captures
// a bounded tail so the judgment prompt sees the claim plus its evidence.
var verdictPattern = regexp.MustCompile(`(?i)HYDRA[\s_-]?VERDICT`)

// recordedVerdictSummary returns the HYDRA_VERDICT snippet recorded in a
// description, truncated for the judgment prompt. Empty when none — a missing
// verdict is itself evidence Pan must weigh.
func recordedVerdictSummary(desc string) string {
	loc := verdictPattern.FindStringIndex(desc)
	if loc == nil {
		return ""
	}
	snippet := strings.TrimSpace(desc[loc[0]:])
	if len(snippet) > 240 {
		snippet = snippet[:240] + "…"
	}
	return snippet
}

// hollowVerdict reports whether a recorded verdict claims done:true — the
// claim that re-verification must confirm before a fix bead is closed.
func hollowVerdict(verdict string) bool {
	return strings.Contains(verdict, `"done"`) && strings.Contains(verdict, "true")
}

// BuildFixVerifier returns the cheap re-verifier for fix-bead closure audits:
// it re-runs the ORIGINAL bead's scoped test gate in the original's inherited
// worktree (the same evidence the verdict claims to stand on). Only the test
// gate runs — cheap re-verification, not a full close gate; the satisfied
// gate re-proves full acceptance daemon-side before any close_satisfied lands.
// Any doubt returns Status "unknown" (fail open for the judgment, never for
// the close itself).
func BuildFixVerifier(o *Overseer) pan.FixVerifierFunc {
	return func(fixBeadID, originalID string) pan.FixVerification {
		id := types.BeadID(originalID)
		if originalID == "" {
			return pan.FixVerification{Status: "unknown", Detail: "no original bead recorded"}
		}
		bead, projectPath, _, err := resolveSatisfiedBead(o, id)
		if err != nil {
			return pan.FixVerification{Status: "unknown", Detail: fmt.Sprintf("original %s unresolved: %v", originalID, err)}
		}
		if o.beadCloser == nil || o.beadCloser.gateRunner == nil {
			return pan.FixVerification{Status: "unknown", Detail: "no gate runner attached"}
		}
		gates := gatesForProject(o.config, beadProject(bead))
		if gates == nil || !gates.DaemonOwned {
			return pan.FixVerification{Status: "unknown", Detail: "project has no daemon_owned gates"}
		}
		if gates.Test.Cmd == "" {
			return pan.FixVerification{Status: "unknown", Detail: "project has no test gate"}
		}
		targets := beadTargetFiles(bead)
		scope := ""
		if len(targets) > 0 {
			scope = targets[0]
		}
		// Run in the original bead's inherited worktree when one exists; the
		// fix worker would inherit the same branch state.
		worktreePath := projectPath
		if o.worktreeManager != nil {
			key := o.chainKeyForBead(*bead)
			if wt, ok := o.worktreeManager.ExistingWorktree(projectPath, key); ok {
				worktreePath = wt
			}
		}
		a, err := o.beadCloser.gateRunner.RunGate(GateRequest{
			Project:           beadProject(bead),
			BeadID:            string(id),
			Gate:              "fix-audit-reverify",
			Spec:              gates.Test,
			ProjectPath:       projectPath,
			WorktreePath:      worktreePath,
			Scope:             scope,
			AllowMainCheckout: true,
		})
		if err != nil {
			return pan.FixVerification{Status: "unknown", Detail: fmt.Sprintf("re-verify gate fault: %v", err)}
		}
		if a.Status != GateStatusPassed {
			return pan.FixVerification{Status: "contradicted", Detail: fmt.Sprintf("test gate red (exit %d): %s", a.ExitCode, truncateRunes(a.Summary, 200))}
		}
		log.Printf("[Pan] fix-audit re-verification passed for original %s (fix %s)", originalID, fixBeadID)
		return pan.FixVerification{Status: "verified", Detail: fmt.Sprintf("test gate green in %s", worktreePath)}
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
