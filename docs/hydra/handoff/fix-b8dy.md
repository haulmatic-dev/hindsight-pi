# fix handoff (fix-b8dy, original hydra-sykj8)

## What changed (files + one line each)
- internal/covevidence/lcov.go (new): RED-phase compile-only contract stub — `CoverageMetric{Found, Hit int}` + `Pct()` (0 when Found==0), `FileCoverage{Path, Statements, Branches, Functions, Lines}`, `ParseStats{MalformedLines}`, inert `ParseLcov`/`ParseLcovWithStats`/`Match` (return nil/zero, nil), RED gate flag `covevidenceImplemented=false`. Types pinned so `go vet` passes and the contract tests compile; zero parsing behavior.
- internal/covevidence/gocover.go (new): inert `ParseGoCover(profilePath)` stub (compile-only).
- internal/covevidence/lcov_test.go (new): 10 contract tests gating on `covevidenceImplemented` / `COVEVIDENCE_ENFORCE=1` — 3-SF-block fixture yields correct FN/FNDA→Functions{count(FN),count(FNDA>0)}, BRDA/BRH→Branches{count(BRDA),count(BRH)}, DA/LH/LF→Lines with Statements==Lines asserted; zero-hit file Pct 0; garbage lines (unknown token, malformed DA, truncated SF) skipped + counted (3) with evidence identical to clean parse; duplicate SF merged by DA union (Lines{3,2}, never summed 4/3); Match: exact path, `src/` dir prefix (4 files, nothing outside), `src/**` matches direct AND nested, `src/**/*.ts` matches .ts only, unmatched targets → empty result (absent, not zero-valued).
- internal/covevidence/gocover_test.go (new): 4 contract tests — mode:set 2-file profile yields statement counts (Hit=Σ NumStmt over Count>0, Found=Σ NumStmt; Statements alias Lines); mode:count weights by NumStmt (Hit=4, never 100/104/106); missing profile file → non-nil error + nil map; branches/functions Found=0 so Pct is skippable.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- Original failure diagnosis (verify-first, A1/A2): bead hydra-sykj8's exit 1 was pure worker infra — `Pi rpc turn reached agent_end but the model call FAILED: 400 {"type":"error","error":{"type":"invalid_request_error","code":"1261","message":"[1261][prompt is too long]"}}` ×4. No code defect was ever produced; `internal/covevidence/` did not exist on the base. This pass landed the RED phase per the coverage-truth PRD FR-001 contract.
- `gofmt -l internal/covevidence/` — clean.
- `go vet ./internal/covevidence/` — PASS (clean).
- `go test ./internal/covevidence/ -run 'TestLcov_|TestGoCover_' -count=1 -v` — PASS, exit 0 (`ok ... 0.007s`): all 14 behavior tests SKIP loudly ("RED: covevidence contract not yet implemented"), daemon gate stays green pre-GREEN.
- `COVEVIDENCE_ENFORCE=1 go test ./internal/covevidence/ -count=1` — FAIL for the RIGHT reason: every behavior test fails at `requireImpl` ("RED: covevidence contract not yet implemented (COVEVIDENCE_ENFORCE=1 set)") — proves the RED phase detects the missing contract (26 FAIL/assert lines; GREEN obligation is ≥8 `--- PASS` lines from the same suite).

## Open edges for the next phase (GREEN contract)
- Implement `ParseLcov`/`ParseLcovWithStats` (lcov.go), `ParseGoCover` (gocover.go), `Match`; flip `covevidenceImplemented = true`. Test Command: `go test ./internal/covevidence/ -run 'TestLcov_|TestGoCover_' -count=1 -v` must list ≥8 `--- PASS` and no `[no tests to run]`; `go build ./...` exits 0.
- `ParseStats.MalformedLines` is the RED-invented surface for "skipped with a count" (PRD pins `ParseLcov`'s 2-tuple signature, so the count needed an observable); GREEN may reshape it but must keep the count observable.
- FR-002 (coverage close gate, `internal/core/coverage_gate.go`) depends on this package and on two other PRD chains landing first — sequencing precondition recorded in the PRD.
