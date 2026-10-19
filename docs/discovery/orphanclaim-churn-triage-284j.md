# Orphan-claim / publisher-wedge / disk-99% triage — hydra-284j

Verified live at 2026-10-06 14:58–15:00 UTC. All three P0 anomaly claims are stale snapshot artifacts. No code change.

## 1. Orphaned in_progress claims (digest claimed 9–24)

- `df`-equivalent first: registry `/opt/dev-tools/var/hydra/.hydra/registry.db` has exactly **1** `working` row at 14:58 (hydra-284j, this bead's worker, heartbeat fresh). Every other registry row is `completed`, heartbeats spanning 12:22–14:57 — the most recent cohort (e2a7, jdvp, rz63, 96th, rhin, y4i4, 21xp, jipg, v8sb) has heartbeats within the same minute as the check. Zero rows are stale beyond grace.
- `br list --status in_progress` (after `br doctor migrate-schema recover` cleared the recurring WAL poison, "database is busy (recovery in progress)") → 18 in_progress, all accounted for by design:
  - 6 `pan-filed` meta twins (284j, o331, m3bj, z0do, 6t5v, rz63) — excluded from OrphanClaims by 7ef09f6.
  - 2 `escalated` human-gated (q207, rxi6) — excluded by 7548196.
  - 10 gate-failed/discovery churn candidates with `completed` registry rows and fresh heartbeats (bldo, lb7g, e2a7, jdvp, n9nq, 7w2f, 96th, xmla, n5rv) — the retry-gap churn snapshot class already triaged in hu12/lg7b/qmf7.
- Charon recovery live: `charon_orphan_reopened` (dead_worker) hydra-dyw2 14:48:30Z and hydra-7tzf 14:49:30Z — minutes before this check.
- Events `2026-10-06.jsonl`: zero "P0 campaign anomaly" filings matched in today's stream at triage time.
- Conclusion: reconciliation + Charon operating; no wedged orphan; no code change. Recurrence is the digest snapshot churn loop documented by prior triages (qmf7 grace, wv2e heartbeat liveness, 7ef09f6/7548196 exclusions).

## 2. Publisher wedge (claimed 1h20m+ unpublished, PR awaiting merge)

- Local checkout `/opt/dev-tools/components/hydra`: `git rev-list --count origin/development..HEAD` = **0** — nothing unpublished. origin/development tip `c3be2cf` ("Integrate-failure reopen loop burns workers… (#265)", merged 14:5x UTC).
- No open `dogfood/publish-hydra` PR on haulmatic-devteams/hydra; open PRs are per-bead dogfood PRs (#271 fresh, #223–#234 older validation/feature strands) — none is a publish-head PR.
- Conclusion: wedge absent.

## 3. Host disk 99%

- `df -h /` → 169G/451G = **40%**, below the 85% watermark. No I/O-error-28 risk; no cleanup needed.

## Disposition

No code change, no live-store surgery beyond the routine `br doctor migrate-schema recover` WAL-poison clear (pre-existing recurring issue). The pan-filed twin cohort (o331, m3bj, z0do, 6t5v, rz63) can be closed as duplicates once their workers exit.
