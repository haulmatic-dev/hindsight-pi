# PRD: Shared Provider Concurrency Pool (Shepherd B3)

## 1. Overview

**Problem:** The provider plan allows 4 concurrent requests. Hydra has at least four independent consumers of that budget — worker spawns (`agents.pi`), TDD/PRD validation, agent verification, and the gap adjudicator on `k3-256k` with `adjudicator_thinking: max` — and none of them knows the others exist. Each fires whenever it wants, the provider refuses the overflow, and the refusals surface as failures.

Measured on 2026-07-30: with `max_workers: 4`, 16 requests were refused in 45 minutes and the AIMD governor walked the worker limit down 4 → 2 → 1 chasing a ceiling it could not see the cause of. At one worker the refusals stopped and throughput collapsed to roughly zero — one bead closed in 45 minutes. The fleet was not competing with other tenants; it was competing with hydra's own judgment calls. A max-thinking adjudication holds a slot for minutes, and nothing in the system accounted for it.

Capping `max_workers` cannot fix this. It gates one consumer against a budget shared by four, so it either under-uses the plan (workers idle while no adjudication is running) or over-subscribes it (workers refused because one is).

**Solution:** A single in-daemon pool sized to the provider's concurrency budget. Every consumer acquires a slot before calling the provider and releases it after. When the pool is full, callers **queue** rather than fire and fail. Fairness is FIFO — priority deliberately stays out of the pool, because `bv` and the DAG already decide which work matters, and a second priority scheme here would silently reorder those decisions.

**Success Metrics:**
- Provider refusals attributable to our own concurrency (exit 7 with a concurrency marker) drop to zero at `max_workers` equal to the pool size.
- With the pool enabled, an adjudication in flight delays a worker spawn instead of causing a refused request.
- Total slot utilisation is observable: queue depth and wait time are emitted, so saturation is a measurement rather than an inference.

---

## 0. Scope Profile
- **System Type:** library (internal Go, daemon-side)
- **Interfaces:** internal Go API only
- **Surfaces Explicitly Excluded:** the AIMD governor (unchanged — see §6 on how they compose), the provider circuit breaker, the pathology brake (B2), per-request gating *inside* worker processes (see §5), bead prioritisation
- **Deployment Model:** single-binary daemon

---

## 2. User Stories

### US-001: Every provider consumer shares one budget — Integration Bead
**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Provider Pool
- FR-002: Consumer Integration
- FR-003: Saturation Observability

**Acceptance Criteria:**
- `go build ./...` exits 0
- `go test ./internal/core/...` exits 0
- An integration test drives the real pool with a size of 2, starts three concurrent acquirers, and asserts the third blocks until one releases
- With the pool disabled (config absent), a test asserts scheduling and adjudication behaviour is identical to today
- No mocking of internal dependencies: the test drives the real pool and the real slot arithmetic

---

## 3. Functional Requirements

