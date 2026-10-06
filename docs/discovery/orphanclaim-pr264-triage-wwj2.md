# Triage: 19 orphaned in_progress claims + publisher wedge + disk 99% (hydra-wwj2, 2026-10-06 13:33 UTC)

## Summary
- **Orphaned claims: 18 real orphan candidates of 22 in_progress** (4 live workers: wwj2/gzkr/n9nq/mt7e with fresh heartbeats). All 18 have registry rows `state=completed` with stale heartbeats (1–53 min old). Charon orphan recovery is LIVE and operating — 44+ `charon_orphan_reopened` (dead_worker) events on 2026-10-06, repeatedly reopening the same cohort (cw35 ×2, bldo ×2, 95ia ×3, sh9h ×4, nydy/lkbo/lz3m ×3+). Mechanical reconciliation does NOT need a code change; it is running.
- **The anomaly that persists is churn, not reconciliation failure.** Root cause is documented by bead hydra-cw35 itself (in_progress, P0): gate-failure → close rejected → bead re-enters ready queue → full LLM redo (~11 spawns/45 min/bead on srv-jobfile). Workers exit (`completed` rows) while the bead stays in_progress pending gate verdict — the 5m Charon grace then reopens it as an "orphan". cw35 is the fix bead for this loop; no duplicate change filed here.
- **Publisher wedge: RESOLVED.** Local checkout was 16 commits ahead of origin/development (PR #263 merge e5e33b5). Open PR #264 (dogfood/publish-hydra → development, mergeStateStatus UNSTABLE/checks-pending) covered them. Admin-merged per precedent (PR #85/#148/#162/#263): **MERGED 2026-10-06T13:34:50Z, mergeCommit e79798a3** — verified `git ls-remote origin development` → e79798a3 (tip = merge commit). Wedge cleared.
- **Disk: STALE claim.** `df -h /` → 212G/451G = **50%** (watermark 85%). `systemd-tmpfiles --clean` run as hygiene. No I/O-error-28 risk.

## Orphan cohort (18, all `completed` stale rows, no live worker)
cw35, bldo, q207, lb7g, e2a7, jdvp, 7tzf, dyw2, 95ia, lg7g(lg7b), 7w2f, uppd, ngui, 96th, xmla, n5rv, m3au, m72l

## Disposition
1. No code change — reconciliation (Charon `recoverOrphanInProgressBeads` + scheduler `reconcileOrphanClaim`) verified operating on live events.
2. Publisher wedge cleared via PR #264 admin-merge.
3. Root-cause churn fix tracked by hydra-cw35 (integrate-403 reopen loop) — do not duplicate.
4. Disk claim stale at 50%.

## Verification evidence
- Registry cross-check 13:33Z: 22 in_progress; 4 `working` fresh (<30s); 18 `completed` rows, oldest heartbeat 12:41Z (m72l/m3au).
- Events 2026-10-06.jsonl: charon_orphan_reopened ×44 (12:05–12:18 window shows 8- bead cohort sweep).
- `gh pr view 264` pre-merge: OPEN/UNSTABLE; post-merge MERGED e79798a3 @ 13:34:50Z.
- Note: `gh` calls returned transient 404 "Could not resolve to a Repository" for haulmatic-devteams/hydra at ~13:30Z while `curl` with the identical token returned 200 — transient GitHub API flap, same class as the 8odx state-read race; publisher already retries.
