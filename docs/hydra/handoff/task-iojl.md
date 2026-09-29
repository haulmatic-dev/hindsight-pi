# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-triage-iojl-shared-wedge.md: triage report — merge-tree evidence, per-bead supersession, disposition (abandon-with-summary ×3, no rebase, no park clear, no code change).
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.

## Verification (commands run + pass/fail)
- `git fetch origin development` + `git merge-tree --write-tree origin/development hydra/wt-hydra-{d778,9ydi,014y}` — all three CONFLICT, one file each (system-loop.md / task-242b.md / service_starter.go).
- `git rev-list --count origin/development..hydra/wt-hydra-9ydi` = 2, docs-only commits (242b triage handoff + disposition).
- `git grep integration_retest origin/development -- internal/core/config.go` — present (integration_retest.go + config knob landed) → pre-exhaustion rebase retry (`publishWithRebaseRetry`) and retest flow already on development → no code change required.
- `br show hydra-d778 / 9ydi / 014y / 242b` — labels + s402 triage comment confirmed; 9ydi branch's own handoff records identical supersession disposition for 242b.
- Pipeline health: `git log origin/development` shows recent PR landings (PR #340, wt-hydra-3p41 integration).

## Open edges for the next phase
- Human/Pan closure: abandon-with-summary on hydra-d778, hydra-9ydi, hydra-014y with supersession note (retry/clear-park is contraindicated — labels re-appeared post-s402 because supersession is the correct verdict).
- If supersession closures loop, candidate Pan verb `abandon-superseded` (dedupe vs landed feature signature) — not implemented here.
- `overseer.integration_retest` is opt-in (default disabled); operators wanting automatic conflict recheck must enable it.
