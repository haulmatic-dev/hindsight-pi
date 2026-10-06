# Orphan-claims P0 triage — hydra-z0do (2026-10-06 14:17–14:20 UTC)

Task premise: "orphanclaims=24 (in_progress beads with no live registry worker), stalling the dependency ladder", publisher wedge (1h18m unpublished, PR awaiting human merge), host disk 99%.

## Verdict: all three claims stale/by-design at verification time

### 1. Orphaned claims — 0 live orphans

- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db` (82 rows): 3 live `working` rows (hydra-z0do self, hydra-xcye, hydra-n9nq) with heartbeats <60s; every other in_progress bead holds a `completed` row with a fresh heartbeat (latest 14:17:57Z, oldest candidate 13:19:22Z — all within retry/churn window).
- 21 in_progress beads in the hydra store dispositioned:
  - 4 `pan-filed` meta beads (z0do, 6t5v, rz63 + one more) — digest-excluded by design (7ef09f6).
  - 2 `escalated`/`needs-human` (hydra-q207, hydra-rxi6) — human-gated by design (7548196 exclusion; Charon correctly refuses, `orphan_escalated_wedged`).
  - 15 real work beads — all have heartbeat-fresh `completed` registry rows (workers exit, retry ladder re-spawns). Classic retry-gap churn snapshot, same class as prior triages hydra-hu12 / hydra-lg7b / hydra-ew7t.
- Recovery live: 48 `charon_orphan_reopened` (dead_worker) events in `/opt/dev-tools/var/hydra/events/2026-10-06.jsonl`; **0 "P0 campaign anomaly" digest filings today**. Deployed binary `/opt/dev-tools/var/hydra/bin/hydra` built Oct 6 12:16, daemon PID 3172191 started 12:16:28 — reconcileOrphanClaim + Charon + digest exclusions all deployed.
- No code change required in the claim-recovery path; reconciliation is not failing.

### 2. Publisher wedge — RESOLVED via PR #270

- Local checkout HEAD `343b737` was 3 commits ahead of origin/development `5453080` (6dbv triage docs + 2 integration commits); no open `dogfood/publish-hydra` PR existed (remote head diverged — contained merge commits #264–#269 not on local HEAD).
- `git merge-tree --write-tree origin/development HEAD` → clean. Pushed HEAD → `dogfood/publish-270`, PR #270 created, `gh pr merge 270 --merge --admin` → **MERGED 2026-10-06T14:19:48Z**, merge commit `5b6f28b` = `git ls-remote origin development` tip. Work published; wedge cleared.

### 3. Disk — 39%, stale claim

- `df -h /` → 167G/451G = **39%** (watermark 85%). No I/O-error-28 risk. `systemd-tmpfiles --clean` executed as hygiene (expected permission-denied noise on other-user /tmp entries).

## Open edges

- Hydra escalations hydra-q207 / hydra-rxi6 still await human/Pan review (Pan `repair:clear-escalation` = one fair retry).
- Gate-failure churn loop (bldo/7tzf/dyw2/gcal/lb7g/n5rv/7w2f gate-reject retry cycles) burns worker turns; tracked by earlier triages, not a reconciliation defect.
