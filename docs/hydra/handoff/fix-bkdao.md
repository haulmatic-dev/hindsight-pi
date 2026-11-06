# fix handoff (fix-bkdao, original hydra-rnbxi REFACTOR lcov extractor)

## What changed (files + one line each)
- internal/covevidence/lcov.go: real parsers replacing the inert RED stub — `ParseLcovWithStats` (single bufio scan; per-record FN/FNDA/BRDA/DA detail sets; LH/LF/FNF/FNH/BRF/BRH recomputed from unioned detail with summary lines as fallback only when a record has no detail; duplicate SF blocks merged by DA union, never summed; malformed lines skipped and counted, never fatal) + `ParseLcov` delegate + `Match` (exact, `dir/` prefix, `**` = zero-or-more-dirs segment glob via filepath.Match per segment; absent, not zero-valued). `covevidenceImplemented=true`.
- internal/covevidence/gocover.go: real `ParseGoCover` — bufio scan over `path:loc numStmt count` blocks, Found=Σ NumStmt all, Hit=Σ NumStmt over Count>0 (raw execution counts never summed), Line/Statements alias, Branches/Functions zero; read error → nil map + error.
- CHANGELOG.md: Added entry under Unreleased (existing skill-induction bullet split out intact).

## Verify-first finding
- Original bead hydra-rnbxi's exit-1 was worker infra (compaction/summarization hit the GLM token cap; activity timeout), not a code defect — but the REFACTOR target was itself incomplete: the GREEN strand for the package (hydra-t1jfh) died the same way, leaving `covevidenceImplemented=false`, inert stubs, 0% coverage, all contract tests skipping. So this bead lands the implementation (GREEN) with an optimized single-pass design (REFACTOR).

## Verification (commands run + pass/fail)
- `go test ./internal/covevidence/ -count=1 -v` — PASS, 14/14 (GoCover ×4, Lcov ×10 incl. malformed skip+count, dup-SF DA-union, all 4 Match semantics); plain + `COVEVIDENCE_ENFORCE=1` (same result, no skips).
- `-cover`: 76.3% of statements.
- `go build ./...` — PASS. `gofmt -l internal/covevidence/` — clean.
- Commit ad7c50ce verified via `git show --stat` (3 files, +488/−31).
- hindsight_search at bead start — context-mode hook unavailable (NODE_TOO_OLD); skip rule applied.

## Open edges for the next phase
- Integration bead (SYNTAX/closing) should add a consumer wiring check if the coverage-truth PRD requires daemon-side adoption; this bead delivered the package contract only.
- Match's `**` matcher is recursive split-based; fine for target-file size (tens of files); no benchmark exists.
