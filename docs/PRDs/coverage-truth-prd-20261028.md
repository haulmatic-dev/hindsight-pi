# PRD: Coverage Truth — Per-FR Coverage Evidence at Close

**Version:** 1.0 · **Date:** 2026-10-28 · **System Type:** library · **Repo:** components/hydra

## Problem

Coverage criteria in PRDs are prose hydra cannot check. The srv-data-harbour
rollout (2026-10) planned per-slice "≥80 on the slice's files" criteria, but the
machine gate ran only the repo-global `test:cov` exit code against a low global
floor — enforcement existed nowhere until the terminal FR flipped per-path
ratchets, which then found 584 per-file breaches (223 files) at once. Every
slice closed green while `services/` sat at 43% coverage next to `dto/` at 95%.
PRD-6a now proves each bead's *tests exist* in its evidence; nothing yet
measures *what those tests cover* at bead close.

Verified facts (2026-10-22/23, srv-data-harbour @ c9f6364f): jest `--coverage`
writes `coverage/lcov.info` as part of `test:cov`; the lcov format carries
per-file statement/branch/function/line hit data (NF/NH, BRDA, DA records);
Go emits a convertible profile via `go test -coverprofile`. Worker verification
commands already produce these artifacts inside the chain worktree at close
time. The close path resolves an evidence root per bead
(`BeadCloser.resolveEvidenceRepo`, `bead_closer.go` ~1457).

## Goals

- At close, hydra measures coverage on the bead's declared Target Files from
  the coverage report the verification command already produced — no extra
  test runs.
- The measurement is recorded, evented, and (when enabled) enforced per file,
  never as an aggregate.
- Ships dark (shadow/off) — zero behavior change until an operator turns it on.

### FR-001: lcov evidence extractor package

**Type:** fr
**Priority:** P1
**System Type:** library
**Target Files:** `internal/covevidence/lcov.go`, `internal/covevidence/lcov_test.go`, `internal/covevidence/gocover.go`, `internal/covevidence/gocover_test.go`
**Test Command:** `go test ./internal/covevidence/ -run 'TestLcov_|TestGoCover_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** none

`internal/covevidence` (standard library only): `FileCoverage{Path string;
Statements, Branches, Functions, Lines CoverageMetric}` with
`CoverageMetric{Found, Hit int; Pct() float64}`. `ParseLcov(r io.Reader) (map[string]FileCoverage, error)`
parses SF/FN/FNDA/FNF/BRDA/BRF/BRH/DA/LH/LF records; malformed lines are
skipped with a count, never fatal; duplicate SF blocks merge by unioning the
per-line DA map and recomputing LH/LF from it (never sum LH/LF across blocks).
Functions: Found=count(FN), Hit=count(FNDA>0). Branches: Found=count(BRDA),
Hit=count(BRH). Statements alias Lines (DA/LH/LF) — Statements and Lines
report identical numbers by definition.
`ParseGoCover(profilePath string) (map[string]FileCoverage, error)` converts a
`go test -coverprofile` file (mode: set/atomic/count) to the same shape:
statements Hit = Σ NumStmt over blocks with Count>0, Found = Σ NumStmt over
all blocks (never sum raw execution counts); branches/functions report
Found=0 so Pct is skipped by callers when Found==0. `Match(targets []string, files map[string]FileCoverage) map[string]FileCoverage`
matches targets as exact paths, `dir/` prefixes, or `dir/**`(/glob) patterns:
strip the `dir/**` prefix, accept every file under `dir` whose remaining path
matches the remainder pattern segment-by-segment via `filepath.Match` (`**` =
zero or more directories; `src/**` matches both `src/a.ts` and `src/x/a.ts`). Test functions are named `TestLcov_*` and
`TestGoCover_*`; fixtures embed real lcov excerpts (≥3 files, one with zero
hits) and a real Go cover profile. Update `CHANGELOG.md` (Added).

**Acceptance Criteria:**
- `TestLcov_*`: a fixture with 3 SF blocks yields correct FN/FNDA/BRDA/BRH/
  LH/LF per file (Statements == Lines asserted); a zero-hit file yields Pct 0;
  a truncated/garbage line is skipped and counted; duplicate SF merges via DA
  union without double-counting covered lines.
- `TestGoCover_*`: mode:set profile with 2 files yields statement counts;
  mode:count weights blocks by NumStmt where Count>0; a missing file returns
  an error.
- `Match`: exact path, trailing-slash directory, `src/**` matches `src/a.ts` AND
  `src/x/a.ts`, and `src/**/*.ts` matches nested .ts only; unmatched targets are absent, not zero-valued.
- Test Command output lists ≥8 `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

### FR-002: Coverage-delta close gate (shadow-first)

