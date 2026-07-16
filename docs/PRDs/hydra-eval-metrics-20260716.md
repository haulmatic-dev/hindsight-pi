# PRD: Hydra Run Metrics Extractor (hydra-eval)

## 1. Overview

**Problem:** Every improvement claim about Hydra runs is anecdotal — runs produce artifacts (beads JSONL export, daemon log with `[CloseEvent]` lines) but nothing computes the numbers (one-shot rate, verified-accept count, hang count, wall-clock), so run quality cannot be compared across changes.
**Solution:** A small CLI, `tools/hydra-eval`, that parses a finished run's artifacts and emits a metrics JSON — the measurement half of the eval harness.
**Success Metrics:**
- `hydra-eval --beads-jsonl <file> --daemon-log <file> --out <file>` writes a valid JSON metrics report and exits 0.
- The report's counts match hand-computed values on fixture inputs.
- Missing/partial inputs degrade gracefully (fields zeroed, exit still 0) rather than crashing.

---

## 2. User Stories

### US-001: A finished run yields comparable numbers
**As a** Hydra operator
**I want** one command that turns a run's artifacts into a metrics JSON
**So that** I can compare runs before/after a change instead of eyeballing logs.

**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Artifact Parsers
- FR-002: Metrics Computation
- FR-003: CLI Entrypoint

**Acceptance Criteria:**
- `go test ./tools/hydra-eval/...` exits 0
- Running the built binary against fixture files produces a JSON file whose `verified_accepts` and `total_beads` match the fixtures
- No mocking of internal dependencies in the integration test

---

## 3. Functional Requirements

### FR-001: Artifact Parsers
**Type:** fr
**Priority:** P1

**Target Files:** `tools/hydra-eval/parse.go`

Create package `main` file `tools/hydra-eval/parse.go` with three items. (1) `type CloseEvent struct { BeadID string `json:"bead_id"`; Verdict string `json:"verdict"`; Reason string `json:"reason"`; Attempt int `json:"attempt"` }`. (2) `func ParseCloseEvents(r io.Reader) []CloseEvent` — scans lines for the marker `[CloseEvent] ` and JSON-decodes the remainder of the line into a CloseEvent; undecodable remainders are skipped; the daemon log line prefix (timestamp etc.) before the marker must be tolerated. Use a `bufio.Scanner` with a 1 MB buffer (log lines are long). (3) `type BeadRecord struct { ID string `json:"id"`; Status string `json:"status"`; IssueType string `json:"issue_type"`; CreatedAt string `json:"created_at"`; ClosedAt string `json:"closed_at"` }` and `func ParseBeadsJSONL(r io.Reader) []BeadRecord` — one JSON object per line, skip blank and undecodable lines. (4) `func CountMarker(r io.Reader, marker string) int` — counts lines containing the substring `marker`.

**Acceptance Criteria:**
- `ParseCloseEvents` on the line `2026/07/14 18:52:25 [CloseEvent] {"bead_id":"hydra-dogfood-x1","verdict":"accepted","reason":"verified","attempt":0}` returns one event with `BeadID == "hydra-dogfood-x1"`, `Verdict == "accepted"`, `Attempt == 0`
- `ParseCloseEvents` skips a line containing `[CloseEvent] not-json` without error and without producing an event
- `ParseBeadsJSONL` on two lines `{"id":"a","status":"closed","issue_type":"task"}` and `{"id":"b","status":"open","issue_type":"epic"}` returns two records with those fields
- `ParseBeadsJSONL` skips blank lines and invalid JSON lines
- `CountMarker` on input containing three lines with `STUCK WORKER DETECTED` returns 3
- `go build ./...` exits 0
- `go vet ./tools/hydra-eval/...` exits 0

**Depends On:** —

---

### FR-002: Metrics Computation
**Type:** fr
**Priority:** P1

**Target Files:** `tools/hydra-eval/metrics.go`

Create `tools/hydra-eval/metrics.go` with `type Metrics struct` (JSON tags in snake_case): `TotalBeads int`, `ClosedBeads int`, `VerifiedAccepts int`, `FailedCloses int`, `OneShotAccepts int`, `OneShotRate float64`, `StuckWorkerKills int`, `ScopeWarnings int`, `WallClockSeconds float64`, `BeadsByType map[string]int`. And `func ComputeMetrics(beads []BeadRecord, events []CloseEvent, stuckKills, scopeWarnings, failedCloses int) Metrics`: `TotalBeads` = len(beads); `ClosedBeads` = beads with `Status == "closed"`; `BeadsByType` counts beads per `IssueType`; `VerifiedAccepts` = events with `Verdict == "accepted"`; `OneShotAccepts` = accepted events with `Attempt == 0`; `OneShotRate` = OneShotAccepts / VerifiedAccepts (0 when VerifiedAccepts is 0); `WallClockSeconds` = latest parseable `ClosedAt` minus earliest parseable `CreatedAt` across beads, parsing RFC3339 (`time.RFC3339` then `time.RFC3339Nano` fallback), 0 when fewer than one of each parse; the three remaining fields are passed through.

**Acceptance Criteria:**
- With 3 beads (2 `closed`, 1 `open`; types `task`,`task`,`epic`): `TotalBeads == 3`, `ClosedBeads == 2`, `BeadsByType["task"] == 2`
- With events `[accepted/attempt 0, accepted/attempt 2, rejected/attempt 0]`: `VerifiedAccepts == 2`, `OneShotAccepts == 1`, `OneShotRate == 0.5`
- With zero accepted events, `OneShotRate == 0` (no division panic)
- With `CreatedAt "2026-07-14T10:00:00Z"` on one bead and `ClosedAt "2026-07-14T11:30:00Z"` on another, `WallClockSeconds == 5400`
- Unparseable timestamps are ignored (no panic; `WallClockSeconds == 0` when nothing parses)
- `go build ./...` exits 0
- `go test ./tools/hydra-eval/...` exits 0

