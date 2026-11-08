# syntax handoff (sw8l — Turn telemetry in worker close events)

## What changed (files + one line each)
- None in source (verification-only SYNTAX pass). Turn telemetry GREEN (emitWorkerTurnsEvent, forwardTurns, SetTurns heartbeat, main.go close wiring both outcomes) and the refactor (97cef02e "refactor: optimize Turn telemetry in worker close events") are committed; tree clean.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS (BUILD_OK).
- `go vet ./cmd/worker/ ./internal/core/` — PASS (clean, RC=0).
- `go test ./cmd/worker/ ./internal/core/ -run 'Turn' -count=1` — PASS (ok cmd/worker 0.465s, ok internal/core 0.108s).
- `go test ./cmd/worker/ -run 'TurnTelemetry' -count=1 -v` — PASS 6/6 (ThreeTurnEndsProduceTurnsSoFarThree, NoTurnEndsStayZero, CloseSuccessEmitsWorkerTurnsEvent, CloseFailureEmitsWorkerTurnsEvent, DeriveBeadPhase, CloseReferencesTurnsEmitter).
- `gofmt -l` on turn_telemetry.go / turn_telemetry_test.go / pi_rpc_turn.go / main.go / reporter.go / types.go — clean.
- Placeholder grep (TODO/FIXME/Placeholder) over turn_telemetry.go + test — 0 hits; files non-empty (110 / 228 lines).
- Doc comments verified on incrementTurns / turnCount / forwardTurns / deriveBeadPhase / emitWorkerTurnsEvent (16 comment lines over declaration block); main.go:566 call site wired with outcome = exit status.

## Open edges for the next phase
- None for this chain. tools/turn-report GREEN (task-249m) is a sibling bead, already landed and green.
