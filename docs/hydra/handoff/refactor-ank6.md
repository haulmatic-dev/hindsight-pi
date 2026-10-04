# refactor handoff (refactor-ank6)

## What changed (files + one line each)
- internal/core/stranded_work.go: duplicated logic extracted into 6 named helpers — `worktreeKeyFromBranch` (prefix strip + versioned-key collapse), `beadStateFor` (GetBead lookup, unknown-owner semantics), `beadIsResumable` (non-terminal && no stranded-conflict/integration-unverified label, FR-021), `hasAnyLabel` (variadic over existing `hasLabel`), `emitStrandedEvent` (nil-sink fail-open seam, replaces 4 duplicated `if events != nil` guards), `labelBead` (fail-open UpdateBeadLabels with explicit repo argument; detector passes "" as before, repair passes `id.Repo()`). `chainActiveForKey` check folded into the activeKeys skip line. Net +112/−63 (helper doc comments).
- CHANGELOG.md: Unreleased/Changed entry.
- Behavior preserved: event payloads, label names, unknown-owner strandability, repo-name handling in label writes all byte-identical.

## Verification (commands run + pass/fail)
- BEFORE (pre-edit): `go test ./internal/core/ -run 'TestDetectStrandedWork|TestIntegrateStrandedRepair' -count=1` — ok (16 subtests).
- AFTER: same command — ok, 14 named tests all PASS (gofmt clean, `go build`/`go vet ./internal/core/` clean).
- Commit verified via `git show --stat HEAD` (7408fb3, 2 files).
- No benchmarks exist for this functionality; no perf check run.

## AC mapping
- Unit tests per evidence class + none-present path: pre-existing suite (SkipsLiveWorker/SkipsActiveChain/SkipsEmptyDiff/SkipsOpenBead/DetectsTerminalBead/NonePresent/DedupByLabel/VersionedKeyResolvesToBase + repair trio + FR-021 pair) — all pass. MET.
- Event payload includes quarantine branch: `stranded_conflict` Fields carries `quarantine_branch` (TestIntegrateStrandedRepair_Conflict). MET.
- Daily count queryable from event stream: `grep -h '"worktree_stranded"' $HYDRA_HOME/events/YYYY-MM-DD.jsonl | wc -l` (per-day: one file per day). MET (pre-existing, unchanged).

## Open edges for the next phase
- `branchAge`/`branchAheadCount` invoke one git subprocess each per branch — fine at current scale; batch via a single `for-each-ref --format` with %(committerdate) could halve calls if branch counts grow.
