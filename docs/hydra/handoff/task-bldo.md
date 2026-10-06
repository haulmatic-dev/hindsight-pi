# green handoff

## What changed (files + one line each)
- internal/core/bead_discovery.go: `filterByDAG` (single chokepoint all discovery paths — br ready, bv triage, bv plan — return through) now drops beads carrying `panFiledLabel` ("pan-filed", mirrors internal/pan.FiledLabel); filter runs before the dag==nil early return so the gate holds with no DAG loaded (same pattern as the escalated-label gate, kgo).
- internal/core/spawn_preflight.go: `spawnPreflightBead` blocks pan-filed-labeled beads at the spawn funnel with `SpawnSuppressedError` (no retry burned, re-checked next tick) + typed `spawn_blocked_human_triage` event — defense in depth behind the discovery gate.
- internal/core/overseer.go: `spawnWorker` store-truth refresh now also refreshes `bead.Labels` from `current.Labels` when present — a pan-filed label Pan adds while a stale in-memory copy sits queued must be seen by the preflight (in-memory labels alone silently missed it).
- internal/core/pan_filed_spawn_test.go (new): 5 tests — filterByDAG excludes pan-filed (and only pan-filed; mixed-label bead included), nil-DAG early return still filtered, spawn preflight suppression mentions pan-filed, end-to-end `spawnWorker` suppressed with store-truth label, non-pan-filed label ("needs-human") passes preflight.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS; `go build ./...` — PASS (BUILD_OK).
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l` on touched files — clean.
- `go test ./internal/core/ -run 'TestFilterByDAG_ExcludesPanFiled|TestFilterByDAG_NoDAG_PanFiledStillFiltered|TestSpawnPreflight_PanFiled|TestSpawnWorker_PanFiled|TestSpawnPreflight_NonPanFiledLabelAllowed' -count=1` — PASS (ok 0.087s; note the package's custom runner prints `Test Results:` lines but standard ok/FAIL status was verified).
- Iteration note: first run showed TestSpawnWorker_PanFiledBlocked failing (in-memory labels empty; preflight never saw store label) → label refresh added in spawnWorker → PASS. That refresh is load-bearing for AC2.

## Open edges for the next phase
- `filterByDAG` gate keys on the label only; if Pan ever files beads via a different label or type, the gate must be extended. Type "hold"-class coverage already exists.
- Beads already in_progress with pan-filed label (workers running now) are untouched by design — gate is spawn-time, not execution-time.
- docs/architecture/shepherd.md event table could gain a `spawn_blocked_human_triage` row (docs polish, not blocking).
