# PRD: Memory and Recall Quality (Hindsight/CASS)

**Status:** Draft (planning-complete, pending approval)
**Date:** 2026-08-13
**Origin:** operator directive — "verify the quality of the recalls"; recall
quality is a first-class hydra enhancement, measured before tuned.

---

## 1. Overview

**Problem:** Workers receive Hindsight enrichment on every bead (limit 5,
max 800 tokens) and retain execution memories, but nothing measures whether
recalls are relevant, used, or valuable — and the memory banks accumulate
duplicates and stale entries (CASS showed 3× identical junk rules; the
2026-08-13 operator session received August-6 hung-worker memories during a
post-fix planning conversation — stale recall in production).

**Solution:** Telemetry-first recall-quality measurement (relevance,
utilization, counterfactual value, bank health), then evidence-based tuning
of retain/recall knobs.

**Success Metrics:**
- 100% of worker beads log a structured enrichment record (items, relevance scores, tokens injected)
- Recall→outcome correlation report reproducible from one command
- Golden-probe precision@5 baseline established for N≥20 documented past incidents
- Bank hygiene report: dup rate and stale-entry rate for CASS + Hindsight, with deprecation candidates listed
- Tuning change proposal with quantified expected token delta (no tuning before measurement)

---

## 2. Research Insights

Verified 2026-08-13:

- Transcript-tax measurement (saihm.coti.global): compact memory recall uses
  62.8–85.9% fewer context tokens than transcript re-send — recall quality is
  a token-cost lever, not just correctness.
- Judge-calibration pattern (Temrel verification-starter; startdebugging
  LLM-as-judge harness): score the judge/recaller against recorded failures
  before trusting it. Applied here as golden-probe precision@k.
- Live evidence: worker logs already print per-memory relevance scores and
  success flags (`cmd/worker/main.go logHindsightContext`) — telemetry
  substrate exists, it is unstructured and uncorrelated with outcomes.
- **Audit finding (2026-08-13, operator retro-audit): recall never executes on
  the daemon path.** `queryHindsightForContext` has exactly one call site
  (`executeWithRetry`, the legacy exec path); daemon-spawned workers run the
  RPC turn path (`[Worker:rpc]` throughout production logs), which contains
  zero Hindsight references. Production log evidence: 0 `Querying Hindsight`
  lines ever vs 170 `Hindsight config passed to worker` (env wired, never
  consumed). Retention is equally dead on the daemon path — the only
  `Retained execution memory` lines are from 2026-08-11/12 manual runs, and
  even those show empty `memory_id`. The enrichment code is dead code in
  production; every downstream quality question is blocked on this.
- Live evidence: `/etc/hydra/hydra.yaml` hindsight config: enabled, limit 5,
  max_tokens 800, timeout 10s — knobs exist but were never tuned with data.
- Three memory systems coexist without a federation policy: CASS playbook
  (2,475 rules at measurement time), Hindsight bank (`coding-brain`), hydra
  verdict memory. Duplication across systems is unmeasured.

---

## 3. User Stories

- **US-1:** As an operator, I run one command and get a recall-quality report
  (relevance distribution, utilization rate, outcome correlation), so tuning
  is evidence-based.
- **US-2:** As an operator, I run golden probes against documented incidents
  and get precision@k, so recall regressions are detectable.
- **US-3:** As an operator, I get a bank-hygiene report (dups, stale entries,
  deprecation candidates), so memory quality does not silently decay.

---

## 4. Functional Requirements

### FR-000: Wire recall and retention into the RPC execution path

**Type:** fr
**Priority:** P0
**Target Files:** `cmd/worker/main.go`, `cmd/worker/pi_rpc_turn.go`, `cmd/worker/rpc_enrichment_test.go`
**Test Command:** `go test ./cmd/worker/ -run TestRPCEnrichment`
**Build Command:** `go build ./...`
**Reads From:** `internal/agents.ContextEnrichment` (existing), `internal/hindsight.Client` (existing)
**Depends On:** —

**Acceptance Criteria:**
- Daemon-spawned workers (RPC turn path) call `queryHindsightForContext` before the first pi turn; the production log gains `Querying Hindsight for context` lines (today: zero)
- Execution retention (`recordExecutionResultSync` or equivalent) fires on the RPC path at bead completion with a non-empty `memory_id` logged
- Hindsight API unreachable = fail-open: bead execution proceeds unenriched, one WARN log
- The legacy `executeWithRetry` enrichment behavior is unchanged
- The typo'd env prefix `HYDRA_HISTORIESIGHT_*` gains correctly-spelled aliases `HYDRA_HINDSIGHT_*` with the old names kept working (deprecation WARN when the old name is used)
- `go test ./cmd/worker/ -run TestRPCEnrichment` exits 0

### FR-001: Structured enrichment telemetry

**Type:** fr
**Priority:** P0
**Target Files:** `cmd/worker/enrichment_telemetry.go`, `cmd/worker/enrichment_telemetry_test.go`
**Test Command:** `go test ./cmd/worker/ -run TestEnrichmentTelemetry`
**Build Command:** `go build ./...`
**Reads From:** `internal/agents.ContextEnrichment` (existing)
**Depends On:** —

**Acceptance Criteria:**
- Every bead execution appends one JSON line to `$HYDRA_HOME/telemetry/enrichment.jsonl` with fields: `bead_id, project, ts, items_count, relevance_scores[], tokens_injected, success_flags[]`
- Hindsight disabled or empty enrichment still writes a record with `items_count: 0`
- Telemetry write failures are fail-open (log and continue; never block bead execution)
- `tokens_injected` is measured from the actual serialized enrichment, not the config cap
- `go test ./cmd/worker/ -run TestEnrichmentTelemetry` exits 0

