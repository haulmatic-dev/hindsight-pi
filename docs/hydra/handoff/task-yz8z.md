# task handoff (hydra-yz8z)

## What changed (files + one line each)
- docs/discovery/orphan-claim-triage-yz8z.md: full triage of the orphanclaims=2 anomaly — the 2 claims (hydra-vk94, hydra-1p1q) identified, both self-resolved; reconciliation + Charon recovery verified live; disk claim stale (43%).
- CHANGELOG.md: Unreleased entry pointing at the triage.
- No code change (triage/discovery bead).

## Verification (commands run + pass/fail)
- `df -h /` → 185G/451G = 43%, below 85% watermark. PASS (stale claim).
- Registry SQLite read (`/opt/dev-tools/var/hydra/.hydra/registry.db`): at 02:4x hydra-vk94 + hydra-1p1q workers state=completed with beads in_progress (the 2 orphan claims); all other in_progress beads had live `working` rows. PASS.
- Events 2026-10-02.jsonl: `charon_orphan_reopened` hydra-vk94 02:47:25; `reconcileOrphanClaim` symbol present in deployed `/opt/dev-tools/var/hydra/bin/hydra` (strings hit) and on origin/development (bb75c4a ancestor check PASS).
- `br show hydra-vk94` / `hydra-1p1q` — both CLOSED (completed) by 03:0x → no bead remains wedged. PASS.
- `hydra-l867` (in_progress at triage time): worker PID 3896762 alive, heartbeat fresh, child pi session actively writing → NOT orphaned; its build-gate failure (`error obtaining VCS status: exit status 128` ×10, 0.28s) reproduced clean on 2 re-runs → transient git/buildvcs race with the still-running worker session.
- Publish identity probe: nipuna.j PAT → `/repos/haulmatic-dev/hydra` 404, `/repos/haulmatic-devteams/hydra` 200; `gh repo view`/`gh pr list` in the vk94 worktree resolve devteams — the `haulmatic-dev/hydra` slug in 24 integration failures today comes from inside `gh pr create` GraphQL, not repo resolution (recurrence of m372 class).

## Open edges for the next phase
- Clean-exit gap: `reconcileOrphanClaim` iterates retrySnapshot only — a worker exiting `completed` without closing its bead gets no retry entry and is invisible to the scheduler reconcile (only Charon's 5m-grace sweep reclaims). Code-bead candidate.
- Daemon build gate should run after worker process exit or use `-buildvcs=false`: gate racing the worker's own git activity yields transient `error obtaining VCS status: exit status 128` and burns a verified-close round. Code-bead candidate.
- `gh pr create` resolving pre-rename slug `haulmatic-dev/hydra` despite correct remote/token: needs focused repro; `GH_REPO` pin on publisher gh calls would moot it.

HYDRA_VERDICT follows in session.
