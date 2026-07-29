# PRD: Adaptive Concurrency Governor (Shepherd B1)

## 1. Overview

**Problem:** Hydra runs a fixed number of concurrent workers (`max_workers`, currently 5) regardless of provider health. When the provider degrades — the recurring "engine is currently overloaded" state — five workers pile into the same overload band: the 2026-07-28 known-answer run logged **219 activity timeouts against 4 clean turns**, and one dogfood window closed 0 beads in 45 minutes while sequential requests succeeded 9/9. The fleet's own request pressure raises its exposure to the very errors stalling it. Nothing adapts: the only levers today are the all-or-nothing provider circuit and a human editing config.

**Solution:** An AIMD (additive-increase, multiplicative-decrease) governor that shrinks effective concurrency when provider-health signals degrade and regrows it slowly when turns succeed — the congestion-control pattern TCP has used for decades, applied to worker slots. Never a hard stop: the floor is one worker, so progress continues through the worst storm. Adapts, self-releases, and emits an event on every change so the behaviour is observable.

**Success Metrics:**
- During a simulated failure burst, effective concurrency drops multiplicatively (5 → 2 on sustained failures) within one evaluation window.
- After sustained successes, concurrency recovers by exactly one slot per window until the configured maximum.
- Effective concurrency never leaves the range [1, max_workers], and with the governor disabled behaviour is byte-identical to today.

---

## 0. Scope Profile
- **System Type:** library (internal Go package, daemon-side)
- **Interfaces:** internal Go API only
- **Surfaces Explicitly Excluded:** the provider circuit breaker (`provider_breaker.go`, unchanged), worker binary, retry policy, bead selection order, config file format beyond one new block
- **Deployment Model:** single-binary daemon

---

## 2. User Stories

### US-001: The fleet backs off a degraded provider by itself — Integration Bead
**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: AIMD Governor
- FR-002: Scheduler Integration

**Acceptance Criteria:**
- `go build ./...` exits 0
- `go test ./internal/core/...` exits 0
- With the governor enabled and a recorded burst of failures, the scheduler's available slots shrink on the next tick without a daemon restart
- With `concurrency_governor.enabled: false` (or the block absent), scheduler behaviour is unchanged from today
- No mocking of internal dependencies in the integration test: it drives the real governor and the real slot arithmetic

---

## 3. Functional Requirements

### FR-001: AIMD Governor
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/concurrency_governor.go`, `internal/core/concurrency_governor_test.go`

Add `type ConcurrencyGovernor struct` with constructor `func NewConcurrencyGovernor(maxWorkers int, cfg GovernorConfig) *ConcurrencyGovernor`. A nil governor is a no-op on every method (callers never nil-check). Safe for concurrent use.

1. **Signals in.** `RecordOutcome(success bool)` is called once per worker completion; failures counted are provider-shaped only — the caller decides (timeouts and exit codes 2/7), not this type. `RecordOutcome` appends to a sliding window of the most recent `WindowSize` outcomes (default 8).
2. **Decrease.** After each recorded outcome, when the window holds at least `WindowSize` entries and the failure fraction is ≥ `DecreaseThreshold` (default 0.5), halve the current limit (round up), clamp to a floor of 1, clear the window, and record the decision time. Halving, not decrement: the point of multiplicative decrease is to exit the overload band in one or two steps rather than dribbling down while every extra worker keeps failing.
3. **Increase.** When the failure fraction is ≤ `IncreaseThreshold` (default 0.125, i.e. at most one failure in a full window) and at least `CooldownSeconds` (default 120) have passed since the last change, add one slot, clamp to `maxWorkers`, clear the window, record the decision time. Additive increase probes capacity gently.
4. **Read.** `Limit() int` returns the current effective limit; it starts at `maxWorkers`. `maxWorkers <= 0` (unlimited mode) disables the governor entirely: `Limit()` returns 0 and `RecordOutcome` is a no-op, because there is no ceiling to adapt.
5. Every limit change calls an injectable `onChange func(old, new int, reason string)` hook (nil-safe) — the scheduler wires this to logging and the event stream. Reasons are the slugs `"multiplicative_decrease"` and `"additive_increase"`.
6. Time is injectable (`now func() time.Time` field) so cooldown behaviour is testable without sleeping.

**Acceptance Criteria:**
- A fresh governor with maxWorkers 5 returns `Limit()` of 5
- Recording 4 failures and 4 successes (failure fraction 0.5, window full) drops `Limit()` from 5 to 3 (halve 5 → ceil 2.5 → 3)
- Recording a further full window at failure fraction ≥ 0.5 drops 3 to 2, then 2 to 1, and never below 1
- With the window at 8 successes and the cooldown elapsed, `Limit()` rises by exactly 1 per window, and never above maxWorkers
- With the cooldown NOT elapsed, a success-filled window does not raise the limit
- `NewConcurrencyGovernor(0, cfg).Limit()` returns 0 and 100 recorded outcomes leave it 0
- A nil `*ConcurrencyGovernor` tolerates `RecordOutcome` and `Limit()` without panicking
- The onChange hook receives (5, 3, "multiplicative_decrease") on the first decrease
- Concurrent `RecordOutcome` calls from 10 goroutines cause no race (`go test -race`)

### FR-002: Scheduler Integration
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/scheduler.go`, `internal/core/worker_lifecycle.go`, `internal/core/scheduler_governor_test.go`

