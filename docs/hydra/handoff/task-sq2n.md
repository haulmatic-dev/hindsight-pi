# red handoff (sq2n)

## What changed (files + one line each)
- internal/core/stranded_work_test.go: added `TestDetectStrandedWork_NonePresent` (clean repo, no hydra/wt-* branches → 0 units, 0 worktree_stranded/stranded_conflict events) and pinned the `quarantine_branch` field in the `stranded_conflict` event payload inside `TestIntegrateStrandedRepair_Conflict` (asserts `hydra/quarantine/<key>`).
- CHANGELOG.md: Unreleased/Fixed entry (tests + daily-count jq recipe).
- No production code changed: the Dangling-work (stranded-work) detector itself was already implemented and landed upstream — commits 817f479 (mechanical detector), 59ad2f8 (FR-021 blind spots), b3f8a1b (conflict manifest) are ancestors of origin/development and present in this branch. RED-against-empty-repo is therefore impossible; verify-first close per established precedent (test-6lmz/test-izp2/test-udlg handoffs).

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestDetectStrandedWork|TestIntegrateStrandedRepair' -count=1 -v` — PASS, 16 tests, 0 failures.
- Coverage (same run, -coverprofile): stranded_work.go DetectStrandedWork 89.1%, IntegrateStrandedRepair 85.3%, hasLabel 100%, splitNonEmpty 100%, writeConflictManifest 81.4% (new code).
- `gofmt -l internal/core/stranded_work_test.go` — clean.
- `go vet ./internal/core/` — clean.
- `go build ./internal/core/` — PASS (package compiles).

## AC mapping
- AC1 (unit tests per evidence class + none-present path): existing tests cover live-worker skip, active-chain skip, empty-diff skip, open-bead skip, terminal-bead detect, dedup-by-label, versioned-key resolution, quarantine-labeled open bead accepted, integration-unverified open bead accepted, repair success/conflict/verification-failure; `TestDetectStrandedWork_NonePresent` added this bead for the none-present path.
- AC2 (event payload includes quarantine branch name when present): event emits `Fields["quarantine_branch"]` (stranded_work.go conflict path); now pinned by test.
- AC3 (daily count queryable from event stream): events land in `$HYDRA_HOME/events/YYYY-MM-DD.jsonl`; count per day:
  `jq -s '[.[] | select(.type=="worktree_stranded")] | length' $HYDRA_HOME/events/$(date -u +%Y-%m-%d).jsonl`
  (same with `.type=="stranded_conflict"` for the conflict class).

## Open edges for the next phase
- chainActiveForKey/branchAheadCount/branchAge at 66–71% — remaining branches are error fallbacks (git failures), acceptable for RED scope.
- If the validation gate still flags "RED bead has no test files", it is bead metadata (target_files), not the suite.
