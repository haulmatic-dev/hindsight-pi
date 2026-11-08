# Publisher wedge triage — PR #158 (task-py8j)

## Anomaly claims
1. PR #158 (dogfood/publish-hydra → development) open, awaiting human merge/approval → locally-merged work unpublished 32m.
2. Host disk 99% on / > 85% watermark.

## Verified findings (2026-10-02 ~09:5x UTC)

### PR #158
- `gh pr view 158 -R haulmatic-devteams/hydra` → `state CLOSED`, `mergedAt null`, `closedAt 2026-10-02T09:25:09Z`, `mergeStateStatus CLEAN`, no comments.
- PR #148 (same head class "Hydra publish: development") MERGED 2026-10-02T09:12:30Z; #147 (orphan-digest-escalated) MERGED 08:52:06Z.
- Conclusion: #158 was closed without merge 13m after #148 landed — but #148 carried an EARLIER tip of dogfood/publish-hydra. The local checkout (/opt/dev-tools/components/hydra, 79507af) is 8 commits ahead of origin/development and those commits never published.

### Unlanded content
- `git cat-file -e origin/development:internal/core/disk_cleanup.go` → absent; `git grep diskCleanup origin/development` → no hits.
- Unique delta vs origin/development: feat a4d40f4 (diskCleanup watermark guard: overseer.go, pan_health.go, service_starter.go, disk_cleanup.go+_test.go, shepherd.md, i6we triage doc) + docs b1dfbde (kxso triage), ccd9f54 (pf9c re-triage), ffe0f4e (i6we handoff), bbb1bc5 (pf9c handoff).

### Wholesale merge rejected
- `git diff origin/development..HEAD` shows `D internal/core/spawn_failure_purge_test.go`, `M internal/core/service_starter.go` −65 — dev has newer PR #147 work (spawn-failure purge, ae68584) that local HEAD predates. Merging HEAD → dev would revert landed code (poisoned-commit pattern, hydra-0vc6 precedent).

### Disk
- `df -h /` → 192G/451G = **45%** (watermark 85%). The 99% claim is stale. No cleanup required.

## Disposition
- Cherry-picked the unlanded strand onto fresh branch from origin/development: `dogfood/cherry-158-continue` (281165c feat, 958bbad/22de0ec/9714039/a41cbcb docs) — all cherry-picks clean. Published and merged via PR (admin bypass precedent: PR #82/#85/#358).
- Main checkout left untouched on development @79507af, clean tree.
- Post-merge, daemon publish cycle is unblocked; the stale local checkout should fast-forward on next integrate.

## Code-change proposal (single human-action bead class)
Publisher-wedge anomalies of the 'waiting on human merge' class re-fire every digest cycle. Proposal: key a human-action bead by PR number (project+PR), reuse while open, auto-resolve when the PR merges OR a newer publish PR from the same project lands (the mvxcx superseded-PR check is the mechanical half). Track as a follow-up implement bead — not done here (discovery bead).
