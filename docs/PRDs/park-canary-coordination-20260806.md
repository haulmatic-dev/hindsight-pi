# PRD: Park Canary — Coordinated Congestion Probing

**Date:** 2026-08-06
**Status:** proposed — not scheduled
**Origin:** operator design discussion 2026-08-06, after the srv-smart-engine
fleet run spent an afternoon oscillating between work and
`provider-capacity: concurrency limit` parks against a saturated shared
Kimi key.
**Related:** `docs/PRDs/worker-park-and-resume-20260731.md` (park mechanics,
registry, Pan), `internal/core/concurrency_governor.go` (AIMD),
`internal/core/scheduler.go` (congestion pause).

## Problem

When N workers are parked on provider-capacity, each probes independently on
its own exponential backoff (30s × 2^n, 5-minute ceiling, ±25% jitter):

1. **Uncoordinated probe load.** Four parked workers means up to four
   concurrent resume attempts against an account whose concurrency cap is
   the reason they parked. Each collision re-rejects everyone and re-parks
   them, lengthening the outage.
2. **Thundering-herd wake.** When capacity finally frees, every parked
   worker wakes inside the same backoff window, instantly re-saturating the
   account — all park again. The fleet oscillates instead of ramping.
3. **Detection latency.** Worst case ~5 minutes (backoff ceiling) between a
   capacity opening and the fleet noticing.

Park budget (60 min/worker) and session-preserving respawn bound the damage,
so this is an efficiency problem, not a correctness one.

## Requirement (operator)

> In an event of concurrency issues or provider overload, at least one
> worker keeps trying every 30–60 seconds. When it gets through, the others
> wake up gradually. No workers mass-sleeping for hours. **The canary must
> be an actual pending worker** — one of the parked workers doing its real
> bead work — not a synthetic pinger process created only to poll.

## Design

### Canary election (registry-backed, no new infrastructure)

- The **canary** is the parked worker with the oldest `park_since` in the
  registry (deterministic, every worker computes the same answer from the
  same table).
- Election is recomputed at every park wake. Ties broken by bead id.
- A canary that wakes successfully, dies, or gets abandoned yields to the
  next-oldest parked worker — leadership is a property of the registry
  state, not a lease to manage.

### Canary behavior

- Overrides its park backoff to **30–60s** (fixed window + jitter) instead
  of the exponential curve. It keeps doing exactly what park wake already
  does: re-prompt its own live pi session and continue its bead. The probe
  is the work.
- **Followers** (all other parked workers) extend their backoff to the
  5-minute ceiling immediately, skipping the short early steps — they rely
  on the canary for detection.

### Capacity-available signal and staggered wake

- The canary's first successful provider response after a park writes a
  `capacity_available_at` timestamp (new nullable column on a registry
  meta table, or a one-row `signals` table — smallest viable change).
- Followers check the signal at each wake:
  - No fresh signal → stay on the ceiling backoff.
  - Fresh signal (within e.g. 2 min) → wake now, staggered by
    `15s × hash(bead_id) mod N` so followers ramp one at a time instead of
    as a herd.
- The signal is advisory and expires quickly; a stale or absent signal
  never blocks a wake.

### Fail-open (non-negotiable, per doctrine)

Any failure in the coordination layer — registry unreachable, signal write
error, empty parked set — falls back to today's independent per-worker
backoff. Coordination must never gate a wake.

### What does not change

- Park triggers (`decideParkClass`), park cap (60 min), session resume,
  Pan's judgment role, the AIMD governor, the scheduler congestion pause.
- Pan remains the escalation path for workers that are parked *and* making
  no progress; the canary mechanism never overrides a Pan verdict.

## Acceptance criteria

1. With ≥2 workers parked, exactly one reports canary backoff (30–60s) and
   the rest report ceiling backoff (~5 min); leadership passes on
   wake/death/abandon.
2. After a canary success, followers wake staggered (observed spread, not
   one burst) — verified by park-wake timestamps in the registry/log.
3. With the registry unavailable, workers behave exactly as today
   (independent backoff) — proven by a test that severs the registry client.
4. No synthetic probe processes: grep shows no new binary/daemon; the
   canary is a row in `workers` with real bead work.
5. `go test ./...` green; new tests cover election, signal expiry,
   stagger computation, and the fail-open path.

## Phases

- **P1 — Canary election + backoff override** (worker-side, registry read).
- **P2 — Capacity signal + staggered follower wake** (registry schema +
  worker wake path).
- **P3 — Observability**: park-wake log lines carry `role=canary|follower`;
  Pan's context includes the canary identity.

## Open questions

- Signal storage: meta column vs one-row signals table (smallest diff wins).
- Whether the scheduler congestion pause should consult the canary signal
  instead of raw parked-count when deciding to resume spawns.
- Whether followers should count *their own* successful wake as a capacity
  signal for the next outage (signal symmetry).