**Depends On:**
- FR-001: Artifact Parsers

---

### FR-003: CLI Entrypoint
**Type:** fr
**Priority:** P1

**Target Files:** `tools/hydra-eval/main.go`

Create `tools/hydra-eval/main.go` (package `main`, `func main`). Flags: `--beads-jsonl` (path, required), `--daemon-log` (path, optional, may be given multiple times is NOT required — single value), `--out` (path, default `hydra-eval.json`). Behavior: open beads JSONL and parse with `ParseBeadsJSONL`; if `--daemon-log` given and readable, run `ParseCloseEvents` plus `CountMarker` for `"STUCK WORKER DETECTED"`, `"[ScopeGate]"`, and `"closed as failed"` (the log must be read once into memory or re-opened per pass — either is acceptable); a missing/unreadable daemon log yields zero events/counts, not an error. Compute metrics via `ComputeMetrics`, marshal with `json.MarshalIndent`, write to `--out`, and print the JSON to stdout too. Exit non-zero only when `--beads-jsonl` is missing or unreadable.

**Acceptance Criteria:**
- Built binary with fixture beads JSONL (2 closed beads) and a fixture daemon log (1 accepted CloseEvent) writes `--out` JSON where `total_beads == 2` and `verified_accepts == 1`, and exits 0
- With `--daemon-log` pointing to a nonexistent file, the binary still exits 0 and `verified_accepts == 0`
- Without `--beads-jsonl`, the binary exits non-zero
- The output file is valid JSON (`json.Unmarshal` round-trips in the test)
- `go build ./tools/hydra-eval` exits 0
- `go test ./tools/hydra-eval/...` exits 0

**Depends On:**
- FR-001: Artifact Parsers
- FR-002: Metrics Computation

---

## 4. Non-Functional Requirements

### NFR-SYS: System Integration Verification
**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- Full project builds with `go build ./...` and exits 0
- `go test ./tools/hydra-eval/...` exits 0
- No empty files or placeholder comments remain in `Target Files` of any closed bead
- `tools/hydra-eval/parse.go`, `tools/hydra-eval/metrics.go`, and `tools/hydra-eval/main.go` all compile as one `main` package

**Verifies:**
- FR-001: Artifact Parsers
- FR-002: Metrics Computation
- FR-003: CLI Entrypoint

---

## 5. Technical Considerations

### Architecture
- Pure read-side tool: parses artifacts a run already produces (no daemon changes, no new state). Orchestration (cloning/seeding/watching a run) is deliberately out of scope — a thin shell wrapper owns that.
- Package `main` under `tools/`, matching the existing `tools/gap-to-prd` / `tools/hydra-compliance-analyzer` layout.

### Technology
- Go standard library only (`bufio`, `encoding/json`, `flag`, `io`, `os`, `time`). Table-driven tests with in-memory fixtures (`strings.NewReader`), no network, no git.

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `tools/hydra-eval/parse.go` | fr | — | — |
| FR-002 | `tools/hydra-eval/metrics.go` | fr | FR-001 | — |
| FR-003 | `tools/hydra-eval/main.go` | fr | FR-001, FR-002 | — |
| US-001 | — | us-integration | FR-001..003 | — |
| NFR-SYS | — | nfr-verification | FR-001..003 | — |

### Edge Cases
- Rotated/truncated daemon log: counts reflect what is present; never an error.
- Beads JSONL with compacted/missing timestamp fields: skipped by the wall-clock computation.
- CloseEvent lines wrapped by worker-prefixed output (`[Worker:x] [stderr] ... [CloseEvent] {...}`): the marker scan tolerates any prefix.

### Constraints
- No third-party dependencies.
- Exit 0 on partial data; non-zero only for a missing/unreadable beads JSONL.

---

## 6. MoSCoW

**Must Have:** FR-001/002/003 as specified.
**Should Have:** BeadsByType breakdown; wall-clock from bead timestamps.
**Could Have:** Baseline-diff mode (`--baseline old.json`) printing metric deltas.
**Won't Have:** Run orchestration (cloning, seeding, watching); token/cost metrics (needs the future event ledger).

---

## 7. Research Insights

### Codebase Findings
- CloseEvent line format (from `internal/core/bead_closer.go` `logCloseEvent`): `[CloseEvent] {"bead_id":"…","verdict":"…","reason":"…","attempt":N}`, preceded in the daemon log by a timestamp prefix.
- Beads export `.beads/issues.jsonl` fields (verified live): `id`, `status`, `issue_type`, `created_at`, `closed_at`, `title`, `labels`, `close_reason`, … — one JSON object per line.
- Hang marker in daemon log: `STUCK WORKER DETECTED` (Charon). Scope warnings: lines containing `[ScopeGate]`. Failed closes: `closed as failed`.
- Existing `tools/` binaries (`gap-to-prd`, `hydra-compliance-analyzer`) establish the standalone-main layout this tool follows.

### Historical Patterns
- Four dogfood runs (2026-07-08..16) were compared by hand-grepping logs; accept-rate improvements (8/24 → 32/36) were eyeballed. This tool is the measurement half of the planned eval harness (WO-E in `docs/hydra-analysis-20260712iv.md`).
