# PRD: Hydra Observer — Phase 1 (Detect + Backlog)

## 1. Overview

**Problem:** Defects in hydra's own operation (empty-diff rejection storms, stuck-worker kills, panics, blocked users, metric regressions) are found today by hand via log forensics; nothing watches the daemon and files the finding.
**Solution:** `tools/hydra-observer` — the deterministic core of roadmap Phase 1 (`docs/roadmap/self-improvement-loop.md`): scan the daemon log for known failure signatures since a timestamp, compare `hydra-eval` metrics against a stored baseline, and write deduplicated ranked backlog entries. No production mutation; the systemd timer and LLM triage escalation are out of scope (ops / Phase 1b).
**Success Metrics:**
- Replaying the empty-diff incident's log window produces a backlog entry flagging it (the roadmap's stated exit signal).
- Re-running over the same window creates zero duplicate entries.
- A quiet log + unchanged metrics produces zero entries and exit 0.

---

## 2. User Stories

### US-001: The daemon is watched by a machine, not a human
**As a** Hydra operator
**I want** a tool that turns log signatures and metric regressions into ranked backlog entries
**So that** defects in hydra's own operation are detected within one polling interval instead of days of manual forensics.

**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Log Signature Scanner
- FR-002: Metrics Baseline Compare
- FR-003: Backlog Writer with Dedupe
- FR-004: CLI Wiring

**Acceptance Criteria:**
- `go test ./...` in `tools/hydra-observer` exits 0
- Running the built binary against a fixture log containing 3 `(empty-diff)` rejection lines and an empty backlog dir exits 1 and creates exactly one `.md` file in the backlog dir whose content matches `/empty-diff/`
- Running it a second time with the same inputs exits 0 and the backlog dir still contains exactly one file
- No mocking of internal dependencies in the integration test

---

## 3. Functional Requirements

### FR-001: Log Signature Scanner
**Type:** fr
**Priority:** P1

**Target Files:** `tools/hydra-observer/signatures.go`, `tools/hydra-observer/signatures_test.go`

Line-oriented scanner for the daemon log (same log format `tools/hydra-eval/parse.go` reads: Go `log` package lines with `YYYY/MM/DD HH:MM:SS` prefixes). Define:

```go
type Signature struct {
    Key       string // stable identifier, e.g. "empty-diff"
    Marker    string // substring to match, case-sensitive
    Severity  string // "critical" | "warning"
    Threshold int    // min hits in the window before it becomes an anomaly
}

type Hit struct {
    Key   string
    Count int
    First string // first matching line, verbatim (evidence)
}

func ScanLog(r io.Reader, since time.Time, sigs []Signature) []Hit
```

`ScanLog` parses each line's timestamp prefix (lines without a parseable timestamp inherit the previous line's — multi-line panics must not be dropped), skips lines before `since` (zero `since` disables filtering), counts marker substring matches per signature, and returns one `Hit` per signature whose count ≥ its threshold. The default signature table (exported as `DefaultSignatures() []Signature`) covers the roadmap's known list, with markers taken from the actual log call sites:

| Key | Marker | Severity | Threshold |
|-----|--------|----------|-----------|
| `empty-diff` | `(empty-diff)` | critical | 3 |
| `stuck-worker` | `STUCK WORKER DETECTED` | warning | 1 |
| `panic` | `panic:` | critical | 1 |
| `blocked-user` | `] BLOCKED ` | warning | 1 |
| `escalation` | `escalating to human` | warning | 1 |
| `worker-timeout` | `exceeded timeout` | warning | 3 |
| `max-cycles` | `MAX CYCLES` | warning | 1 |

**Acceptance Criteria:**
- `ScanLog` over a fixture with 3 lines containing `(empty-diff)` after `since` and 2 before returns a Hit `{Key: "empty-diff", Count: 3}` when `since` sits between them
- `ScanLog` over the same fixture with 2 matching lines after `since` returns no `empty-diff` hit (below threshold 3)
- `ScanLog` with a single line containing `STUCK WORKER DETECTED` returns a Hit with `Count` equal to `1` and `First` equal to that exact line
- A multi-line `panic:` block whose continuation lines have no timestamp prefix is counted once, not dropped and not double-counted
- `ScanLog` with zero `since` counts all lines
- `go test ./...` exits 0

**Depends On:** —

---

### FR-002: Metrics Baseline Compare
**Type:** fr
**Priority:** P1

**Target Files:** `tools/hydra-observer/baseline.go`, `tools/hydra-observer/baseline_test.go`

Compare a current `hydra-eval` metrics JSON (schema: the `Metrics` struct in `tools/hydra-eval/metrics.go` — duplicate the struct locally like `internal/core/gap_adjudicator.go` duplicates the gap schema, since both tools are separate `main` packages) against a stored baseline file. Define:

```go
type Regression struct {
    Field    string  // e.g. "one_shot_rate"
    Baseline float64
    Current  float64
    Severity string
}

func Compare(baseline, current Metrics) []Regression
func LoadMetrics(path string) (Metrics, error)
```

