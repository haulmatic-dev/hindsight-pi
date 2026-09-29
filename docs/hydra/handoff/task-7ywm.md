# discovery handoff

## What changed (files + one line each)
- docs/hydra/handoff/task-7ywm.md: this handoff (ops-triage bead; no code change).
- CHANGELOG.md: Documentation entry recording PR #389 merge verification + disk cleanup.

## Verification (commands run + pass/fail)
- `git log -1 origin/development` → `82c0f6a Merge pull request #389 from haulmatic-dev/dogfood/publish-hydra` — PR #389 already MERGED by human; publisher wedge resolved, locally-merged work is published on development. PASS.
- `git rev-list --count origin/development..origin/dogfood/publish-hydra` = 1 (the merge commit itself; branch tip d6c5752 already on development via PR). PASS.
- Disk on /: was 99% at bead start, first measurement this run 87% (57G free) — tmpfiles cleanup already effective before/without sudo; after hydra-owned cleanup 86% (65G free). Freed: `$HYDRA_HOME/sessions` files >14d (951M→269M), `$HYDRA_HOME/logs` files >14d (260M→57M). PASS.
- `sudo -n true` → "sudo: a password is required" — daemon runs as uid 999(hydra), no root. System-wide cache caps (journald, apt cache) NOT DONE — require human sudo:
  `sudo journalctl --vacuum-size=500M && sudo apt-get clean && sudo sed -i 's/#SystemMaxUse=.*/SystemMaxUse=500M/' /etc/systemd/journald.conf && sudo systemctl restart systemd-journald`
- br WAL poison (`database is busy (recovery in progress)`) RESOLVED this run: `br doctor migrate-schema recover` succeeded on retry (recovery family retained at .beads/.br_recovery/schema-migrations/20260929T172032*), `br list` now returns beads (hydra-w2s, hydra-4md). PASS (fixed).
- `git fetch`/`git worktree prune`/`git gc` in main checkout fail: `error: cannot lock ref 'worktrees/hydra-deploy-263/HEAD': ... HEAD.lock: Permission denied`. Verified cause: /opt/dev-tools/.git/modules/components/hydra/worktrees/hydra-deploy-263/ is root-owned (755, files root:root, dated Sep 21) while sibling worktree dirs are hydra-owned — hydra user cannot remove it; `git worktree prune` also fails for the same reason. OPERATOR-ONLY (requires root): `sudo rm -rf /opt/dev-tools/.git/modules/components/hydra/worktrees/hydra-deploy-263 && sudo -u hydra git -C /opt/dev-tools/components/hydra worktree prune`. Not fixable by daemon — recorded as the sole open operator action.

## Open edges for the next phase
- Human: optional sudo cleanup to cap system caches (commands above) — disk currently healthy at 86%/65G free.
- Operator-only: remove root-owned stale worktree dir `hydra-deploy-263` (blocks git gc/fetch reflog; commands above). Sole sudo action remaining.
- br WAL poison recurring under daemon concurrency; consider serialized store access or recovery-on-start hook (recovered this run via `br doctor migrate-schema recover` retry).