### FR-002: Enrichment outcome correlation table

**Type:** fr
**Priority:** P0
**Target Files:** `internal/registry/enrichment_outcomes.go`, `internal/registry/enrichment_outcomes_test.go`
**Test Command:** `go test ./internal/registry/ -run TestEnrichmentOutcome`
**Build Command:** `go build ./...`
**Reads From:** FR-001 (telemetry file), `registry.Store` (existing workers table)
**Depends On:** FR-001

**Acceptance Criteria:**
- A joinable view/query correlates enrichment records with worker outcomes: `exit_code, tokens_so_far, completed_at - started_at, retry count`
- Output per cohort (enriched vs not-enriched): count, exit-0 rate, mean tokens, mean wall-clock
- Implemented as a query function `EnrichmentOutcomeReport(dbPath string) (Report, error)` returning a typed struct (not stringly-typed)
- `go test ./internal/registry/ -run TestEnrichmentOutcome` exits 0

### FR-003: Golden-probe precision harness

**Type:** fr
**Priority:** P1
**Target Files:** `internal/hindsight/golden_probe.go`, `internal/hindsight/golden_probe_test.go`, `testdata/recall_golden_set.json`
**Test Command:** `go test ./internal/hindsight/ -run TestGoldenProbe`
**Build Command:** `go build ./...`
**Reads From:** `internal/hindsight.Client` (existing recall path)
**Depends On:** —

**Acceptance Criteria:**
- `testdata/recall_golden_set.json` holds N≥20 entries: `{incident_query, expected_memory_substring, documented_source}` drawn from docs/operations handoffs and CHANGELOG entries
- `RunGoldenProbes(client, goldenSet) ProbeReport` returns precision@1 and precision@5 per query plus aggregate
- Harness runs read-only against the live Hindsight API; API unreachable = test skips (fail-open for CI, explicit error in report mode)
- `go test ./internal/hindsight/ -run TestGoldenProbe` exits 0

### FR-004: Bank hygiene scanner

**Type:** fr
**Priority:** P1
**Target Files:** `internal/hindsight/bank_hygiene.go`, `internal/hindsight/bank_hygiene_test.go`
**Test Command:** `go test ./internal/hindsight/ -run TestBankHygiene`
**Build Command:** `go build ./...`
**Reads From:** `internal/hindsight.Client`
**Depends On:** —

**Acceptance Criteria:**
- `ScanBank(client) HygieneReport` returns: total memories, exact-dup count, near-dup count (normalized-text Levenshtein ≤ 10), age distribution buckets, entries referencing known-superseded states (matched against a provided superseded-pattern list)
- CASS playbook scanned via `cm ls` JSON output with the same report shape
- Report lists deprecation candidates (dup cluster keeps newest, flags rest) without mutating anything (read-only)
- `go test ./internal/hindsight/ -run TestBankHygiene` exits 0

### FR-005: Recall-quality report command

**Type:** fr
**Priority:** P1
**Target Files:** `cmd/hydra/main.go` (subcommand registration only), `internal/cli/recall_quality.go`, `internal/cli/recall_quality_test.go`
**Test Command:** `go test ./internal/cli/ -run TestRecallQuality`
**Build Command:** `go build ./...`
**Reads From:** FR-002, FR-003, FR-004
**Depends On:** FR-002, FR-003, FR-004
**Consumes:** `EnrichmentOutcomeReport(dbPath) (Report, error)`, `RunGoldenProbes(client, set) ProbeReport`, `ScanBank(client) HygieneReport`

**Acceptance Criteria:**
- `hydra recall-quality` prints one consolidated report: outcome correlation (FR-002), golden precision (FR-003), bank hygiene (FR-004)
- Exit code 0 always (report tool; degraded sections marked `UNAVAILABLE` with the exact error, fail-open per section)
- `hydra recall-quality --json` emits machine-readable output
- `go test ./internal/cli/ -run TestRecallQuality` exits 0

---

## 5. Non-Functional Requirements

### NFR-001: Tuning proposal gate

**Type:** nfr-verification
**Verifies:** FR-001..FR-005
**Acceptance Criteria:**
- A tuning change proposal exists as `docs/operations/recall-tuning-<date>.md` citing report numbers (recall limit, max_tokens, retain filters, recency decay)
- Each proposed knob change carries the measured before-value and the expected token delta
- No tuning knob changes land in the same PR as the measurement tooling

---

## 6. Implementation Map

```
FR-000 (wire recall/retain into RPC path — blocker, nothing else works without it)
FR-001 (telemetry) ──► FR-002 (correlation) ─┐
FR-003 (golden probes) ──────────────────────┼──► FR-005 (report command)
FR-004 (bank hygiene) ───────────────────────┘         │
                                                        ▼
                                              NFR-001 (tuning proposal)
```

P0: FR-000, FR-001, FR-002. P1: FR-003, FR-004, FR-005. NFR-001 last.
FR-000 is the hard prerequisite: without it every measurement reads a dead
path.
The operator-run log retro-audit (existing unstructured logs) may proceed
immediately without any FR — it informs the golden set for FR-003.

---

## 7. Out of Scope

- Replacing or merging the three memory systems (federation policy is a
  follow-up decision informed by this PRD's measurements)
- Changing recall limits/retain behavior before NFR-001's proposal
- Worker prompt template changes
