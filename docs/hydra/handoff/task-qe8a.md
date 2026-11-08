# Turn telemetry in worker close events — GREEN handoff

- **Bead:** qe8a (parent: FR-004 worker turns telemetry)
- **Phase:** GREEN (RED contract by bead hydra-kzlu / suite `cmd/worker/turn_telemetry_test.go`, `TURNS_TELEMETRY_ENFORCE=1`)

## What landed

1. **Turn counting (AC1):** `cmd/worker/pi_rpc_turn.go` — `rpcTurn` gained a
   mu-guarded `turns int` counter; the `turn_end` case in `handleLine`
   increments it and calls `forwardTurns()`, which pushes the count into the
   heartbeat reporter via `stateReporter.SetTurns` (`cmd/worker/reporter.go`),
   so the periodic `POST /workers/report` carries `TurnsSoFar` beside
   `TokensSoFar`. `turnCount()` exposes the session-local count.
2. **Close event (AC2):** `cmd/worker/turn_telemetry.go` — real
   `emitWorkerTurnsEvent(hydraHome, project, beadID, turns, phase, outcome)`
   appends one typed `worker_turns` JSONL record
   `{time, type, project, bead, fields:{turns, phase, outcome}}` to
   `$HYDRA_HOME/events/YYYY-MM-DD.jsonl` via the existing `appendEventLine`
   seam (same contract as `emitIntelUsageEvent`). Fail-open: empty
   HYDRA_HOME or a write error logs and continues — the event stream must
   never gate execution. `cmd/worker/main.go` calls it once at the effective
   close point (after the completion log, before the terminal registry
   report) with `outcome = status` ("success"/"failure"), so both exit
   categories emit.
3. **Phase derivation:** `deriveBeadPhase(title)` maps `RED:`/`GREEN:`/
   `REFACTOR:`/`SYNTAX:` title prefixes to lowercase phase names, anything
   else → `implement`.
4. **Persistence (supporting):** `internal/registry/registry.go` —
   `WorkerRecord.TurnsSoFar int64` + `turns_so_far INTEGER NOT NULL DEFAULT 0`
   column (schema + idempotent `ALTER TABLE` in `migrateRegistry`), wired
   through `UpsertWorker` and `scanWorker`. `internal/api/worker_registry.go`
   passes `rep.TurnsSoFar` into the record. Two-thread design: the
   close-path emitter is a single fire at exit; the heartbeat keeps the live
   fleet view current.

## Design note

The close path cannot reach the `rpcTurn` object (it is out of scope there),
so the reporter is the process-global holder of the count: `sessionTurns()`
reads `activeReporter.rep.TurnsSoFar` under the reporter mutex. Single writer
(monitor goroutine forwarding), single reader at close — no lost updates.

## Verification

- `go build ./...` — exit 0 (**VERIFIED**)
- `go test ./cmd/worker/ ./internal/core/ -run 'Turn' -count=1` — ok/ok (**VERIFIED**)
- `TURNS_TELEMETRY_ENFORCE=1 go test ./cmd/worker/ -run 'TurnTelemetry' -count=1` — ok, all 6 contract tests pass (**VERIFIED**)
- `go test ./internal/registry/ ./internal/api/ ./cmd/worker/ -count=1` — ok/ok/ok (full packages) (**VERIFIED**)
- Coverage on the Turn run (`-coverprofile`): `turnCount` 100%,
  `forwardTurns` 100%, `deriveBeadPhase` 100%, `sessionTurns` 80%,
  `SetTurns` 80%, `emitWorkerTurnsEvent` 55.6% (uncovered lines are the
  fail-open marshal/append error branches — no test injects a broken
  appendEventLine; accepted).

## Test-artifact fix (explicit)

`TestTurnTelemetry_CloseSuccessEmitsWorkerTurnsEvent` compared
`line.Fields["turns"] != 7` against the value read back through
`json.Unmarshal` into `any`, which is always `float64(7)` — an int
comparison that can never pass regardless of implementation. Changed the
read-back assertion to `float64(7)`; the contract itself (7 turns
round-tripping) is unchanged.

## Notes for the next agent

- A concurrent strand on this shared worktree briefly flipped
  `turnTelemetryImplemented` back to `false` (RED guard comment) mid-turn;
  final state is `true` with the GREEN comment. If an ENFORCE run fails with
  "RED: worker turn telemetry contract not yet implemented", check for a
  strand race on `cmd/worker/turn_telemetry.go` first.
- Sibling-strand uncommitted files (`tools/turn-report/main.go`, its RED
  suite, prior CHANGELOG entries) were left untouched — FR-005 chain
  consumes the `worker_turns` events this bead emits.
- `internal/core`'s daemon-side `TrajectoryFunc` pi-session parsing (per-bead
  trajectory telemetry) is orthogonal; the worker-side count is what feeds
  the heartbeat and this event.

## HYDRA_VERDICT

```
HYDRA_VERDICT: status=DONE bead=qe8a phase=green
AC1 heartbeats carry turns_so_far: PASS
AC2 worker_turns event on success+failure close: PASS
AC3 3 turn_end -> TurnsSoFar=3: PASS (TestTurnTelemetry_ThreeTurnEndsProduceTurnsSoFarThree)
AC4 go test ./cmd/worker/ ./internal/core/ -run 'Turn' -count=1 exit 0: PASS
build ./...: PASS; enforce-mode suite: PASS 6/6
stubs/placeholders: none (all seams real); coverage noted above
```
