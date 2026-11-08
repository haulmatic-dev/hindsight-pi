# syntax handoff (6vpk)

## What changed (files + one line each)
- internal/core/tool_health.go (new): `ToolHealthProbe`, `ToolHealthMonitor` (RegisterProbe/RunProbes; healthy→degraded emits `tool_degraded` + files one fix bead via BeadsClient, deduped; degraded→healthy emits `tool_recovered` and clears the dedup entry), `ToolHealthDedupStore` (durable file-backed JSON map tool→fix-bead ID, atomic write, missing file = empty). All fail-open; doc comments on every exported API.
- internal/core/tool_health_test.go (new): integration drill — real `EventSink` (events read back from the central JSONL day file) and real file-backed dedup store, NOT mocked; only the BeadsClient is a stub (probe errors injected via stubbed probe fn). 3 tests: `TestToolHealthDegradationDrill` (healthy→no events/no beads; first failure → exactly 1 `tool_degraded` + exactly 1 fix bead titled `tool-health: bv probe failing — investigate and repair` labeled `tool-degraded`; repeat failures → no duplicate; recovery → `tool_recovered` + dedup cleared; fresh episode → second bead), `TestToolHealthDedupStorePersistence` (entry survives a new store over the same path; Remove clears; missing file clean), `TestToolHealthFailOpen` (CreateBead failure does not lose the `tool_degraded` event).
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestToolHealth' -count=1` — PASS (ok).
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l internal/core/tool_health*.go` — clean.
- Placeholder grep (TODO/FIXME/Placeholder) over both files — none.
- graphify-out/graph.json absent in cwd → rg fallback per protocol (no tool_degraded/tool_recovered machinery existed; implemented fresh).
- hindsight_search at bead start — not connected (skip rule applied).

## Open edges for the next phase
- Monitor is not yet wired into the daemon service loop (no Start/Stop ticker registered in service_starter.go) — a wiring bead should construct it in NewOverseer with real probes (br/bv/gptcache) and a `$HYDRA_HOME/tool-health-dedup.json` store.
- `fileFixBead` records the dedup entry AFTER create: a crash in between files a duplicate bead on the next tick (deliberate: duplicate > silent loss).
