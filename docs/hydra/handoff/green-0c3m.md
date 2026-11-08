# green handoff (hydra-0c3m)

## What changed (files + one line each)
- .agents/skills/create-prd/SKILL.md: new §3.1a "Execution-Type Producer Contract" — five-value System Type enum (library/cli/api/application/ops), five chain prefixes (RED:/GREEN:/REFACTOR:/SYNTAX:/FIX:), strict classification precedence (prefix > target-file shape > System Type > tdd_chain); `**Type:**` values noted as structural, not execution types.
- tools/prd-lint/main.go: rule R9 — invalid `**System Type:**` first token is a lint violation (enum-tolerant of parenthetical qualifiers); helper `systemType` + `isValidSystemType`.
- tools/prd-lint/main_test.go: `TestCreatePrdProducerContract` (doc pins enum, prefixes, precedence order) + `TestSystemTypeLint` (detection, qualifier tolerance, validity matrix).
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `cd tools/prd-lint && go test ./ -count=1` — PASS (ok, 0.337s, includes the 2 new tests).
- `gofmt -l tools/prd-lint/*.go` — clean.
- Contract test asserts all three parent criteria against SKILL.md verbatim.

## Open edges for the next phase
- RED bead hydra-2rqv never produced test files; the two tests added here close that gap for this chain — the REFACTOR bead (hydra-tf88) can move them or extend coverage.
- Consumers (task-breakdown analyzer) do not yet read `**System Type:**`; only prd-lint validates it. Wiring layer-3 classification into analyzer.mjs is a separate bead.
