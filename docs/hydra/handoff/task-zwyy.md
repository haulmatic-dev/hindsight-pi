# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-triage-d778-014y.md: triage report, revised — pipeline healthy; both branches are SUPERSEDED iterations of already-landed work; disposition = abandon-with-summary both beads, do not rebase, do not clear parks.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage report (revised disposition).

## Verification (commands run + pass/fail)
- `git merge-tree --write-tree origin/development hydra/wt-hydra-d778` — exit 1, conflict only in docs/architecture/system-loop.md (FR-002 and FR-003 rewrote the same mermaid line).
- `git merge-tree --write-tree origin/development hydra/wt-hydra-014y` — exit 1, conflict only in internal/core/service_starter.go.
- `git diff origin/development hydra/wt-hydra-014y -- internal/core/service_starter.go` — shows 014y DELETING origin's newer escalationTTL / ResolveIntegrationBase / clear-escalation / triageSvc code (-62/+5) → 014y is an older iteration, rebasing would revert newer work.
- `git log --oneline origin/development -- internal/core/spawn_exclusivity.go` — empty; `git grep exclusive_paths origin/development` — no hits → d778's FR-002 never landed.
- `git show 84bffe1 --stat` — FR-003 target_overlap.go + scheduler wiring landed 2026-09-29, same problem domain as d778's FR-002 → superseded.
- `git log --oneline origin/development -3` — PR landings present (PR #333, #332, q1lu) → integrate/PR path healthy, no wedge.

## Open edges for the next phase
- Live-store action (outside this worktree, human/Pan-owned): apply abandon-with-summary to hydra-d778 and hydra-014y citing supersession; parks stay (no clear-hard-park, no reset-retries).
- Branch cleanup optional after abandonment: hydra/wt-hydra-d778 and hydra/wt-hydra-014y can be pruned as superseded work.