Wire the governor into slot arithmetic and outcome recording.

1. In `internal/core/scheduler.go`, where `availableSlots` is computed from `maxWorkers - activeCount` (around line 105): when a governor is present and `Limit() > 0`, use `min(maxWorkers, Limit())` in place of `maxWorkers`. The existing unlimited-mode branch (`maxWorkers == 0`) is untouched. Log the substitution only when the governed limit differs from the configured maximum.
2. In `internal/core/worker_lifecycle.go`, record outcomes at the completion site: success → `RecordOutcome(true)`; failure with exit codes 2 or 7, or an activity-timeout failure (exit 3), → `RecordOutcome(false)`; all other failures (real code failures, exit 1/5) are NOT recorded — a bead that fails on its own defect says nothing about provider health and must not shrink the fleet.
3. Construct the governor in the overseer next to the existing breaker wiring, from a new config block `overseer.concurrency_governor` with fields `enabled` (default false), `window_size`, `decrease_threshold`, `increase_threshold`, `cooldown_seconds` — all optional with the FR-001 defaults. `enabled: false` or an absent block constructs no governor, and every call site is nil-safe, so default behaviour is byte-identical to today.
4. On every limit change (via the onChange hook) the scheduler-side wiring logs `[Governor] concurrency %d -> %d (%s)`.

**Acceptance Criteria:**
- With a governor whose `Limit()` is 2, maxWorkers 5 and 1 active worker, the scheduler computes 1 available slot
- With no governor constructed, the computed slots equal today's `maxWorkers - activeCount`
- A worker completing with exit code 7 records a failure outcome; exit code 1 records nothing; success records a success outcome
- The config block parses: `enabled: true` with defaults yields a governor with window 8, thresholds 0.5/0.125, cooldown 120s
- `go vet ./internal/core/...` exits 0

---

## 4. Non-Functional Requirements

### NFR-001: The governor is inert unless enabled
**Type:** nfr-verification
**Priority:** P1

**Target Files:** `internal/core/concurrency_governor.go`

Rollout safety: merging and deploying this with the config block absent must change nothing.

**Acceptance Criteria:**
- `go build ./...` exits 0
- `go test ./internal/core/... -run 'Governor' -count=1` exits 0
- With no `concurrency_governor` config block, the scheduler's slot computation is unchanged (asserted by a test that runs the slot arithmetic with and without the governor and compares)

**Verifies:**
- FR-002: Scheduler Integration

---

## 5. Out of Scope
- Changing the provider circuit breaker or its thresholds
- Any hard budget or spend cap (explicitly rejected: adapts-and-self-releases only)
- Per-provider or per-model limits (one global effective limit)
- Worker-side changes; retry policy; bead prioritisation
- The pathology brake (Shepherd B2) — that consumes this governor's signals later but is a separate change

---

## 6. Adversarial Review

**Reviewed by:** Claude Opus 5 (independent pass after drafting)

- **"Halving on a 50% failure window is aggressive when the provider blips once."** The window requires `WindowSize` (8) recorded outcomes with ≥ 4 provider-shaped failures — a single blip cannot fill that. And the failure set deliberately excludes code failures, so a bad PRD cannot shrink the fleet.
- **"Additive increase every 2 minutes takes ~8 minutes to recover from 1 to 5."** Deliberate: TCP's lesson is that fast reprobing re-enters the overload band. Recovery time is bounded and observable; each step emits an event.
- **"Governor and circuit breaker can both act — double braking."** They compose safely: the breaker handles a *dead* provider (quota, sustained 429s) by pausing spawns entirely; the governor handles a *degraded* one by thinning them. The breaker's pause dominates when both fire, which is correct.
- **"Clearing the window after each change loses information."** Intentional: after a limit change the old outcomes describe a different concurrency level; keeping them would double-count the same congestion into a second decision.
