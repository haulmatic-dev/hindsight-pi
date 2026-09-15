# Lane Sync: llm-router capacity → Hydra parallelism (idea PRD)

Status: idea — approved for future build, not yet scheduled
Date: 2026-09-15
Depends on: llm-router `/capacity` endpoint (shipped 2026-09-15, PR #3), worker park-wake probe (hydra PR #232)

## Problem

Hydra's parallelism ceiling (`overseer.max_workers`, `provider_pool.size`) and
llm-router's lane pool (`lanes` per account) are two knobs that must stay in
parity by hand. The 2026-09-15 capacity expansion hit this twice in one
morning: the OpenRouter account was provisioned with 9 lanes against a
5-worker hydra (overshoot — wasted lanes invite 429 churn), then corrected to
4 (1 + 4 = 5). Every future account add, lane retune, budget-cap event, or
circuit cooldown reintroduces the mismatch.

Mismatch costs:

- `max_workers` > router lanes: spawn pile-up, provider-capacity parks,
  wake-denied-repark churn (partially mitigated by the PR #232 park probe,
  but prevention beats reaction).
- `max_workers` < router lanes: paid capacity sits idle; beads queue behind
  an artificial ceiling.
- Budget exclusion / circuit-open changes router capacity dynamically today,
  and nothing tells hydra — the fleet keeps its static ceiling until a human
  notices.

## Goals

| # | Goal |
|---|---|
| G1 | Hydra's effective spawn ceiling always equals `min(configured_max_workers, llm-router total lanes)` without human edits |
| G2 | Router-side capacity changes (config, account add/remove, budget soft/hard exclusion, circuit state) propagate to hydra within ~60s |
| G3 | No flap: capacity reads are cached and smoothed; a transient lane dip must not kill healthy workers |
| G4 | Fail-open: router unreachable → hydra keeps its configured ceiling; router data can only *lower* hydra below config, never raise it |

## Non-goals

| # | Non-goal | Why |
|---|---|---|
| NG1 | Syncing `free_lanes` to worker count | Lanes are held per request-turn, not per worker; free-lanes flaps every few seconds and would throttle streaming workers |
| NG2 | Router pushing events to hydra | Poll is sufficient at 30–60s latency; no subscription machinery |
| NG3 | Router raising hydra above its configured max | Operator config remains the hard upper bound |
| NG4 | Multi-router aggregation | One fleet router (localhost) for the foreseeable future |

## Design

### Router side: extend the existing `/capacity` endpoint

Do not add a new endpoint; enrich the one shipped in llm-router PR #3:

```json
GET /capacity → {
  "free_lanes": 2,
  "total_lanes": 5,
  "accounts": {
    "kimi-main":       { "lanes": 1, "in_flight": 1, "excluded": false },
    "openrouter-kimi": { "lanes": 4, "in_flight": 2, "excluded": false }
  }
}
```

- `total_lanes` = sum of configured lanes over enabled, non-excluded
  accounts (budget-hard-excluded and circuit-open accounts contribute 0 —
  this is what makes budget caps propagate)
- `accounts` breakdown is for debugging/dashboards, not for hydra's decision

### Hydra side: `effectiveSlots()` in the scheduler

Each scheduler tick computes:

```
effectiveSlots = min(configured max_workers, cachedRouterTotalLanes())
```

- **Source**: HTTP GET to `LLM_ROUTER_CAPACITY_URL` (env var already wired
  into workers by PR #232; the daemon reads it from the same knob or its own
  config field). Response cached ~30s.
- **Fail-open**: probe error, bad JSON, missing field, or URL unset →
  return a sentinel "no limit" so the configured ceiling applies.
- **Floor**: effectiveSlots >= 1 always (a zero-lane router answer must not
  freeze the fleet; the park probe already handles saturation).
- **Application sites**: the batch-spawn-limit calculation and the provider
  pool acquisition check. AIMD remains as the inner governor.

### Safety properties

| Property | Mechanism |
|---|---|
| Router cannot over-provision hydra | `min()` with configured max |
| Router outage cannot freeze the fleet | fail-open to configured max |
| Zero-lane answer cannot deadlock spawns | floor of 1 |
| Flap | 30s cache; only the ceiling reads the value, workers in flight are never reaped by a dip |

## Phasing

| Phase | Scope | Value alone |
|---|---|---|
| P1 | llm-router: `total_lanes` + per-account breakdown in `/capacity` | Debuggability; dashboards |
| P2 | hydra: `effectiveSlots()` = min(config, cached total) at batch-limit + pool sites | The parity property; zero manual sync |
| P3 | Dashboard/metrics: expose effective vs configured ceiling in `hydra status` or metrics | Observability of the sync itself |

## Success criteria

| # | Criterion | Target |
|---|---|---|
| SC1 | Change router lanes in config.json + restart → hydra ceiling follows within 60s, no daemon restart | Verified by test + live drill |
| SC2 | Router stopped → hydra continues at configured ceiling (fail-open) | Test |
| SC3 | Hard-budget-excluded account → hydra ceiling drops by that account's lanes | Test |
| SC4 | No spawn decisions read the router value more than once per 30s | Test |

## Open questions

| # | Question | Blocks |
|---|---|---|
| Q1 | Should provider_pool.size also track effectiveSlots, or stay at configured max? (Pool gates *acquisition*, not existence — keeping it at config max with the batch limit lower may be simpler) | P2 detail |
| Q2 | Pin TTL: 30s fixed, or back off when the router is unreachable (avoid hammering a down router every tick)? | P2 detail |
| Q3 | Should the same mechanism feed Pan's budget-arbitration (workers vs housekeeping split)? | Future |

## References

- llm-router PR #3 (`/capacity` endpoint)
- hydra PR #232 (park-wake capacity probe; wires `LLM_ROUTER_CAPACITY_URL`)
- 2026-09-15 capacity-expansion incident (9→4 lane correction) — the
  manual-parity failure this PRD eliminates
