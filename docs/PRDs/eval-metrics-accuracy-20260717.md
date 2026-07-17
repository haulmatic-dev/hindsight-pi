# PRD: Eval Metrics Accuracy

## 1. Overview

**Problem:** hydra-eval's first live report exposed two accuracy flaws in its own inputs: (1) the accepted-close event hardcodes `attempt: 0` (`logCloseEvent(bead, "accepted", "verified", 0)`), so the one-shot-rate metric is degenerate (always 1.0); (2) daemon-log-derived counters (stuck kills, scope warnings, close events) span every run since the last log rotation, polluting per-run metrics.
**Solution:** Emit the real validation-attempt count in accepted close events, and give hydra-eval a `--since` timestamp that scopes all daemon-log parsing to the current run.
**Success Metrics:**
- An accepted close after N validation rejections logs `"attempt":N`.
- `hydra-eval --since <ts>` counts only log lines at/after the timestamp.
- Existing behavior unchanged when `--since` is omitted.

---

## 2. User Stories

### US-001: Run metrics reflect one run and real attempts
**As a** Hydra operator
**I want** close events to carry the true attempt number and hydra-eval to scope log parsing to a run window
**So that** one-shot-rate and per-run counters are trustworthy for before/after comparisons.

**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Real Attempt in Accepted Close Events
- FR-002: Timestamp Filter for Log Parsing
- FR-003: --since CLI Wiring

**Acceptance Criteria:**
- `go test ./internal/core/... ./tools/hydra-eval/...` exits 0
- A close accepted after 2 validation rejections produces a `[CloseEvent]` line containing `"attempt":2`, parsed by `ParseCloseEvents` into `Attempt == 2`
- No mocking of internal dependencies in the integration test

---

## 3. Functional Requirements

