# GREEN handoff

## What changed (files + one line each)
- internal/core/pan_stale_filing.go: sweeps stale-filings with deterministic probes (age > 48h + empty cohort + recent spawn success) and in-process close ledger dedupe.
- internal/core/overseer.go: records spawn success taken from WorkerSpawner.Spawn via noteSpawnSuccessFiling.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'StaleFiling|PanHealth' -count=1` — PASS (6/6 PASS).
- `go build ./...` — PASS (exit 0).
- `gofmt -l` on `internal/core/pan_stale_filing.go` + `internal/core/overseer.go` — clean.
- `go vet ./internal/core/` — PASS (clean).
- Coverage (new implementation code, same guarded run):
  - sweepStalePanFilings 100.0%
  - sweepStalePanFilingsForProject 81.6%
  - isPanFiledBead 75.0%
  - hasCohortMember 0.0% (test-harness probe seams, deterministic RED body)
  - noteSpawnSuccessFiling / lastSpawnSuccessFor / filepathBaseForSpawnProbe 0.0%
  - wireStaleFilingProbes 0.0%

## Open edges for the next phase
- `hasCohortMember` (0.0% coverage) runs the closed-path tests via the test harness `staleFilingEligibleHooks` intercepts; the deterministic RED body is tail-cull and real store branch counts on next GREEN bead if the harness mirrors the store simulation.
- Integration edge: the `staleFilingLastSpawnOK` map avoids a redundant store read by relying on the in-process ledger; if a RED harness injects `mockBeadsClient` with a fresh store but overrides the probe to `return true`, the ledger dedupe (`staleFilingAlreadyClosed`) still blocks the second close tick. This matches the rate-limit contract.
- The RED fixture suite expects the in_progress filing to close on the first tick and the open filing on the second; the current in-process ledger ensures the first close is `proj-stale1` and the second `proj-stale2`.
