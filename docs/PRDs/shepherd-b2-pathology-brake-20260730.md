# PRD: Pathology Brake (Shepherd B2)

## 1. Overview

**Problem:** The AIMD governor (B1) protects the fleet from a *degraded provider*, but nothing protects a *single piece of work* from burning the fleet's time on a known-bad pattern. Measured pathologies: a discovery bead retried through 4 duplicate fix attempts producing five identical implementations (run D3); a seeded-impossible requirement generated 158 beads of churn before the caps landed; the same chain quarantined repeatedly means the fix loop is not converging. Today each retry costs a full worker slot and provider tokens immediately, no matter how often that exact work has just failed.

**Solution:** A per-subject brake: when a failure *signature* (same original bead failing the same way) repeats, subsequent attempts on that subject are deferred with exponential backoff — the bead stays open, nothing is capped or killed, and the brake self-releases on success or signature change. Flow-shaping, not regulation: the fleet spends its slots on work that is moving, and stuck work gets progressively cheaper to carry. Never a hard stop (user constraint).

**Success Metrics:**
- A bead failing 3× with the same signature waits ≥ the configured backoff before attempt 4, while unrelated beads schedule freely in the same ticks.
- A braked bead that succeeds (or fails with a *different* cause) resets to no brake.
- With the brake disabled (config absent), scheduling is byte-identical to today.

---

## 0. Scope Profile
- **System Type:** library (internal Go, daemon-side)
- **Interfaces:** internal Go API only
- **Surfaces Explicitly Excluded:** the AIMD governor (unchanged; it shapes global concurrency, the brake shapes per-subject cadence), fix-bead creation/dedup/caps (unchanged), the retry manager's infra backoff (unchanged — the brake composes above it), worker binary
- **Deployment Model:** single-binary daemon

---

## 2. User Stories

### US-001: Stuck work slows down; moving work does not — Integration Bead
**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Failure Signatures And Brake State
- FR-002: Scheduler Deferral Integration

**Acceptance Criteria:**
- `go build ./...` exits 0
- `go test ./internal/core/...` exits 0
- An integration test drives three same-signature failures through the real closer and asserts the scheduler defers the subject while scheduling an unrelated ready bead in the same tick
- With `overseer.pathology_brake.enabled: false` or the block absent, a test asserts identical scheduling decisions to a brakeless run
- No mocking of internal dependencies

---

## 3. Functional Requirements

### FR-001: Failure Signatures And Brake State
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/pathology_brake.go`, `internal/core/pathology_brake_test.go`

1. `func FailureSignature(beadID string, exitCode int, cause string) string` — a stable hash of the *original* bead ID (fix beads map to their original via the existing `fixTitlePrefix` convention and dependency), the exit code class (code failure vs each infra class), and the first structurally-anchored failure line of the cause (empty cause hashes as its own class). Same failure → same signature; a different failing test or different exit class → different signature.
2. `type PathologyBrake struct` with `func NewPathologyBrake(cfg BrakeConfig) *PathologyBrake`; nil receiver is a total no-op. Config: `enabled` (default false), `base_delay_seconds` (default 300), `max_delay_seconds` (default 3600), `trigger_count` (default 2).
3. `RecordFailure(subject, signature string, now time.Time)`: same signature seen `trigger_count` times consecutively → brake engages with delay `base * 2^(n-trigger_count)` clamped to max. A *different* signature for the subject resets the count to 1 (new problem, fresh start).
4. `RecordSuccess(subject string)` clears all state for the subject.
5. `DeferUntil(subject string, now time.Time) (time.Time, bool)` — the scheduler's read. Returns false when unbraked.
6. Every engage/release calls a nil-safe `onChange(subject, signature string, until time.Time, engaged bool)` hook; state is in-memory only (a daemon restart releases all brakes — deliberate: restarts are rare and a stale brake is worse than a retried failure).
7. Concurrency-safe; injectable clock.

**Acceptance Criteria:**
- Two identical failures (trigger_count 2) engage the brake at base delay; the third doubles it; clamp at max
- A different signature after two identical ones resets: no brake until it repeats
- `RecordSuccess` releases immediately
- Nil brake and disabled config: all methods no-op, `DeferUntil` false
- Race test: 10 goroutines recording outcomes on 3 subjects (`go test -race`)
- The onChange hook fires on engage and release with correct fields

### FR-002: Scheduler Deferral Integration
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/scheduler.go`, `internal/core/worker_lifecycle.go`, `internal/core/config.go`, `internal/core/brake_scheduler_test.go`

