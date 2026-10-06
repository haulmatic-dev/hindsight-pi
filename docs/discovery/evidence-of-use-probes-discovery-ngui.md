# Discovery: Evidence-of-use probes (deep tier) — FR-004 (bead hydra-ngui)

Date: 2026-10-06. Discovery bead; no production code change.

## 1. Existing state (verified)

- `rg "tool_degraded|tool_recovered|ToolHealth|intel_usage"` across `internal/`, `cmd/`, `agents/` → **zero hits**. Neither the shallow tier (US-001 typed tool_degraded/tool_recovered events) nor any tool-health scanner has landed on this branch. The deep tier cannot build on an existing shallow scanner in-tree; it is the first tool-health subsystem to land here.
- The RED contract for THIS feature exists on branch `hydra/wt-hydra-e2a7` (commits e9783c8 + 0edefcc handoff, not merged into HEAD):
  - `internal/core/tool_health_evidence.go` — inert stub `ToolHealthEvidenceScanner` + `IntelUsageEvent`.
  - `internal/core/tool_health_evidence_test.go` — 5 behavior tests pinning the GREEN contract (see §3).
  - RED is a **skip-gate suite**: tests skip loudly unless behavior present, exit 0 pre-implementation; `TOOL_HEALTH_ENFORCE=1` makes the skips fail (proven RED gate pattern, same as PRDCONTRACT_ENFORCE).
- Existing analogous subsystem to mirror for wiring: `internal/core/integration_exhausted_sweep.go` (periodic sweeper with Start/Stop, knob-gated, nil-safe, fail-open) — the deep-tier scanner should follow that exact shape.
- Existing intel-emission seams:
  - Worker pre-invocation: `cmd/worker/main.go` `executeWithPi` / prompt-build path — `intel_usage` must be emitted there, before the agent invocation, with hindsight/CASS/graphify-fallback booleans known at that point.
  - `agents/context-processor.go` QueryHindsight* records degrade counters under "hindsight-recall" (internal/degrade) — the hindsight-used boolean can reuse the same call sites.
  - Graphify fallback: `graphify-out/graph.json` presence check already used by worker discovery prompts (`graphify query ... else rg fallback`) — `graphify_fallback = true` when graph.json missing.

## 2. Acceptance criteria → requirement mapping

| AC | Requirement | Resolution |
|---|---|---|
| 1 | Worker execution emits `intel_usage` with hindsight, cass, graphify-fallback fields before agent invocation | Event type `intel_usage`, `Fields{"hindsight":bool,"cass":bool,"graphify_fallback":bool}`; pinned by `TestToolHealthEvidence_IntelUsageEventFields` via pure fn `IntelUsageEvent(hindsight, cass, graphifyFallback bool) Event`. Emission site: worker pre-invocation (cmd/worker). |
| 2 | Sweep with worker activity but zero `hindsight_memories` → `tool_degraded` with `probe:"evidence"` | `ToolHealthEvidenceScanner.scan()`: per project, activity probe true + hindsight probe (0, nil) → one `tool_degraded` event, Fields["probe"]="evidence", Project set. |
| 3 | Sweep with zero worker activity → no evidence-tier events | activity false → zero events, regardless of hindsight count. |
| 4 | `go test ./internal/core/ -run 'TestToolHealthEvidence' -count=1` exits 0 | GREEN implements scanner; RED suite must pass (skip-gate design means RED exits 0 too — GREEN enforces via TOOL_HEALTH_ENFORCE=1 if needed). |
| 5 | `go build -buildvcs=false ./...` exits 0 | Standard gate; `-buildvcs=false` avoids the transient git VCS-status race seen in hydra-yz8z triage. |

## 3. GREEN contract (pinned by RED tests on hydra/wt-hydra-e2a7)

- `IntelUsageEvent(hindsight, cass, graphifyFallback bool) Event` — Type `intel_usage`, Fields carry all three booleans (presence-checked AND value-checked for hindsight).
- `NewToolHealthEvidenceScanner(getProjects func() []types.Project, events *EventSink) *ToolHealthEvidenceScanner`.
- `SetActivityProbe(fn func(project string) bool)` — "recorded worker activity" for a project.
- `SetHindsightProbe(fn func(project string) (int, error))` — hindsight_memories count for a project.
- `scan()` — evidence-tier sweep. Rules: activity && zero memories → 1 `tool_degraded` per project with `probe:"evidence"`; no activity → no events; activity with memories → no degradation; **probe errors fail open** (no event, no panic).

## 4. Ambiguities and resolutions

1. **data_models empty** (task metadata claims full definitions; array empty) — schemas inferred from RED tests (§3). GREEN must implement exactly the pinned API; no additional types.
2. **Where "worker activity" comes from** — unspecified. Options: registry rows (recent `working` state), or a probe closure injected at wiring time. Resolution: keep it injectable (`SetActivityProbe`); wiring decision deferred to GREEN — recommend registry heartbeat (<5m freshness, same liveness rule as pan_health orphan-claim digest, commit 0d4aeaa) to avoid re-introducing the heartbeat-fresh false-positive class.
3. **hindsight_memories source** — unspecified. Recommend a per-project Hindsight bank query or reusing the daemon-side hindsight client; probe-error must fail open per RED test.
4. **intel_usage emission is worker-side but scanner is daemon-side** — the event must be visible to the daemon's sweep. Events are repo-mirrored JSONL (`$HYDRA_HOME/events/`); worker-side emission via the existing event stream is sufficient.
5. **Sweep cadence/config knob** — RED tests do not pin a knob or Start/Stop. Recommend mirroring `integration_exhausted_sweep` (periodic + once at startup, config knob default e.g. 1h, 0 disables) but GREEN may land scan()-only if the ACs are read literally; the ACs only require scan() behavior. Minimal-change rule: implement the pinned contract; knob/wiring optional unless AC 2/3 demand a live sweep (they say "a sweep … emits", so wiring a periodic sweep is in scope for the feature to be observable in production).
6. **"before the agent invocation" timing** — intel_usage reflects the *intent/availability* at invocation time (hindsight client reachable, CASS binary present, graph.json present), not post-run usage counts. Confirmed by the boolean field types.

## 5. Risks / open edges for GREEN

- Fail-open everywhere: probe errors and nil EventSink must not panic (RED test only pins hindsight-probe errors; extend the discipline to activity probe).
- Do not double-fire tool_degraded per sweep for the same project (RED pins exactly 1 event per project per scan).
- If the shallow-tier tool_degraded emitter lands concurrently, keep the deep-tier event distinguishable via `probe:"evidence"` field (that is the contract's discriminator).
- Branch note: the RED commit e9783c8 lives on `hydra/wt-hydra-e2a7`, NOT on HEAD — GREEN must cherry-pick or re-apply it (same restore path as prior test-sq2n / fixture-restore precedent).