### FR-001: Real Attempt in Accepted Close Events
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/bead_closer.go`

In `internal/core/bead_closer.go`, the accepted-close event is emitted as `c.logCloseEvent(bead, "accepted", "verified", 0)` (currently ~line 309, end of `verifyBeforeClose`). Replace the hardcoded `0` with the bead's current validation-retry count obtained exactly the way the reject path already does (~lines 353-354): `rounds := 0; if c.validationService != nil && c.validationService.retryManager != nil { rounds = c.validationService.retryManager.GetValidationRetryCount(bead.ID) }` then `c.logCloseEvent(bead, "accepted", "verified", rounds)`. Extract that lookup into a small unexported helper `func (c *BeadCloser) validationRounds(beadID types.BeadID) int` and use it in BOTH the accept path and the existing escalate/reject paths so the three stay consistent. No other behavior changes.

**Acceptance Criteria:**
- A `BeadCloser` whose retry manager reports 2 validation retries for a bead emits an accepted `[CloseEvent]` line containing `"attempt":2`
- With a nil `validationService` (or nil retry manager), the accepted event still emits with `"attempt":0` (no panic)
- The escalate and reject paths compile against the same helper and their emitted attempt values are unchanged in meaning
- `go build ./...` exits 0
- `go test ./internal/core/...` exits 0

**Depends On:** —

---

### FR-002: Timestamp Filter for Log Parsing
**Type:** fr
**Priority:** P1

**Target Files:** `tools/hydra-eval/parse.go`

Add to `tools/hydra-eval/parse.go`: (1) `func ParseLogTime(line string) (time.Time, bool)` — parses the Go standard log prefix at the start of a daemon-log line, layout `2006/01/02 15:04:05` (the first 19 characters); returns `ok == false` when the line is shorter than 19 chars or the prefix does not parse. (2) `func FilterSince(r io.Reader, since time.Time) io.Reader` — returns a reader over only the lines whose parsed timestamp is at or after `since`; lines WITHOUT a parseable timestamp are KEPT (fail-open: worker-stderr lines and wrapped output must not be dropped) — but only once the filter has seen at least one line at/after `since`; before that point, unparseable lines are dropped (they belong to the earlier run's spill). When `since` is the zero `time.Time`, return the input unchanged. Use a `bufio.Scanner` with a 1 MB buffer, matching the existing parsers.

**Acceptance Criteria:**
- `ParseLogTime("2026/07/17 10:00:00 something")` returns 2026-07-17T10:00:00 and `true`
- `ParseLogTime("no timestamp here")` returns `ok == false`
- `FilterSince` over three lines timestamped 09:00, 10:00, 11:00 with `since` = 10:00 yields only the 10:00 and 11:00 lines
- An untimestamped line appearing AFTER a kept timestamped line is kept; an untimestamped line appearing BEFORE any kept line is dropped
- `FilterSince` with zero `since` yields all lines unchanged
- `go build ./...` exits 0
- `go test ./tools/hydra-eval/...` exits 0

**Depends On:** —

---

### FR-003: --since CLI Wiring
**Type:** fr
**Priority:** P1

**Target Files:** `tools/hydra-eval/main.go`

Add a `--since` string flag to `tools/hydra-eval/main.go` (default empty). When non-empty, parse it accepting either RFC3339 (`2006-01-02T15:04:05Z07:00`) or the daemon-log layout (`2006/01/02 15:04:05`); an unparseable value is a fatal error (non-zero exit with a message naming both accepted layouts). Apply `FilterSince(reader, since)` to the daemon-log content before EVERY pass over it (`ParseCloseEvents` and each `CountMarker` call). When `--since` is empty, behavior is byte-for-byte identical to today.

**Acceptance Criteria:**
- Built binary with a fixture daemon log containing one accepted CloseEvent at 09:00 and one at 11:00, run with `--since "2026/07/17 10:00:00"`, reports `verified_accepts == 1`
- The same invocation without `--since` reports `verified_accepts == 2`
- `--since garbage` exits non-zero and the error message mentions both accepted layouts
- `go build ./tools/hydra-eval` exits 0
- `go test ./tools/hydra-eval/...` exits 0

**Depends On:**
- FR-002: Timestamp Filter for Log Parsing

---

## 4. Non-Functional Requirements

### NFR-SYS: System Integration Verification
**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- Full project builds with `go build ./...` and exits 0
- `go test ./internal/core/... ./tools/hydra-eval/...` exits 0
- No empty files or placeholder comments remain in `Target Files` of any closed bead
- `internal/core/bead_closer.go`, `tools/hydra-eval/parse.go`, and `tools/hydra-eval/main.go` all compile

**Verifies:**
- FR-001: Real Attempt in Accepted Close Events
- FR-002: Timestamp Filter for Log Parsing
- FR-003: --since CLI Wiring

---

## 5. Technical Considerations

### Architecture
- FR-001 reuses the exact retry-count source the reject path already trusts — one helper, three call sites, no new state.
- FR-002's fail-open-after-first-match rule keeps multi-line worker output attached to its run while excluding the previous run's spill — the pragmatic middle until the event ledger exists.

### Technology
- Go standard library only. In-memory fixtures; no network, no git.

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `internal/core/bead_closer.go` | fr | — | — |
| FR-002 | `tools/hydra-eval/parse.go` | fr | — | — |
| FR-003 | `tools/hydra-eval/main.go` | fr | FR-002 | — |
| US-001 | — | us-integration | FR-001..003 | — |
| NFR-SYS | — | nfr-verification | FR-001..003 | — |

### Edge Cases
- Year-less or truncated prefixes: `ParseLogTime` returns false → line treated by the fail-open rule.
- `--since` in the future: zero events/counters, exit 0.
- Rotated log where the whole file predates `--since`: zero counters, exit 0.

### Constraints
- No behavior change without `--since`; no change to reject/escalate attempt semantics.

---

## 6. MoSCoW

**Must Have:** FR-001/002/003.
**Should Have:** Helper shared across accept/reject/escalate paths.
**Could Have:** `--since auto` reading the newest `[ComplianceCycle] Starting` line. **Won't Have:** event ledger; per-run token metrics.

---

## 7. Research Insights

### Codebase Findings
- Hardcoded accept event: `internal/core/bead_closer.go` ~309 `c.logCloseEvent(bead, "accepted", "verified", 0)`; reject path reads `c.validationService.retryManager.GetValidationRetryCount(beadID)` (~353-354) — the proven source.
- Daemon log lines use Go's default log prefix `2006/01/02 15:04:05`.
- `tools/hydra-eval/parse.go` scanners already use 1 MB buffers; keep parity.

### Historical Patterns
- These are the first two findings produced by hydra-eval measuring its own birth-run (2026-07-17): `one_shot_rate: 1` (degenerate) and counters spanning runs since rotation. This PRD closes the measurement loop it opened.
