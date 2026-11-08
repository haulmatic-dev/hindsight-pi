# P0 fleet-anomaly triage (hydra-gzkr) — 2026-10-06

Twin bead: hydra-lg7b (pan-filed duplicate; closed as duplicate of hydra-gzkr).

## Claim 1: "14 in_progress beads with no live registry worker (orphaned claims wedging the DAG ladder)" — STALE / CHURN, NOT A RECONCILIATION BUG

Evidence (2026-10-06 13:30–13:35 UTC):

- Registry (`/opt/dev-tools/var/hydra/.hydra/registry.db`): only 2 `working` rows
  (hydra-gzkr, hydra-n9nq), heartbeats fresh.
- `br list --status in_progress` (hydra store, after WAL recovery): 20 beads.
  Every non-live one has a **fresh `completed` registry row with exit 0**
  (bldo 13:32, q207 13:29, lg7b 13:26, dyw2 13:23, jdvp 13:20, e2a7 13:19,
  7tzf 13:18, lb7g 13:16, 95ia 12:57, cw35 12:35).
- Workers are therefore spawning and finishing — the retry queue is live, not dead.
- Charon recovery live: 47 `charon_orphan_reopened` (dead_worker) events today,
  latest 13:27:33 (hydra-gcal). 0 `orphan_escalated_wedged` misfires on real work
  (57 events, all against escalated beads by design).
- Digest: **0 "P0 campaign anomaly" filings today** — the escalated/pan-filed/grace
  exclusions (7548196, 7ef09f6, 0a2a6c9, 0d4aeaa) hold. The anomaly claim traces to
  the pre-grace digest snapshot, not live state.
- Fleet progress: 14 `bead_closed` events today; publish pipeline active
  (PR #263 landed). The DAG ladder is advancing.

Residual churn (known, hu12 disposition): cw35/bldo/95ia carry
`verified-close-reject-gate-*` labels and re-run one worker per retry; dyw2's
workers exit 0 but two close attempts hit `verification_command_missing`
(fail-open, legacy bead) and the bead stays in_progress. This burns a worker per
retry but is bounded by the retry ladder, not a wedge. Recorded as known cost;
no code change in this bead.

## Claim 2: "Publisher holds locally-merged work unpublished 24m0s (PR awaiting human merge)" — RESOLVED BEFORE BEAD START

- Local checkout `/opt/dev-tools/components/hydra` (development): `git rev-list
  --count origin/development..HEAD` = **0** ahead. Nothing unpublished.
- `origin/development..HEAD` = 3 behind (daemon publish loop fast-forwards; not
  an operator action).
- PR #263 (dogfood/publish-hydra → development): **MERGED 2026-10-06T13:25:30Z**,
  mergeCommit e5e33b5 = current `git ls-remote origin development` tip.
- Open PRs on the repo are per-bead dogfood PRs (#228–#262, several days old),
  not the publish-channel PR. No human merge pending for the publish path.

## Claim 3: "Host disk 99% exceeds 85% watermark" — STALE

- `df -h /` → 212G/451G = **50%** (221G available). Watermark 85% not breached.
- `disk_cleanup_run` guard fired 402 times today (hourly cooldown as designed).
- No I/O-error-28 (ENOSPC) risk.

## Disposition

No reconciliation bug, no scheduler code change, no disk action, no human merge
pending. All three digest/anomaly claims were stale snapshots of a system whose
digest-false-alarm class was already fixed and deployed. Twin bead hydra-lg7b
closed as duplicate with triage comment.
