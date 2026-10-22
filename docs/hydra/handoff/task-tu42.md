# GREEN handoff

## What changed (files + one line each)
- internal/core/pan_stale_filing.go: real sweep body live (`staleFilingSweepImplemented = true`) — deterministic probes (age > 48h + empty cohort + spawn success within 24h), in-process close ledger dedupe (one auto-close per project per tick).
- internal/core/overseer.go: wiring for spawn-success recording from `WorkerSpawner.Spawn` via `noteSpawnSuccessFiling` (throttle field + interval-gated sweep call at ~lines 132-134, 571, 977-978).
- internal/core/pan_stale_filing_test.go: 6 contract tests for the stale-filing sweep (in_progress closes first tick, open closes second tick, branch-protection never closed, cohort-still-blocked, no-recent-spawn, young-filing, rate-limit + typed `pan_filing_stale_closed` event shape).

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'StaleFiling|PanHealth' -count=1` — FAILED/not verified in this session. Persistent `spawn /bin/bash EAGAIN` resource exhaustion on the host runner plus anomalous wrapper output (`Test Results: PASS: 0 passed`) blocking clean go test invocation. Re-try when the host stabilizes.
- `go build ./...` — FAILED/not verified in this session for the same reason.
- Placeholder grep (`// TODO|FIXME|stub` in pan_stale_filing.go, pan_stale_filing_test.go) — NOT VERIFIED in this session (command execution repeatedly failing).
- gofmt/vet — NOT VERIFIED in this session.

## Open edges for the next phase (or re-run of this bead)
- Re-run the standard gate verification: `go test ./internal/core/ -run 'StaleFiling|PanHealth' -count=1` and `go build ./...` — must confirm exit 0 before this bead is considered closed.
- Coverage report for new code not captured this session (`go test ... -coverprofile=/tmp/cov.out` then `go tool cover -func=/tmp/cov.out`}.
- `hasCohortMember` (0.0% coverage) runs via the test-harness `staleFilingEligibleHooks` probe seams; the real store branch counts if a harness mirrors the store simulation.
- The `staleFilingLastSpawnOK` in-process ledger dedupe may need care if a RED harness injects a fresh store while overriding the probe to `return true`.
- Integration edge: `noteSpawnSuccessFiling` recording from `WorkerSpawner.Spawn` — confirm no leaks or duplicate recording during high-spawn scenarios.
