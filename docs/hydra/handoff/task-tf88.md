# REFACTOR handoff

## What changed (files + one line each)
- .agents/skills/create-prd/SKILL.md §3.1a: removed duplicated inline list of the five System Type enum values (stated once at level 3, cross-referenced from level 1); tightened prose ("strongest signal wins, weaker signals apply only when no stronger one is present"; override rationale split from drift-warning note). No pinned term or precedence order changed.
- CHANGELOG.md: Unreleased refactor entry.

## Verification (commands run + pass/fail)
- BEFORE (pre-edit): `go test ./internal/prdcontract/ -count=1 -v` — 3/3 PASS (TestSystemTypeEnumDocumented, TestSystemTypePrefixesDocumented, TestClassificationPrecedenceDocumented).
- First edit attempt FAILED the precedence test (capitalized "Target-file shape" broke case-sensitive `strings.Index("target-file shape")` in the pinned test) — reverted to lowercase, tests green again.
- AFTER: `go test ./internal/prdcontract/ -count=1 -v` — 3/3 PASS. Behavior (doc contract) unchanged.
- No benchmarks exist for this functionality (test-only package, no non-test Go code).
- Commit verified via `git show --stat HEAD` (2 files, +18/−18).

## Open edges for the next phase
- Pinned-test note: `internal/prdcontract` precedence test is case-sensitive on the literal "target-file shape" and "prefix" and position-sensitive (LastIndex "System Type" must exceed the shape index) — future §3.1a rewording must preserve lowercase "target-file shape" at level 2 and keep "prefix" mentioned before it.
- Untracked internal/prdcontract/ directory in worktree remains pre-existing (ported in green phase, not part of this bead's diff); commit 62638ed already carried it per green handoff.
