# green handoff (qx6km — close-time junk gate)

## What changed (files + one line each)
- internal/core/gate_junk.go: real FR-003 gate (`gateJunkImplemented=true`) — fail-open git probe (`junkGitLines`, `junkGitRun` sets cmd.Dir to worktree) classifies committed (`git ls-tree -r --name-only HEAD`) + staged junk via `junksig.IsJunkPath`; committed junk auto-dropped once (`git rm --cached -r` + `ensureGitignoreLine` for `junksig.IgnorePatterns()` + single "chore: auto-drop junk artifacts (close gate)" commit, files kept on disk); staged-only junk unstaged via `git reset -q --`, zero commits; typed `close_gate_junk_dropped` event names ≤50 paths verbatim (`capJunkPaths`); durable `junk-drop-N` label counter (`maxJunkDrops=2`) via shared `maxLabelCount`/`clearCounterLabels` helpers; third re-commit → `rejectJunkArtifacts` → `close_gate_rejected` reason/label exactly `junk-artifacts`, escalation ladder untouched.
- internal/core/bead_closer_hard_gates.go: `gateJunkArtifacts` wired into `runCloseHardGates` before the dependency gate.
- CHANGELOG.md: Added entry (FR-003 junk-gate GREEN) under Unreleased.
- docs/architecture/shepherd.md: event-table rows (`close_gate_junk_dropped`, `close_gate_rejected` junk-artifacts reason) + FR-003 junk-gate paragraph.
- internal/core/gate_junk_test.go (contract repairs, refactor strand): junk files written to disk before `git add`; re-adds use `--force`; drop baseline counted post-fixture-commit; `junkCommitCount` parses `rev-list --count`; AC3 test seeds bead in mock store so labels persist.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestGateJunk_' -count=1 -v` — PASS, exit 0: 6/6 `--- PASS` (CleanWorktreeFailOpenCoverage, CommittedJunkDroppedOnceCloseProceeds, StagedOnlyJunkUnstagedWithoutCommit, ReAddedAfterTwoDropsRejectsJunkArtifacts, ThreadCacheServiceFileNotTouched, NonJunkOffScopeWarnOnlyNoRejection); `ok ... 0.535s`; no `[no tests to run]`.
- `go test ./internal/core/ -run 'TestGateJunk_' -count=1` re-verified post-integration (HEAD 8c6fbfff) — ok 0.453s.
- `go build ./...` — PASS. `go vet ./...` — PASS. `gofmt -l` on gate_junk.go / bead_closer_hard_gates.go — clean.
- Coverage (TestGateJunk_ coverprofile): `gateJunkArtifacts` 90.9%, `dropJunkArtifacts` 78.9%, `rejectJunkArtifacts` 85.7%, `junkFromPaths` 85.7%, `junkGitLines` 76.9%, `junkGitRun` 66.7%, `capJunkPaths` 66.7%.

## Open edges for the next phase
- Git probe failures stay fail-open by design (a broken probe never blocks a healthy close); no test pins probe-error fail-open end-to-end — optional hardening.
- Junk removal commit resets nothing; counter is per-bead durable via labels. Removal of junk from the branch resets the budget only via `clearCounterLabels` on a clean gate pass.