Rules (each producing one `Regression` when violated): `one_shot_rate` dropped by more than 0.15 absolute (severity `warning`); `stuck_worker_kills` increased (severity `warning`); `failed_closes` increased by more than 2 (severity `warning`); `verified_accepts` is 0 while `total_beads` > 0 (severity `critical`). A missing baseline file is not an error: `Compare` is skipped and the caller stores the current metrics as the new baseline.

**Acceptance Criteria:**
- `Compare(Metrics{OneShotRate: 0.8}, Metrics{OneShotRate: 0.6})` returns exactly one Regression with `Field` equal to `"one_shot_rate"`
- `Compare(Metrics{OneShotRate: 0.8}, Metrics{OneShotRate: 0.7})` returns no `one_shot_rate` regression (within tolerance; use a 1e-9 epsilon on the 0.15 threshold, not exact float equality)
- `Compare(Metrics{StuckWorkerKills: 0}, Metrics{StuckWorkerKills: 1})` returns one Regression with `Field` equal to `"stuck_worker_kills"`
- `Compare` with `current.VerifiedAccepts` equal to 0 and `current.TotalBeads` equal to 10 returns a Regression with `Severity` equal to `"critical"`
- `LoadMetrics` on a missing path returns an error matching `/no such file/`
- `go test ./...` exits 0

**Depends On:** —

---

### FR-003: Backlog Writer with Dedupe
**Type:** fr
**Priority:** P1

**Target Files:** `tools/hydra-observer/backlog.go`, `tools/hydra-observer/backlog_test.go`

Persist anomalies as one markdown file per unique finding, deduplicated by fingerprint so one defect never becomes N entries across runs (modeled on `gapFingerprint` in `internal/core/gap_adjudicator.go:48-57`). Define:

```go
type Entry struct {
    Kind     string // "signature" | "regression"
    Key      string // signature key or regression field
    Severity string
    Evidence string // first matching line / baseline-vs-current values
}

func Fingerprint(e Entry) string            // sha1 hex (first 8 bytes) of kind|key|severity
func WriteBacklog(dir string, entries []Entry) (written int, err error)
```

`WriteBacklog` creates `dir` if missing, and for each entry writes `<dir>/<fingerprint>.md` containing: a title line `# Observer: <key> (<severity>)`, the evidence block, and a `Fingerprint:` footer line. If the file already exists, the entry is skipped (dedupe across runs — the fingerprint intentionally excludes counts and timestamps so a recurring defect maps to the same file). Entries are written ordered critical-first, then by key.

**Acceptance Criteria:**
- `Fingerprint(Entry{Kind:"signature",Key:"empty-diff",Severity:"critical"})` returns the same 16-char hex string on every call
- `WriteBacklog` with two entries into an empty temp dir returns `written` equal to `2` and the dir contains exactly 2 `.md` files
- Calling `WriteBacklog` again with the same entries returns `written` equal to `0` and the dir still contains exactly 2 files
- A written file for key `empty-diff` contains the exact literal string `# Observer: empty-diff (critical)` and its evidence line
- `WriteBacklog` into a non-existent nested dir path creates it and succeeds
- `go test ./...` exits 0

**Depends On:** —

---

### FR-004: CLI Wiring
**Type:** fr
**Priority:** P1

**Target Files:** `tools/hydra-observer/main.go`, `tools/hydra-observer/main_test.go`, `tools/hydra-observer/go.mod`

**Reads From:**
- FR-001: Log Signature Scanner
- FR-002: Metrics Baseline Compare
- FR-003: Backlog Writer with Dedupe

CLI: `hydra-observer --log <daemon-log> [--since <RFC3339>] [--metrics <hydra-eval.json>] [--baseline <path>] --backlog-dir <dir>`. Flow: scan the log (FR-001, default signatures) → if `--metrics` is given, load it, compare against `--baseline` (default `<backlog-dir>/../observer-baseline.json`; on first run store current as baseline and skip comparison) → convert hits and regressions to entries → `WriteBacklog` → print a one-line summary per new entry to stdout and a machine-readable JSON summary `{"new_entries": n, "hits": n, "regressions": n}` as the final stdout line. On success the current metrics overwrite the baseline file. Exit codes: 0 = no new backlog entries, 1 = new entries written, 2 = configuration error (missing `--log`/`--backlog-dir`, unreadable log). Own `go.mod` (module `github.com/haulmatic-dev/hydra/tools/hydra-observer`, standard library only). The tool never mutates anything outside `--backlog-dir` and the baseline file (roadmap safety invariant: observation only).

**Acceptance Criteria:**
- Running with no `--log` exits 2 and stderr matches `/--log/`
- Against a fixture log with 3 `(empty-diff)` lines and an empty backlog dir: exits 1, the dir contains exactly one `.md` file, and the final stdout line parses as JSON with `new_entries` equal to `1`
- Immediately re-running the same command exits 0 with `new_entries` equal to `0` and no new files
- With `--metrics` pointing at a fixture whose `one_shot_rate` is 0.5 and `--baseline` at a fixture with 0.8: exits 1 and a backlog file matching `/one_shot_rate/` exists
- With `--metrics` given and no baseline file present: exits 0 (assuming a quiet log), the baseline file is created, and no regression entry is written
- `go build ./...` exits 0
- `go test ./...` exits 0

