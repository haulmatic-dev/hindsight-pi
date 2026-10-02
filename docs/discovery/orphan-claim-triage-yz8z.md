# Triage — P0 campaign anomaly: orphaned in_progress claims wedging the hydra DAG ladder (hydra-yz8z)

Date: 2026-10-02 ~03:15 UTC. Triage-only bead: no code change. Every claim below verified with live commands/queries.

## TL;DR

- The digest snapshot (orphanclaims=2, inprog=6) is **transient, not a permanent wedge**. The 2 orphaned claims were **hydra-vk94** and **hydra-1p1q** (workers had completed in the registry but beads were still `in_progress`). Both beads **closed on their own** by 03:0x as their retry cycles finished.
- **Reconciliation runs and is verified live**: `charon_orphan_reopened` fired for hydra-vk94 at 02:47:25 ("reopened orphan in_progress bead hydra-vk94 (no live worker)"), the bead re-ran gates and closed at 03:0x. `reconcileOrphanClaim` (bb75c4a, PR #59) is present in the deployed daemon binary (`strings` hit on `/opt/dev-tools/var/hydra/bin/hydra`) and on origin/development.
- The digest's **disk claim is stale**: `df -h /` = 185G/451G = **43%**, below the 85% watermark. No systemd-tmpfiles action needed (confirmed independently by task-mvcx and the 1p1q ops triage earlier the same day).
- Residual real findings worth a code bead (below): clean-exit-without-close beads are invisible to `reconcileOrphanClaim` (needs a retry entry), and gate `go build` can fail transiently with `error obtaining VCS status: exit status 128` when the gate races the worker's own git activity.

## The 2 orphaned claims, identified

Registry (`/opt/dev-tools/var/hydra/.hydra/registry.db`) at 02:4x showed for project `hydra`:

- `hydra-vk94` worker state=**completed** (exit 0) at 02:49, bead still `in_progress` → orphaned claim
- `hydra-1p1q` worker state=**completed**, bead still `in_progress` → orphaned claim
- the other in_progress beads (b5p2, ctix, qmf7, t2e1, yz8z, l867) had live `working` rows → not orphans

Both orphaned beads closed 03:00–03:05 (`br show`: hydra-vk94 CLOSED completed, hydra-1p1q CLOSED completed). **No bead remains wedged.**

## Why workers died without releasing their claims

Not a registry/session desync in the claim path — the desync is upstream, in the close path:

1. Worker delivers a verdict; the daemon's verified-close runs gates **concurrently with the worker still finishing its turn**.
2. `hydra-vk94`: gates passed, but close integration failed — `gh pr create` hit `GraphQL: Could not resolve to a Repository with the name 'haulmatic-dev/hydra'` (24 occurrences today, beads ns8y/1p1q/vk94). The failure mode is the known org-rename flap (docs/discovery/publisher-wedge-triage-m372.md): the publish identity resolved to `nipuna.j`'s owner PAT. Verified with that PAT: `/repos/haulmatic-dev/hydra` → **404**, `/repos/haulmatic-devteams/hydra` → **200**, and `gh repo view`/`gh pr list` inside the vk94 worktree resolve to `haulmatic-devteams/hydra` with the same token — so the stale slug is produced somewhere inside the `gh pr create` GraphQL call, not by repo resolution. Bead reopens, in_progress, worker already gone → orphan claim until Charon sweeps.
3. `hydra-1p1q`: same shape (`gate-failed-build` + the same `haulmatic-dev/hydra` integration error).
4. `hydra-l867` (still in_progress at triage time): **NOT orphaned** — its worker PID 3896762 is alive, heartbeating (row heartbeat 03:11:38), and its child pi session is actively writing. Its build gate failed at 02:45:22 in 0.28s with `error obtaining VCS status: exit status 128` ×10 — git exit 128 inside buildvcs. The worktree builds clean when re-run (verified twice), all files and git dirs are hydra-owned, no index.lock reproduced the failure. Best explanation: the gate raced the worker's own git activity (worker committed at 02:44, gate ran at 02:45 while the worker session was still alive). The gate ran while the worker was still executing — by design verified-close does not wait for process exit.

## Mechanical fixes confirmed working

- `reconcileOrphanClaim` (scheduler, requires a retry-manager entry): deployed; no trigger today because today's orphans were clean-exit beads with no retry entries (see open edge 1).
- Charon `recoverOrphanInProgressBeads`: fired at 02:47:25 for vk94, `charon_orphan_reopened` event present; recovery → re-gates → close.
- Pan filed 7 duplicate anomaly beads (yz8z, t2e1, qmf7, ctix, b5p2, l867, vk94, wv2e…) for the same transient snapshot — all duplicates of this triage.

## Open edges for a code bead

1. **Clean-exit gap**: a worker that exits `completed` without closing its bead (close rejected or crash-after-verdict) leaves the bead in_progress with NO retry-manager entry, so `reconcileOrphanClaim` (which iterates `retrySnapshot`) never sees it. Only Charon's sweep (5m grace + heartbeat staleness) reclaims it. Consider a scheduler-side reconcile keyed on `in_progress` + registry row `completed`, no retry entry.
2. **Transient buildvcs gate failure**: daemon build gate runs `go build ./...` in the chain worktree while the worker process may still be executing git commands → occasional `error obtaining VCS status: exit status 128`, burning a verified-close round. Candidate fixes: `-buildvcs=false` in the daemon build gate spec, or gate after worker process exit.
3. **`haulmatic-dev/hydra` slug inside `gh pr create`**: repo resolution is correct (remote + `gh repo view` = devteams) yet create fails with the pre-rename org. Needs a focused repro bead; a `GH_REPO=haulmatic-devteams/hydra` pin on the publisher's gh invocations would make it moot.

## Live actions

None required: no wedged bead remains; disk below watermark; no park to clear. (A parallel strand, PR #60 / commit 09adab2, already recorded the same self-resolution for the 1p1q alarm.)
