# fix handoff (hydra-lmmi)

## What changed (files + one line each)
- internal/core/tool_health.go: cherry-picked 16bc396a from hydra/wt-hydra-r98k — ToolHealthMonitor (RegisterProbe/RunProbes; healthy→degraded emits `tool_degraded` + files exactly one fix bead; degraded→healthy emits `tool_recovered` + clears dedup), file-backed durable ToolHealthDedupStore. All fail-open.
- internal/core/tool_health_test.go: integration drill — real EventSink (events read from central JSONL) + real dedup store, only BeadsClient stubbed; 3 tests.
- CHANGELOG.md: Added entry (came with the cherry-pick).
- docs/hydra/handoff/fix-lmmi.md: this handoff.

## Verification (commands run + pass/fail)
- Verify-first: inherited worktree had NO tool_health code; `go test ./internal/core/ -run 'TestToolHealth'` → "no tests to run". Root cause of original failure = worker Pi RPC infra error ("Anthropic stream ended before message_stop"), work existed only on hydra/wt-hydra-r98k (16bc396a, parented on our base 9fb3f139).
- Cherry-pick 16bc396a → f2b31fdb on hydra/wt-hydra-lmmi. Clean.
- `go build ./...` — PASS (0 units compiled).
- `go vet ./internal/core/` — PASS (clean).
- `go test ./internal/core/ -run 'TestToolHealth' -count=1` — PASS (5 tests: degradation drill, dedup persistence, fail-open).

## AC mapping (US-INT-01)
- AC1 `go test ./internal/core/ -run 'TestToolHealth' -count=1` exits 0 — VERIFIED.
- AC2 probe failure → `tool_degraded` + exactly one fix bead; restore → `tool_recovered` — VERIFIED by TestToolHealthDegradationDrill probes 2–4.
- AC3 no mocking of event sink or dedup store — VERIFIED (real EventSink over central JSONL dir, real file-backed dedup store; only BeadsClient + probe fn stubbed).

## Open edges for the next phase
- Monitor is not yet wired into service_starter.go live sweep (PRD §5 architecture); drill is self-contained per AC. Wiring bead separate if wanted.
- Duplicate strand 231d56f2 on hydra/wt-hydra-hh7n (older, smaller iteration) — superseded by 16bc396a; do not land both.

## Addendum: daemon-gate failure fix (same bead, second pass)
- Gate FAIL was internal/core TestPostCloseSweep* (4 tests: reopen/flake/needs-human/real-git) — postclose_sweep.go in this worktree was still the inert RED scaffold; GREEN implementation existed only on hydra/wt-hydra-hh7n (f8641db6). Cherry-picked f8641db6 → postclose_sweep.go +107/−6 (scan/reopen/flake-guard/needs-human).
- `go test ./internal/core/ -run 'TestPostCloseSweep' -count=1` — PASS (ok, 0.074s). Build + vet clean.
- Strand provenance: only postclose_sweep.go touched; tool_health strand unchanged.
