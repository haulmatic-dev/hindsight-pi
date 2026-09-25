# green handoff

## What changed (files + one line each)
<<<<<<< HEAD
- internal/pan/pan.go: added FixBead/FixVerification/FixBeadsFunc/FixVerifierFunc types, Config.FixBeads, SetFixBeads/SetFixVerifier, 30-minute-cached fixVerification(), FIX-AUDIT candidates in Candidates() (suppressed on live worker rows and pseudo-beads), and closure-audit judgment rules in BuildPrompt (verdict+verified → close_satisfied; contradicted → continue + flag verdict hollow; unknown → keep fix).
- internal/core/fix_audit.go: BuildFixBeads (scans projects for open auto-fix/fix-type beads, extracts close reason + recorded HYDRA_VERDICT snippet) and BuildFixVerifier (re-runs the original bead's scoped test gate in the original's inherited worktree; verified/contradicted/unknown on doubt).
- internal/core/service_starter.go: wires SetFixBeads + SetFixVerifier into the Pan shepherd.
- internal/pan/fix_audit_test.go, internal/core/fix_audit_test.go: regression tests.
- CHANGELOG.md, docs/architecture/shepherd.md: operator-facing entries.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/pan/ ./internal/core/` — PASS.
- `go test ./internal/pan/ -count=1` — PASS (full package).
- `go test ./internal/core/ -run 'TestBuildFixVerifier|TestFixAuditExtraction|TestSatisfied|TestCloseSatisfied|TestBuildCloseSatisfied' -count=1` — PASS.

## Open edges for the next phase
- FixBeads scan lists every project's store every Pan tick (fail-open per store); if store counts grow, add a scan interval cache like shouldPresentPseudo.
- close_satisfied on a FIX-AUDIT candidate follows the existing fail-closed satisfied gate (ratchet guard, acceptance command, scoped gates) — no new bypass introduced.
- Recorded-verdict claim uses the description snippet only; hollow detection (`"done"` + `true` substring) is deliberately crude — the prompt instructs the judge to weigh it, not to trust it.
- reJudgeInterval (15m) re-presents unchanged candidate sets; combined with the 30m fix-verifier cache this bounds gate runs to ≤1 per 30m per fix bead.
=======
- internal/core/empty_close_gate.go: new accepted-empty-reason helpers — parse last HYDRA_VERDICT from newest worker log (fail-open), match docs-only/chain-deferred-to-root/superseded against diff evidence, accept or leave rejection unchanged.
- internal/core/bead_closer.go: verifyBeforeClose empty-diff branch consults tryAcceptEmptyClose before rejectClose; accepted close skips target-scope gate (docs-only changes are off-code-target by definition).
- internal/core/bead_closer_empty_gate_test.go: unit tests for reason matching, verdict extraction (last-wins), doc-path classification, and 3 integration tests through verifyBeforeClose.
- CHANGELOG.md: Added entry documenting the gate.

## Verification (commands run + pass/fail)
- `go build ./...` — pass.
- `go test ./internal/core/ -run 'TestAcceptedEmptyReason|TestExtractEmptyGateVerdictLastWins|TestVerifyBeforeClose_DocsOnly|TestVerifyBeforeClose_EmptyDiff|TestIsDocPath|TestAllDocsOnly'` — pass (narrow, then full internal/core package: 1060 passed).

## Open edges for the next phase
- Verdict parsing reads the newest `safeBeadID(beadID)-*.log` under $HYDRA_HOME/logs/workers; a bead whose log was rotated away silently keeps old reject behavior (fail-open by design).
- Duty 1 (verdict precedence over non-zero exit) and duty 3 (phantom triangle-check reconciler) remain unimplemented — separate beads.
- Docs-only acceptance trusts verdict tokens; an agent could self-report docs-only for code changes only if it also lies about changed files — evidence (changed file list) is still derived from git, not the verdict.
>>>>>>> origin/development
