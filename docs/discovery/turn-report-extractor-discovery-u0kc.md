# FR-005 Discovery — Turn distribution extractor + budget calibration report

Bead: hydra-u0kc (discovery, FR-005) · branch hydra/wt-hydra-kzlu · HEAD dcd0d65a
PRD: docs/PRDs/prd-enhancement-pipeline-20261022.md (Target Files `tools/turn-report/{main.go,main_test.go}`)

## Verified current state (this worktree, code inspection + test runs)

- **The RED surface for the extractor is already staged on this branch (untracked, by a
  sibling strand), inert and complete in contract:**
  - `tools/turn-report/main.go` (36 lines): inert RED stub — `turnReportImplemented=false`,
    `eventTypeWorkerTurns="worker_turns"`, `run(args, stdout) int` does nothing, exit 0.
  - `tools/turn-report/main_test.go` (281 lines): full RED contract suite (9 test funcs) —
    percentiles per phase, nearest-rank percentile semantics, budgets comment block
    (round-up-to-10 of p90, emitted as COMMENT), zero-events `insufficient data: N events`
    exit 0, drift warning < 20 samples, `--days` window filters old events, unrelated
    events ignored, window-scoped insufficient-data count. gated by `TURN_REPORT_ENFORCE=1`
    with skip-loud default (established RED pattern: `STALE_FILING_ENFORCE`,
    `MIRROR_HEALTH_ENFORCE`, `BEADS_MIRROR_AUDIT_ENFORCE`, ...).
- **Verified runs (both VERIFIED this session):**
  - `go test ./tools/turn-report/ -count=1` → `ok ... 0.003s` — AC4 met at discovery time
    (RED suite green pre-GREEN, all behavior tests skip loudly).
  - `TURN_REPORT_ENFORCE=1 go test ./tools/turn-report/ -count=1` → FAIL — RED proven.
  - `go vet ./tools/turn-report/` → clean.
- **Test seam discovered:** the suite injects `now` via a `TURN_REPORT_NOW` env (RFC3339,
  set with `t.Setenv`) and calls `run([]string{"--events-dir", dir, "--days", N}, &out)`.
  GREEN must implement run() to honor that seam (deterministic day-window tests).
- **Event shape the extractor consumes** (matches `core.Event`/central-stream JSONL):
  `{"type":"worker_turns","bead":...,"project":...,"fields":{"turns":N,"phase":"..."}}` —
  day-file `<events-dir>/<YYYY-MM-DD>.jsonl`. Turn source is FR-004's close-path
  `worker_turns` event (sibling chain; see docs/discovery/turn-telemetry-fr004-rinq.md
  for its contract). Order of phases in the chain: the extractor may go GREEN before
  telemetry emits any event — that is fine, zero events fail-open is AC2.

## Ambiguities resolved for GREEN

- **Percentile method**: nearest-rank (pinned by `TestTurnReport_PercentileNearestRank` on
  a 1..10 sample; p90 = the 9th value under nearest-rank).
- **Budget suggestion**: `turn_budgets:` yaml block = round-up-to-10 of each phase's p90,
  emitted as COMMENT lines for operator review. The tool never writes any config file —
  the report-only contract is pinned in the package doc and test.
- **Empty window**: print `insufficient data: N events`, exit 0 (fail-open reporting), N =
  events inside the `--days` window only (`TestTurnReport_InsufficientDataCountsOnlyWindowedEvents`).
- **Drift warning**: when a phase's sample size < 20 the per-phase output must carry a
  small-sample/drift warning — pinned by `TestTurnReport_DriftWarningUnderTwentySamples`.
- **Phase grouping key**: the `fields.phase` string as emitted by
  `deriveBeadPhase` (red/green/refactor/syntax/implement) — no re-derivation in the tool.
- **Unrelated event types**: skipped silently (`TestTurnReport_UnrelatedEventsIgnored`).

## Chain blocker to flag (NOT this bead's fix, front-loaded for GREEN/RED peers)

`cmd/worker/turn_telemetry_test.go` (sibling RED file, untracked) imports
`github.com/haulmatic-dev/hydra/internal/types` — **a package that does not exist**
(`rg -rn "^package types"` → 0 hits; typed Bead/Event live in `internal/core`). Verified:
`go test ./cmd/worker/ -count=1` fails `[setup failed] no required module provides package
github.com/haulmatic-dev/hydra/internal/types`. Until the telemetry strand fixes the import
(→ `internal/core`, using `core.BeadID`/the close-path event emitter seam), the cmd/worker
package's test build — including every other test in that package — is broken. The
extractor package itself is unaffected (does not import worker code).

## AC mapping

| AC | Status | Evidence |
|---|---|---|
| 1. CLI reads worker_turns, per-phase percentiles + `turn_budgets:` COMMENT, never writes config | contract pinned, pending GREEN | main_test.go `TestTurnReport_PercentilesPerPhase` / `_BudgetsCommentBlock`; stub inert |
| 2. Zero events → `insufficient data: N events`, exit 0 | contract pinned, pending GREEN | `TestTurnReport_ZeroEventsInsufficientData` |
| 3. Drift warning when phase n < 20 | contract pinned, pending GREEN | `TestTurnReport_DriftWarningUnderTwentySamples` |
| 4. `go test ./tools/turn-report/ -count=1` exits 0 | **MET (RED skip-loud)** | `ok ... 0.003s` VERIFIED |

## GREEN checklist

- flip `turnReportImplemented = true` in `tools/turn-report/main.go`
- implement `run(args, stdout) int`: parse `--events-dir`/`--days`, read
  `<dir>/<YYYY-MM-DD>.jsonl` across the window (respect `TURN_REPORT_NOW` seam), collect
  `fields.turns`/`fields.phase` per `worker_turns` event; nearest-rank percentiles
  p25/p50/p75/p90/max per phase; drift warning per phase with n < 20; zero events →
  `insufficient data: N events`, exit 0; `turn_budgets:` block as COMMENT = round-up-to-10
  of p90; never touch any config file.
- accept `TURN_REPORT_NOW` env for deterministic windows.
- CHANGELOG entry; the suite is already landed — GREEN must not weaken any pinned assertion.
