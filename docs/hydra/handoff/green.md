# green handoff

## What changed (files + one line each)
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