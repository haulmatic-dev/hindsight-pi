# task handoff (hydra-xg6s)

## What changed (files + one line each)
- docs/discovery/integration-wedge-triage-xg6s.md: triage of the mhk2/xxq5/5ob8 shared integration-exhaustion wedge — no capacity problem, per-bead dispositions, no code change.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.
- No code change (triage bead; the only real code proposal is the unlanded pre-rebase precheck on hydra/wt-hydra-mhk2 — land that strand, don't duplicate).

## Verification (commands run + pass/fail)
- hindsight_search on shared-wedge terms — durable-park-by-design + prune-worktree-registration playbook confirmed. PASS.
- br show hydra-mhk2/xxq5/5ob8 — mhk2 only open bead with integration-exhausted; xxq5/5ob8 meta beads. PASS.
- hydra store br list scan — only mhk2 open with the label. PASS.
- git cat-file -e origin/development:docs/{discovery/integration-wedge-triage-rsyw.md,hydra/handoff/task-rsyw.md} — landed. PASS.
- git show origin/development:docs/discovery/integration-strand-triage-mhk2.md — full human rebase path landed (e93d270, PR #438). PASS.
- git merge-tree --write-tree origin/development hydra/wt-hydra-mhk2 — rc=1, 2 add/add doc conflicts, publisher.go auto-merges. PASS.
- Triage comments added to hydra-xxq5 and hydra-5ob8 (br comment ✓). PASS.

## Open edges for the next phase
- Human-only action: resolve the 2 docs add/add conflicts on hydra/wt-hydra-mhk2 per integration-strand-triage-mhk2.md, rebase, land via dogfood PR → then mhk2 park can clear and the precheck feature + cache-cap-guard.sh integrate.
- Close hydra-xxq5/hydra-5ob8 as cause-fixed (5ob8 after its own chain verdict).
- Tier B agentic conflict resolution remains deliberately unimplemented — separate bead.
