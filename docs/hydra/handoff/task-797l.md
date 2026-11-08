# red handoff

## What changed (files + one line each)
- cmd/worker/turn_telemetry_test.go (new): 6 RED contract tests pinning worker turn telemetry — AC1 `TestTurnTelemetry_ThreeTurnEndsProduceTurnsSoFarThree` (fake rpc session, 3 `turn_end` lines through `turn.handleLine` → `turnCount()==3` AND `rep.rep.TurnsSoFar==3` within one heartbeat interval, alongside `TokensSoFar>0`), `NoTurnEndsStayZero` (usage-free session must not fabricate turns); AC2 `CloseSuccessEmitsWorkerTurnsEvent` + `CloseFailureEmitsWorkerTurnsEvent` (typed `worker_turns` event `{bead, turns, phase, outcome}` read back from the central `$HYDRA_HOME/events` JSONL on BOTH exit kinds), `DeriveBeadPhase` (title-prefix phase mapping: RED/GREEN/REFACTOR/SYNTAX → phase, else `implement`), `CloseReferencesTurnsEmitter` (source-level: cmd/worker/main.go close path must call `emitWorkerTurnsEvent`).
- cmd/worker/turn_telemetry.go (new): RED inert stub — `turnTelemetryImplemented=false`, `workerTurnsEventType="worker_turns"`, inert `rpcTurn.turnCount` / `forwardTurns` / `deriveBeadPhase` / `sessionTurns` / `emitWorkerTurnsEvent`; doc comment names the GREEN obligations.
- internal/registry/types.go: `WorkerReport.TurnsSoFar int64` (`json:"turns_so_far"`) — compile surface for the heartbeat report (not persisted yet).
- CHANGELOG.md: Added entry under Unreleased.
- docs/hydra/handoff/red-kzlu.md + this task-797l.md: handoffs (shared-worktree landing merged both, see Provenance).

## Provenance
- This worktree (hydra-kzlu) landed the files via a shared-worktree merge commit e529071e ("test(core): RED contract suite for claim re-proof cache at respawn (hydra-kzlu BEAD)") swept by a concurrent strand that also carries the sibling bead_claims chain files — my 5 files are inside it, verified with `git show --stat e529071e`.
- A parallel strand on branch `hydra/wt-hydra-797l` (commit b13dd6b3, 296-line `turn_telemetry_test.go`) pins the SAME contract; integration must pick ONE — reconcile the two RED suites (assertion sets overlap: 3-turn_end → TurnsSoFar=3, worker_turns {bead,turns,phase,outcome}); mine additionally pins turnCount and the main.go emitter wiring.
- CHANGELOG entry shared with the tools/turn-report RED suite (lines 14–16, Unreleased/Added).

## Verification (commands run + pass/fail)
- `go test ./cmd/worker/ -run 'TurnTelemetry' -count=1` — PASS, exit 0 (all 6 SKIP loudly pre-GREEN: "RED: worker turn telemetry contract not yet implemented (set TURNS_TELEMETRY_ENFORCE=1 to enforce)").
- `TURNS_TELEMETRY_ENFORCE=1 go test ./cmd/worker/ -run 'TurnTelemetry' -count=1 -v` — FAIL 6/6 for the RIGHT reason. Exact failure: `turn_telemetry_test.go:81: turnCount = 0, want 3 (the rpc session must count its turn_end events)`.
- Coverage: `-coverprofile` on the gated run reports 0.0% on all five stub functions — inert stub pre-GREEN; GREEN must re-measure.
- `go build ./...` — PASS. `go vet ./cmd/worker/ ./internal/registry/` — PASS. `gofmt -l` on touched files — clean.
- AC gate `go test ./cmd/worker/ ./internal/core/ -run 'Turn' -count=1` — cmd/worker PASS (`ok ... 0.461s`); internal/core FAIL PRE-EXISTING (concurrent strand `internal/core/bead_claims_cache_test.go:153:40: expected '(', found proofsAndRefreshesCache` — sibling chain's WIP, not this bead's file; later commit e529071e may have landed its fixed form).
- Discovery: `cm context` (ctx_execute) — unrelated bullets only; `graphify query` ran; hindsight_search CLI unavailable (skip rule).
- Commit race note: two `git commit` attempts failed with `error: insufficient permission for adding an object to repository database`; the sweep commit e529071e carries the content.

## Open edges for the next phase (GREEN)
- Implement real turn counting in `rpcTurn.handleLine` (count every `turn_end`; `forwardTurns` → `stateReporter.SetTurns` alongside `SetTokenBreakdown`), `sessionTurns()` reads the active reporter for the close path, `emitWorkerTurnsEvent` via `appendEventLine`, wire into BOTH close sites (main.go success + auto-close-guard/`closeWithHold` paths) so failure exits emit too.
- The `tools/turn-report` chain consumes `worker_turns` events with `fields.phase` — keep the exact field name.
- Reconcile with the parallel 797l-branch RED suite before integration (see Provenance).
