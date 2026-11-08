# REFACTOR handoff

- bead: u6cr (phase `refactor`, parent_phase `tdd`)
- scope: "REFACTOR: Optimize Infra classification at the executable-acceptance gate"
- behavior: UNCHANGED (naming + duplication only)

## What changed (files + one line each)

- `internal/core/gate_infra_class.go` — the infra helper trio (`infraRejectCount`, `infraLabels`, `clearInfraRejectLabels`) replaced by the shared, prefix-parameterized helpers `maxLabelCount(labels, prefix)`, `(c) currentLabels(bead, projectPath, repoName)`, `(c) clearCounterLabels(bead, projectPath, repoName, prefix)`; `rejectInfraGate` call sites updated, all consts/events/error strings untouched.
- `internal/core/bead_closer_hard_gates.go` — the byte-identical dep-open trio (`depOpenRequeueCount`, `depOpenLabels`, `clearDepOpenRequeueLabels`) deleted in favor of the same three shared helpers (prefix `depOpenRequeueLabelPrefix`); `gateExecutableAcceptance`, `gateDependencyOrder` (x2) and `requeueDepOpen` call sites updated; now-unused `"strconv"` import dropped.
- `CHANGELOG.md` — entry under `[Unreleased] / Changed`.
- `docs/hydra/handoff/task-u6cr.md` — this handoff.

Net: −~70 duplicated lines, one counter-label implementation instead of three copies (infra count, dep-open count, shared clear).

## Verification (commands run + pass/fail)

BEFORE (pre-edit baseline, same session):

- `go test ./internal/core/ -run 'TestGateInfra_' -count=1 -v` → **PASS** ("Test Results: PASS: 7 passed" — table test's 7 cases + reopen/escalate/success-clear/project-failure suites).

AFTER (post-edit):

- `rg 'infraRejectCount|infraLabels|clearInfraRejectLabels|depOpenRequeueCount|depOpenLabels|clearDepOpenRequeueLabels' internal/core/` → **0 matches** (exit 1, no stragglers).
- `gofmt -l internal/core/` → empty output, exit 0 (clean).
- `go vet ./internal/core/` → no output, exit 0 (clean).
- `go test ./internal/core/ -run 'TestGateInfra_' -count=1 -v | tail -30` → **PASS**: ClassificationTable, CommandNotFoundReopensWithoutStrike, FourthConsecutiveInfraRejectEscalates, SuccessClearsInfraCounter, ProjectFailureStaysVerificationFailed — all `--- PASS`, `ok github.com/haulmatic-dev/hydra/internal/core`.
- `go test ./internal/core/ -run 'TestCloseGate|TestQuarantineClose|TestCloseDep|TestVerifiedClose_HardGate|TestDepOpen_' -count=1` → **ok** (regression set over the close-gate suites touched by the shared helpers).

Benchmarks: only `BenchmarkScopeViolations`/`BenchmarkTargetlessWarningPaths` exist in the package; no close-gate or infra-classification benchmarks → no benchmark regression check possible/needed.

## Provenance note

The refactored content in both Go files is **byte-identical** (matching git blob
hashes `dfc86ea2`/`412798c2`) to what commit 64b07397 (a sibling bead's feat
commit, 15:25) already carried: this session's worktree still held the
pre-refactor helpers, and re-applying the dedup converged to exactly the same
result. Commit 425aa1bb therefore lands the paper trail only (changelog entry +
this handoff); the code refactor itself is already in the tree. Tests were re-run
after convergence to confirm the on-disk state is green.

## Open edges for the next phase

- `rejectInfraGate` and `requeueDepOpen` still mirror each other deliberately (reopen-without-strike + durable counter + escalate-at-N). NOT unified: their event payloads differ (`max_rejections` vs `max_requeues` fields, `infraEscalationReason` in the infra escalation label list vs bare `escalated` set) and error-message wording is pinned by tests. Unifying them is a behavior-risk change, not a pure refactor — left out on purpose.
- Full-suite verification (`go test ./...`) is daemon-owned; this handoff covers the narrow + close-gate regression sets only.