### FR-001: Provider Pool
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/provider_pool.go`, `internal/core/provider_pool_test.go`

Add `type ProviderPool struct` with `func NewProviderPool(size int, cfg PoolConfig) *ProviderPool`. A nil pool is a no-op on every method so callers never nil-check.

1. **Acquire.** `Acquire(ctx context.Context, consumer string, weight int) (release func(), err error)` blocks until `weight` slots are free, then returns a release closure. Weight exists because one consumer is not one request: a worker may open several concurrent streams per turn, and the daemon must be able to account for that without pretending a worker costs the same as a single adjudication. `weight <= 0` is treated as 1.
2. **FIFO fairness.** Waiters are served in arrival order. A weighted waiter must not be starved by a stream of lighter ones — if the head of the queue needs 2 slots, a later 1-slot waiter does not jump ahead. Priority is explicitly out of scope: bead ordering is already decided upstream.
3. **Bounded waiting.** `Acquire` honours context cancellation and returns an error on timeout rather than blocking forever. A caller that cannot get a slot must be free to give up and be retried by its own existing retry path — the scheduler's next tick, or the compliance cycle's next pass.
4. **Release is idempotent and safe.** Calling the returned closure twice releases once; a release after pool shutdown does not panic. A leaked slot is worse than a refused request, so `Acquire` must never return a nil release alongside a nil error.
5. **Introspection.** `Stats() PoolStats` reports `Size`, `InUse`, `Waiting`, and per-consumer in-use counts, for the observability in FR-003.
6. Safe for concurrent use; time and queue behaviour testable without sleeping (injectable `now`).

Config `overseer.provider_pool`: `enabled` (default false), `size` (default 4), `acquire_timeout_seconds` (default 120), and `weights` — a `map[string]int` keyed by consumer name so the worker weight can be tuned from measurement rather than guessed in code.

**Acceptance Criteria:**
- A pool of size 2 admits two 1-weight acquirers immediately and blocks the third until one releases
- A 2-weight acquirer waits for two free slots and is not overtaken by a later 1-weight acquirer (FIFO under weight)
- `Acquire` with an already-cancelled context returns an error and acquires nothing
- `Acquire` respects `acquire_timeout_seconds` and returns an error, leaving `InUse` unchanged
- Calling a release closure twice frees exactly one slot's worth
- A nil `*ProviderPool` returns a usable no-op release and a nil error from `Acquire`
- `Stats()` reflects in-use and waiting counts accurately under 10 concurrent goroutines (`go test -race`)
- `NewProviderPool(0, cfg)` yields a disabled pool whose `Acquire` never blocks

### FR-002: Consumer Integration
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/scheduler.go`, `internal/core/gap_adjudicator.go`, `internal/core/validation_service.go`, `internal/core/overseer.go`, `internal/core/provider_pool_integration_test.go`

Route every identified consumer through the pool.

1. Construct one pool in the overseer beside the governor wiring, from `overseer.provider_pool`. Absent block → nil pool → today's behaviour exactly.
2. **Worker spawn** (`scheduler.go`): acquire with consumer `"worker"` before spawning, and release when the worker completes — the release must be tied to worker completion, not to the spawn call returning, or the pool will free a slot while the worker is still calling the provider. On acquire failure, skip the spawn this tick and leave the bead for the next one (it is not a bead failure).
3. **Gap adjudicator** (`gap_adjudicator.go`): acquire with consumer `"adjudicator"` around the `pi` invocation in `adjudicateGap`. This is the longest-held slot in the system (`adjudicator_thinking: max`), so it must be inside the pool rather than beside it.
4. **Validation and verification** (`validation_service.go`): acquire with consumer `"validation"` around provider-invoking validation calls.
5. Acquire failures are logged once per consumer per occurrence and never converted into bead failures or fix beads — a queue timeout says nothing about the code under test.

**Acceptance Criteria:**
- With a pool of size 1 and an adjudication holding the slot, a test asserts the scheduler skips its spawn that tick rather than spawning
- With the pool disabled, a test asserts the same fixture spawns exactly as it does today
- A worker's slot is released on completion, including when the worker fails — asserted by `Stats().InUse` returning to zero after a failed completion
- An acquire timeout produces no fix bead and no bead status change
- `go vet ./internal/core/...` exits 0

### FR-003: Saturation Observability
**Type:** fr
**Priority:** P2

**Target Files:** `internal/core/provider_pool.go`, `internal/core/events.go`, `internal/core/provider_pool_observability_test.go`

Saturation must be visible, because the entire incident this PRD responds to was invisible: refusals looked like provider failures rather than self-inflicted contention.

1. Emit a `pool_saturated` event (via the existing `EventSink`) when a caller waits longer than a threshold (default 5s), with fields `consumer`, `waited_ms`, `in_use`, `waiting`.
2. Emit `pool_timeout` when an acquire gives up, with the same fields.
3. Throttle both to at most one event per consumer per minute — the failure mode of this kind of signal is log spam that gets filtered out and then ignored.
4. Include pool stats in the periodic scheduler status line so a human tailing the log sees `pool 3/4 (1 waiting)` rather than inferring it.

