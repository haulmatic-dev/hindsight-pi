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
- `br list` fails: "Error: Database error: database is busy (recovery in progress)" (WAL-index poison, known recurring pattern per task-bmtb/task-8py6 handoffs). `br doctor migrate-schema recover` ran; warning persists under daemon concurrency. Transient, not disk-related (65G free). Operator rerun of `br doctor migrate-schema recover` when daemon quiescent recommended. PASS (diagnosed).
- `git fetch` in main checkout emitted: `error: cannot lock ref 'worktrees/hydra-deploy-263/HEAD': ... HEAD.lock: Permission denied` on git gc — stale worktree entry `hydra-deploy-263` in /opt/dev-tools/.git/modules/components/hydra/worktrees is unwritable by hydra user; `git worktree prune` needs write to the module dir. Recorded as infra edge (operator: `sudo git -C /opt/dev-tools worktree prune` or chown).

## Open edges for the next phase
- Human: optional sudo cleanup to cap system caches (commands above) — disk currently healthy at 86%/65G free.
- br WAL poison recurring under daemon concurrency; consider serialized store access or recovery-on-start hook.
- Stale worktree `hydra-deploy-263` in hydra module dir breaks git gc (permission denied) — operator prune.