1. Construct the brake in the overseer from `overseer.pathology_brake` config, next to the governor wiring; absent block → nil brake.
2. Worker completion site: failures that already record governor outcomes also call `RecordFailure` with the signature built from the close context (reuse the extracted cause already in hand at the failed-close site — do not re-read logs); successes call `RecordSuccess`. Infra failures (exit 2/3/6/7) do NOT feed the brake — the retry manager's backoff already owns those; the brake is for *code-shaped* repetition.
3. Scheduler ready-bead loop: a bead whose subject is braked (`DeferUntil` true) is skipped this tick with one log line per subject per engage (not per tick), and — via the onChange hook — a `brake_engaged` / `brake_released` event through the existing sink, `Fields: {subject, signature, until}`.
4. The subject of a fix bead is its original bead ID, so the brake follows the work, not the bead identity.

**Acceptance Criteria:**
- A braked subject's bead is skipped while an unrelated bead spawns in the same tick
- After the deferral elapses (injected clock), the bead schedules normally
- Exit-7 failures never engage the brake
- `brake_engaged` and `brake_released` events appear in the event stream exactly once per transition
- With no brake constructed, the ready-loop path is byte-identical (asserted by comparing scheduling decisions on the same fixture with brake nil vs disabled)
- `go vet ./internal/core/...` exits 0

---

## 4. Non-Functional Requirements

### NFR-001: The brake shapes flow; it can never stop it
**Type:** nfr-verification
**Priority:** P1

**Target Files:** `internal/core/pathology_brake.go`

**Acceptance Criteria:**
- `go build ./...` exits 0
- `go test ./internal/core/... -run 'Brake' -count=1` exits 0
- A test asserts the deferral never exceeds `max_delay_seconds` regardless of failure count (no unbounded growth, no permanent block)
- A test asserts a braked subject is *deferred*, not closed, capped, or labelled — bead state is untouched by the brake

**Verifies:**
- FR-001: Failure Signatures And Brake State
- FR-002: Scheduler Deferral Integration

---

## 5. Out of Scope
- Persisting brake state across restarts
- Braking on infra failures (retry manager owns those)
- Global spawn shaping (governor owns that)
- Auto-escalation of long-braked subjects (the shepherd cron's fix_storm/quarantine_repeat findings already surface them)

---

## 6. Adversarial Review

**Reviewed by:** Claude Fable 5 (independent pass after drafting)

- **"Is this a regulation that stops things moving?"** No path stops work: max deferral is bounded (default 1h), the bead stays open, success or a *new* failure mode releases instantly, and restarts clear everything. The brake only reorders *when* stuck work retries, freeing slots for work that is moving — the opposite of the churn caps, which refuse work.
- **"Signature on cause text is fragile — model prose varies."** The signature uses the structurally-anchored first failure line (post-A2-fix extractor), which for real failures is a go test/build line, stable across retries. When the cause is empty or unstable the signature degrades to (bead, exit-class) — coarser but still correct.
- **"Overlap with fix-bead dedup?"** Dedup prevents duplicate *beads*; the brake prevents rapid *retries of one bead*. Run D3 showed both are needed: dedup was dead (now fixed) AND each duplicate retried immediately.
- **"Deferral hides a bead the operator expects to run."** Every engage emits `brake_engaged` with the until-time into both event tiers, and the D1 shepherd surfaces persistent ones as findings.
