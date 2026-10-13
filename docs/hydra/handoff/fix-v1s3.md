# fix handoff

## What changed (files + one line each)
- internal/core/dag_builder.go: Build edge loop now PRUNES edges whose target node is absent (`!dag.HasNode`) or terminal (closed/completed) instead of log-and-continue; `[DAG] Dependency not found` log path removed; build summary reports `pruned-missing` / `pruned-terminal`; `Sensor.HourlyCheck()` called per build.
- internal/core/missing_edge_sensor.go (new): `MissingEdgeSensor` — hourly window (roll on each build tick), per-edge typed `dag_missing_edge` event, hourly `dag_missing_edge_rate` summary (count>0 only), `dag_missing_edge_rate_alert` event + alert bead via pan filer when rate > `MissingEdgeAlertThreshold` (100/hr, 1h alert cooldown); nil-safe/fail-open.
- internal/core/overseer.go: `o.dagBuilder.Sensor = NewMissingEdgeSensor(o.eventSink, ...filePanBead)` wired at construction.
- internal/core/missing_edge_sensor_test.go (new): 5 tests — typed event, hourly rollup + threshold alert + cooldown + window reset, nil-safety, zero-window silent, DAG build regression (REFACTOR <- GREEN <- SYNTAX closed early + missing edge; GREEN not wedged by closed SYNTAX; REFACTOR dispatchable after GREEN closes; missing edge pruned, live edge kept).
- CHANGELOG.md, docs/architecture/shepherd.md: operator entry + 3 event-type rows.

## Verification (commands run + pass/fail)
- Reproduction (pre-fix): `rg "Dependency not found" internal/` — log-only path at internal/core/dag_builder.go:213, no pruning; closed blocker nodes held dependents (InDegree > 0 → not ready). Production: 23,201 events 2026-10-02.
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'TestSensor|TestDAGBuild_Prunes|TestDAGBuilder|TestDAG' -count=1` — PASS (0 FAIL lines, 36+5 tests incl. new).
- `gofmt -l` on all touched files — clean.
- Commit verified via `git show --stat HEAD` (6 files, +418/−3).

## Open edges for the next phase
- Ghost-node injection for completedBeads still adds closed ghost nodes; their edges are now pruned (harmless, but ghost nodes could eventually be dropped too).
- Alert bead filing goes through filePanBead (pan-filed label + per-project cap + title dedupe); rate alert dedupes by title within an hour — repeated distinct alert titles can file multiple beads.
- The 2026-10-02 flood came from out-of-order SYNTAX-before-GREEN closes in pattern-routing epics; pruning stops the wedge/spam, but epic close ordering itself is guarded separately (epic_close_guard).