**Acceptance Criteria:**
- A waiter blocked beyond the threshold produces exactly one `pool_saturated` event with a non-zero `waited_ms`
- Ten rapid saturations from one consumer produce one event, not ten
- An acquire timeout produces a `pool_timeout` event
- Events carry the consumer name, so the adjudicator and the fleet are distinguishable in the stream

---

## 4. Non-Functional Requirements

### NFR-001: The pool is inert unless enabled, and never deadlocks
**Type:** nfr-verification
**Priority:** P1

**Target Files:** `internal/core/provider_pool.go`

A queue in front of the provider is a new place for the fleet to stop entirely. It must be impossible for the pool to hold a slot that no longer corresponds to live work.

**Acceptance Criteria:**
- `go build ./...` exits 0
- `go test ./internal/core/... -run 'Pool' -count=1 -race` exits 0
- A test acquires every slot, cancels all holders' contexts, and asserts the pool returns to `InUse == 0`
- A test asserts a worker that exits without a recorded completion still frees its slot (release tied to a deferred path, not to the happy path)
- With no `provider_pool` config block, a test comparing scheduling decisions with and without the pool finds them identical

**Verifies:**
- FR-001: Provider Pool
- FR-002: Consumer Integration

---

## 5. Out of Scope
- **Per-request gating inside worker processes.** Workers are separate processes; gating each provider call a worker makes would need worker↔daemon IPC. This PRD gates at spawn with a configurable weight, which covers the measured contention without a new protocol. If measurement shows a worker's stream count varies widely per turn, revisit with a lease service.
- Changing the AIMD governor or the circuit breaker (see §6)
- Per-provider pools (one global pool; multi-provider budgets come with a real fallback provider)
- Priority or weighted-fair queueing between consumers — `bv` and the DAG own prioritisation
- Persisting queue state across restarts

---

## 6. How this composes with the governor and the breaker

Three mechanisms, three distinct causes, deliberately not merged:

- **Pool (this PRD)** prevents *self-inflicted* contention. We know our own budget, so exceeding it is a bug we can simply not commit.
- **Governor (B1)** adapts to *provider-side* degradation we cannot predict — the ceiling moving under us, or a plan change. With the pool in place the governor should go quiet, which is the tell that the refusals were self-inflicted all along.
- **Breaker** handles a *dead* provider, where the remedy is waiting rather than pacing.

The pool makes the governor's job smaller, not redundant: if the provider tightens its limit below our configured pool size, the governor still notices and thins the fleet.

---

## 7. Adversarial Review

**Reviewed by:** Claude Opus 5 (independent pass after drafting)

- **"A queue in front of the provider can deadlock the fleet."** The mitigations are bounded waiting (FR-001.3), release tied to completion via a deferred path rather than the happy path (FR-002.2), idempotent release, and NFR-001's cancellation test. The failure mode of a leaked slot is explicitly treated as worse than a refused request.
- **"Gating at spawn is an approximation — a worker may open several streams."** Acknowledged and made explicit through per-consumer weights, which are configuration rather than a constant precisely because the true number must be measured. The approximation is strictly better than today, where the worker cost is assumed to be zero for three of four consumers.
- **"`max_workers` and pool size are now two knobs for one thing."** They are different things: `max_workers` bounds concurrent *beads in flight*, the pool bounds concurrent *provider requests*. They coincide today only because a worker is the dominant consumer. Keeping them separate is what lets an adjudication and a worker share a budget without either knob lying.
- **"Why not just give the adjudicator its own provider?"** Considered and rejected: siloing wastes slots whenever a silo is idle, and the adjudicator is idle most of the time. A pool keeps the plan fully utilised, which is the point of paying for it.
- **"FIFO could let one heavy consumer stall the fleet."** The heaviest consumer is the adjudicator, which runs at most twice per compliance cycle and is bounded by `max_cycles`. If measurement shows adjudications stalling spawns materially, the answer is a reserved-slot floor for workers — deliberately deferred rather than designed speculatively.