**Depends On:** —

---

## 4. Non-Functional Requirements

### NFR-SYS: System Integration Verification
**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- `go build ./...` in `tools/hydra-observer` exits 0
- `go test ./...` in `tools/hydra-observer` exits 0
- Root `go build ./...` still exits 0 (the tool has its own module; the root build must be unaffected)
- No empty files or placeholder comments remain in `Target Files` of any closed bead

**Verifies:**
- FR-001: Log Signature Scanner
- FR-002: Metrics Baseline Compare
- FR-003: Backlog Writer with Dedupe
- FR-004: CLI Wiring

---

## 5. Technical Considerations

### Architecture
- Deterministic core only. The roadmap's cheap-model triage / strong-model diagnosis escalation (Phase 1 steps 2–3) is intentionally NOT in this PRD: it isn't dogfoodable deterministically and belongs to a follow-up once this core's false-positive rate is measured. The backlog `.md` files are the interface a later triage step (or a human) consumes.
- Separation invariant from the roadmap: the observer is a standalone binary run by a systemd timer (ops), never in-process with the daemon it watches.
- Fingerprint dedupe deliberately excludes counts/timestamps so a recurring defect converges on one file; severity is included so an escalation from warning to critical surfaces as a new entry.

### Technology
- Go standard library only; table-driven tests with fixture logs under `tools/hydra-observer/testdata/`.
- Log format facts: Go `log` default prefix `2026/07/22 14:03:01 `; parsing mirrors `tools/hydra-eval/parse.go` (`ParseDaemonLogSince`) but is reimplemented locally because both tools are `main` packages.

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `tools/hydra-observer/signatures.go`, `signatures_test.go` | fr | — | — |
| FR-002 | `tools/hydra-observer/baseline.go`, `baseline_test.go` | fr | — | — |
| FR-003 | `tools/hydra-observer/backlog.go`, `backlog_test.go` | fr | — | — |
| FR-004 | `tools/hydra-observer/main.go`, `main_test.go`, `go.mod` | fr | Reads From FR-001..003 | — |
| US-001 | — | us-integration | FR-001..FR-004 | — |
| NFR-SYS | — | nfr-verification | FR-001..FR-004 | — |

### Edge Cases
- Log rotation mid-window: the tool reads one file per invocation; the timer wrapper (ops) passes the right path. Out of scope here.
- Timestamps in other formats (worker logs): out of scope — Phase 1 watches the daemon log only.
- Enormous logs: `bufio.Scanner` with a 1 MiB max line buffer; lines longer than that are truncated, never fatal.
- Clock skew / `--since` in the future: yields zero hits, exit 0 — not an error.

### Constraints
- Observation only: no bead creation, no daemon interaction, no network. Backlog dir + baseline file are the only writes.
- Signature markers must match the actual log call sites verbatim (documented per marker in a comment referencing the emitting file, e.g. `internal/core/bead_closer.go:407` for `(empty-diff)`).

---

## 6. MoSCoW

**Must Have:** FR-001..FR-004, dedupe, exit-signal AC (empty-diff replay).
**Should Have:** Metrics regression rules; critical-first ordering.
**Could Have:** `--signatures <json>` override file for custom markers.
**Won't Have:** LLM triage/diagnosis (Phase 1b); PRD auto-generation (Phase 2 Proposer); any auto-fix (Phase 3 Actor); systemd timer (ops).

---

## 7. Research Insights

### Codebase Findings
- Marker sources: `[VerifiedClose] Bead %s rejected (%s)` with reason `empty-diff` (`internal/core/bead_closer.go:339,407`), `STUCK WORKER DETECTED` (`internal/core/charon.go:221`), `] BLOCKED ` (`internal/core/multi_user_discovery.go:87`), `escalating to human` (`internal/core/bead_closer.go:397`), `exceeded timeout` (`internal/core/agent_worker.go:139`), `MAX CYCLES` (`internal/core/compliance_cycle.go:168`).
- `hydra-eval` metrics schema: `tools/hydra-eval/metrics.go` (`total_beads`, `verified_accepts`, `one_shot_rate`, `stuck_worker_kills`, `failed_closes`, …); its log parsing helpers live in `tools/hydra-eval/parse.go` and are the pattern to mirror.
- Fingerprint precedent: `gapFingerprint` (`internal/core/gap_adjudicator.go:48-57`) — sha1 over normalized identity fields, first 8 bytes hex.

### Historical Patterns
- Roadmap Phase 1 and its exit signal: `docs/roadmap/self-improvement-loop.md:34-47`. Evidence incidents: BLOCKED-homes cry-wolf and the empty-diff gate bug (`CHANGELOG.md:117-122`).
