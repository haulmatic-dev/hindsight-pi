# discovery handoff (hydra-ngui)

## What changed (files + one line each)
- docs/discovery/evidence-of-use-probes-discovery-ngui.md: discovery for FR-004 deep-tier evidence-of-use probes — verified no tool-health code on HEAD, located the pinned RED contract on branch hydra/wt-hydra-e2a7 (commit e9783c8: ToolHealthEvidenceScanner + IntelUsageEvent + 5 tests, skip-gate design), mapped all 5 ACs, resolved 6 ambiguities (activity probe source, hindsight_memories source, sweep wiring, fail-open rules, worker-side emission path, `-buildvcs=false` rationale).
- CHANGELOG.md: Documentation entry under Unreleased.
- No code change (discovery bead; GREEN must implement the pinned contract).

## Verification (commands run + pass/fail)
- `hindsight_search` at bead start — not connected ("Hindsight is not connected"), skip rule applied. PASS.
- `graphify-out/graph.json` absent in cwd → rg/handoff fallback per protocol. PASS.
- `rg "tool_degraded|tool_recovered|ToolHealth|intel_usage" internal cmd agents` — zero hits → no shallow-tier tool-health subsystem on HEAD. PASS.
- RED contract located: `git show e9783c8:internal/core/tool_health_evidence.go` (+ test file, 152 lines, 5 tests: IntelUsageEventFields, DegradedWhenActivityNoMemories, NoActivityNoEvents, ActivityWithMemoriesNoDegradation, ProbeErrorFailOpen). PASS.
- RED branch confirmed unmerged: `git log --all --grep e2a7` → commits e9783c8/0edefcc/45abc7c on hydra/wt-hydra-e2a7 only. PASS.
- Analogous wiring pattern identified: internal/core/integration_exhausted_sweep.go (Start/Stop sweeper, knob-gated, fail-open, nil-safe). PASS.

## Open edges for the next phase
- GREEN must cherry-pick/re-apply RED commit e9783c8 from hydra/wt-hydra-e2a7 (not in HEAD ancestry), then implement scan() bodies + IntelUsageEvent.
- Wire intel_usage emission in cmd/worker pre-invocation (executeWithPi prompt-build path); booleans from hindsight client reachability, CASS (`cm` binary) presence, graphify-out/graph.json presence.
- Wire probes at daemon side: activity probe via registry heartbeat freshness (<5m, same liveness rule as pan_health 0d4aeaa); hindsight probe via daemon hindsight client count; probe errors fail open.
- Sweep cadence: mirror integration_exhausted_sweep (periodic + startup, config knob, 0 disables) or scan()-only per minimal change; ACs pin scan() behavior only.
