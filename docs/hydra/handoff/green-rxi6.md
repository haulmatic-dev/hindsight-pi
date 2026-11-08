# green handoff (hydra-ank6)

## What changed (files + one line each)
- internal/core/postclose_sweep.go: full GREEN implementation of the pinned contract — `scan()` sweeps `getProjects()` (list errors fail-open), `verifyBead()` selects `StatusClosed` beads with non-empty `metadata["test_command"]` and `UpdatedAt` within 24h; run→fail→guarded re-run (pass → `postclose-flake` label + `postclose_flake` event, no reopen/comment); repeat failure on a bead already carrying `postclose-reopened` → `needs-human` label + `postclose_needs_human` event, no second reopen/status write; first failure → comment carrying BOTH run outputs (error text used as evidence when a command produces no output, e.g. `false`) + `UpdateBeadStatus(open)` + `postclose-reopened` label + `postclose_reopened` event. `Start()` runs one immediate pass then hourly ticks on a `stopChan`; `Stop()`/nil-receiver nil-safe.
- CHANGELOG.md: Added entry under Unreleased.
- Label constant `needsHumanLabel` already exists in integration_exhausted_sweep.go — reused, no redeclaration.
- RE-RUN NOTE (2026-10-06): the 6dbv/xw4q/wwj2 integration merges resolved postclose_sweep.go back to the inert RED stub (dcc64b0 GREEN was NOT in HEAD ancestry; the merge picked the stub), failing 6 PostCloseSweep tests. Restored the landed GREEN implementation from the 44fe1eeb lineage (hydra/wt-hydra-n9nq strand; its postclose_sweep_test.go is byte-identical to HEAD's, sha 5b479bd6), committed as b5cf4f9e. The prior green-rxi6 handoff doc was also dropped by those merges; this file re-lands it.

## Verification (commands run + pass/fail)
- BEFORE (this pass): `go test ./internal/core/ -run TestPostCloseSweep -count=1` — FAIL 6/7 (`expected bead reopened to open, got status ""` ×4) — inert stub on HEAD after integration merges.
- `git show 44fe1eeb:internal/core/postclose_sweep_test.go` sha1 == HEAD test file sha1 — impl lineage compatible.
- AFTER: `go build ./internal/core/` — PASS; `go vet ./internal/core/` — clean; `gofmt -l internal/core/postclose_sweep.go` — clean.
- `go test ./internal/core/ -run TestPostCloseSweep -count=1` — PASS (6/6, ok 0.083s).
- Coverage (new implementation code, -coverprofile of the same contract): verifyBead 89.3%, scan 87.5%, runBlock 100%, NewPostCloseSweeper/Stop 100%, Start 90% — ~87% avg.
- Placeholder grep (// TODO/FIXME/Placeholder) — 0 matches.

## Open edges for the next phase
- Merge-lesson: integration merges that resolve a file to a different lineage's version must check ancestry — dcc64b0 was dropped silently. Re-integration should carry b5cf4f9e, not re-resolve the stub again.
- `addLabels` passes repoName "" (single-repo projects in tests); multi-repo projects would need `repoDirForBead` resolution if the sweep is ever pointed at them.
- Overseer wiring (overseer.postclose_sweep knob) lives on other strands' lineages via earlier integrate commits; not re-touched here.
