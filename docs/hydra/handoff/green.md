# green handoff

## What changed (files + one line each)
- cmd/worker/main.go: moved execution-report write out of executeWithPi into runWorker (after Duration/Attempts final); added ExecutionResult.TurnCount set from rpc turn count; record now includes `turns`; readExecutionOutputFromFile parses duration/attempts/turns/tokens.
- cmd/worker/execution_report_test.go: new round-trip regression test proving attempts=3/turns=12/duration=7m/tokens survive write+read.
- CHANGELOG.md: Fixed entries for the telemetry bug and the HYDRA_VERDICT re-entry investigation.

## Verification (commands run + pass/fail)
- `go build ./...` — pass.
- `go vet ./cmd/worker/` — pass.
- `go test ./cmd/worker/ -run TestExecutionReportRoundTrip` — PASS.
- `go test ./cmd/worker/` — PASS (2.630s, full package).

## Open edges for the next phase
- HYDRA_VERDICT re-entry (bead gwce, done:true twice 2026-09-24): root cause is daemon-side integration failure, not worker telemetry. Log evidence: `[Integrate] FAILED ajantha-srv-telematics-gwce ... CONFLICT (modify/delete): test/jest-e2e.json deleted in HEAD and modified in hydra/wt-ajantha-srv-telematics-gwce` → close failed → bead re-executed → hard-parked `integration-exhausted`. Both runs parsed done:true with success=true. No code change made; a candidate fix lives in the integration-retest/reconciliation layer, not the worker.
- Tokens in reports come from the whole cumulative rpc session turn stats (can exceed per-run spend when the session resumes); per-attempt attribution is a possible follow-up.