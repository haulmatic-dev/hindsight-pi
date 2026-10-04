# green handoff

## What changed (files + one line each)
- internal/core/stranded_work_test.go: cherry-picked RED commit de4ee5e (test-sq2n strand) — adds TestDetectStrandedWork_NonePresent (none-present path) + TestIntegrateStrandedRepair_Conflict pins non-empty `quarantine_branch` field on the stranded_conflict event payload.
- docs/hydra/handoff/task-sq2n.md: restored (part of cherry-pick).
- No implementation change needed: internal/core/stranded_work.go already satisfies the RED contract on this branch (DetectorDetects per evidence class + quarantine_branch in event fields at line 274).

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestDetectStrandedWork|TestIntegrateStrandedRepair' -count=1` — PASS (ok 1.433s, 14 tests incl. NonePresent).
- Coverage (same run, -coverprofile): DetectStrandedWork 89.1%, IntegrateStrandedRepair 85.3%, writeConflictManifest 81.4%, splitNonEmpty 100%, hasLabel 100%, chainActiveForKey 66.7%, branchAheadCount 71.4%, branchAge 71.4%, removeStrandedWorkLabel 66.7%.
- Event payload criterion: test asserts `fields["quarantine_branch"] == "hydra/quarantine/"+key` (stranded_work_test.go:271-276). PASS.

## Daily count query (AC: countable from event stream)
```bash
# events dir: $HYDRA_HOME/events/YYYY-MM-DD.jsonl — count stranded-work detections per day
grep -h '"worktree_stranded"' /opt/dev-tools/var/hydra/events/2026-*.jsonl | wc -l
# per-day breakdown:
grep -c '"worktree_stranded"' /opt/dev-tools/var/hydra/events/*.jsonl
# conflict-class only (quarantine events, jq):
jq -r 'select(.type=="stranded_conflict") | .fields.quarantine_branch' \
  /opt/dev-tools/var/hydra/events/2026-*.jsonl | sort | uniq -c
```

## Open edges for the next phase
- None for this chain. DetectStrandedWork 89.1% leaves error-fallback branches (git listing failures) uncovered — acceptable fail-open paths.