**Type:** fr
**Priority:** P1
**System Type:** library
**Target Files:** `internal/core/coverage_gate.go`, `internal/core/coverage_gate_test.go`, `internal/core/config.go`, `internal/core/bead_closer_hard_gates.go`
**Test Command:** `go test ./internal/core/ -run 'TestCoverageGate_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** FR-001 (imports `internal/covevidence`). Sequencing precondition: the validator-truth PRD's shadow-mode chain (`config.go`) and the close-path-truth PRD's infra-classification chain (`bead_closer_hard_gates.go`) must land first — this requirement's one call line in `runCloseHardGates` and its config block wait for both (see docs/PRDs/validator-truth-prd-20261022.md and docs/PRDs/close-path-truth-prd-20261023.md).

Add `coverage_gate: {enabled: bool (default false), mode: shadow|enforce
(default shadow), report_paths: [coverage/lcov.info], min_pct: {statements: 80,
branches: 80, functions: 80, lines: 80}}` to config (yaml +
`DefaultConfig`). Add `checkCoverageGate(bead types.Bead, evidenceRoot string) CoverageResult`
in a new file: resolve the bead's target files via `beadfiles.Resolve` (PRD-6a
FR-001, landed); find the first existing report under `evidenceRoot` from
`report_paths`; parse with covevidence; compute per-file Pct for each metric
where Found>0; result carries per-file per-metric numbers and an overall
`MeetsBar`. No report file or no matched files → `CoverageResult{Ran:false}`
(fail-open, logged once per bead) — EXCEPT in enforce mode when the bead HAS
declared targets and the report file is missing: then emit
`coverage_report_missing`, and after 2 consecutive such closes treat it as a
work-class rejection (`coverage-below-threshold`, evidence `report missing`).
Staleness guard: only accept a report whose mtime is ≥ the bead's
verification-command execution start; older reports count as missing. Wire into `runCloseHardGates`
(`bead_closer_hard_gates.go` — one call line only, added by this FR; use the
`worktreePath` param the function already receives — do NOT re-resolve via
`resolveEvidenceRepo`):
- `enabled=false` → skipped.
- `shadow` → emit `coverage_delta` (fields: bead, per-file metrics, meets_bar)
  and never reject.
- `enforce` → emit the event and reject with reason
  `coverage-below-threshold` naming the worst file+metric only when at least
  one matched file with Found>0 is below `min_pct`; this rejection goes through
  the normal `rejectViaGate` path (a work-class failure — it consumes attempts
  like any gate rejection). Beads with no declared target files are skipped.
Update the event table in `docs/architecture/shepherd.md`, the gate section of
that doc, and `CHANGELOG.md` (Added).

**Acceptance Criteria:**
- Unit tests with temp evidence roots + a fixture lcov: target file above bar
  → meets_bar true; one file below on branches only → false with that file
  named; missing report → Ran:false, no error surfaced; no targets → skipped.
- Config test: defaults parse (disabled, shadow, the 4×80 floor);
  `min_pct` overrides apply.
- Shadow mode never returns a rejection; enforce returns a rejection whose
  reason is exactly `coverage-below-threshold`.
- Test Command output lists `--- PASS` lines and no `[no tests to run]`;
  `go build ./...` exits 0.

### FR-003: Coverage measurements in decision packets and events

**Type:** fr
**Priority:** P2
**System Type:** library
**Target Files:** `internal/core/coverage_packet.go`, `internal/core/coverage_packet_test.go`
**Test Command:** `go test ./internal/core/ -run 'TestCoveragePacket_' -count=1 -v`
**Build Command:** `go build ./...`
**Depends On:** FR-002 (imports its `CoverageResult`; no locally-defined shapes)

`summarizeCoverage(result) map[string]any` producing the packet payload: worst
file per metric, count below bar, total matched files, ran flag. When PRD-6b's
`decision_request` event (close-path-truth PRD, spec-infeasible decision
packets) is present on the branch, `decisionPacketCheck` includes this summary
in its `measurements` field via a small adapter func defined here
(`var coverageSummary func(beadID string) map[string]any`, nil-safe). Until
then the adapter stays nil and nothing calls it. The `coverage_summary` event
itself is emitted by FR-002's gate alongside `coverage_delta` when `Ran:true`;
this FR only provides `summarizeCoverage` and the adapter. Update `docs/architecture/shepherd.md` (event) and `CHANGELOG.md`
(Added). Test functions are named `TestCoveragePacket_*`.

**Acceptance Criteria:**
- Tests: a mixed result yields worst-file-per-metric, below-bar count, matched
  count; `Ran:false` yields a summary that says so; nil adapter is never
  called and never panics.
- `go build ./...` exits 0; Test Command lists `--- PASS` lines and no
  `[no tests to run]`.

## Success Metrics

- Shadow mode on hydra's own repo: every closed chain bead with declared
  targets emits one `coverage_delta` with ≥1 matched file.
- Zero behavior change with default config (gate disabled) — full
  `go test ./...` green.

## Out of Scope

- Turning the gate on for any product repo (operator decision after shadow
  data).
- Density-tiered thresholds per file class (the doctrine lives in the
  create-PRD skill; hydra enforces one floor per config).
- Running coverage commands that the verification command did not already run.
