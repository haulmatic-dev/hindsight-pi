# GREEN handoff

## What changed (files + one line each)
- .agents/skills/create-prd/SKILL.md: new §3.1a "Task-Pattern Classification (producer contract)" — documents the four-level classification precedence (explicit Type prefix > target-file shape > System Type enum > tdd_chain fallback) and the five-value System Type enum (`backend-api | web-app | cli-tool | library | full-stack`) with their Type-prefix role.
- internal/prdcontract/producer_contract_test.go: RED-phase test file ported from sibling worktree hydra-cxpf (branch hydra/wt-hydra-cxpf, not committed to this branch); two compile fixes: `const skillRelPath = filepath.Join(...)` → `var` (non-constant initializer is a Go compile error: "value of type string is not constant"), and added `return ""` after the loop-terminal `t.Fatalf` (compiler "missing return" — Fatalf is not marked noreturn).
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- RED confirm (before SKILL.md edit): `go test ./internal/prdcontract/` — FAIL (missing term "precedence"/"target-file shape"/"tdd_chain"; positions all -1). PASS (evidence).
- GREEN: `go test -c ./internal/prdcontract/` + direct binary in package dir — all 3 tests PASS (TestSystemTypeEnumDocumented, TestSystemTypePrefixesDocumented, TestClassificationPrecedenceDocumented).
- `go test -cover ./internal/prdcontract/ -count=1` — `coverage: [no statements]` (package is test-only; no non-test Go code, coverage N/A).
- `gofmt -l internal/prdcontract/` — clean. `go vet ./internal/prdcontract/` — clean (VET-OK).
- Commit 62638ed verified via `git show --stat` (3 files, +142).

## Open edges for the next phase
- RED-phase worktree hydra-cxpf may still hold its own copy of the test file; integration should take THIS branch's version (compiling) and not merge the broken `const` variant.
- Note: go test output in this environment is post-processed by a summarizer that hides per-test detail; direct binary run from the package dir was used for verbose evidence.
