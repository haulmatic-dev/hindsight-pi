# Triage: P0 orphaned in_progress claims + publisher wedge + disk 99% (hydra-fg7e)

Verified 2026-10-06 ~13:55 UTC. All three P0 claims in the fleet digest are **stale or by-design**; no code change, no cleanup required.

## 1. "17 orphaned in_progress claims" — FALSE ALARM (retry-gap churn)

Live cross-check at bead start:

- `br list --status in_progress` (hydra store): **26** in_progress beads.
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: **4 live `working` rows** (fg7e=self, xcye, n9nq, 21xp — heartbeats < 1 min).
- Of the 22 in_progress beads without a live `working` row:
  - **10 carry `pan-filed` and/or `escalated` labels** (3jxo, 4p4d, 6dbv, faeu, fg7e, rz63, xcye, yc0m pan-filed anomaly twins; q207, rxi6 escalated human-gated) — excluded from the digest OrphanClaims counter by design (landed fixes 7ef09f6 / 7548196). The anomaly twins are the orphan-claims digest's own filings.
  - The remaining ~12 are gate-rejected/retry-gap churn beads whose registry rows are `completed` with **heartbeat-fresh timestamps (12:22–13:41 UTC)** — workers exited normally; respawn backoff windows are what a snapshot counts as "orphan". Per the heartbeat-fresh liveness rule (0d4aeaa) these count as live.

- Events 2026-10-06.jsonl: **48 `charon_orphan_reopened`** (dead_worker) — Charon recovery loop operating; **0 `P0 campaign anomaly` filings today** — the digest is not currently firing.

**Verdict:** zero true orphans. Reconciliation (scheduler `reconcileOrphanClaim`) and Charon recovery are live. Snapshot counts are provider-pool-queue + respawn-backoff churn, previously triaged identical (hydra-m5j9, hydra-uzmx, hydra-ew7t, hydra-hu12).

## 2. Wedged beads needing human review (named)

Only two genuine human-gate beads remain in_progress with no worker — both deliberately escalated:

- **hydra-q207** — REFACTOR bead, `escalated` + `needs-human` + `verified-close-escalated` (verified-close-reject-tdd-validation-failed). Resolution: Pan `repair:clear-escalation` (one fair retry) or human verdict.
- **hydra-rxi6** — GREEN post-close-sweep bead, `escalated` + `needs-human` + `verified-close-escalated` (reject-dep-open + reject-tdd-validation-failed). Same remedy.

## 3. Publisher wedge — ABSENT

- Main checkout `/opt/dev-tools/components/hydra` on `development`: `development...origin/development [behind 7]`, `git log origin/development..HEAD` — **0 ahead**. No locally-merged unpublished commits; the "59m unpublished" premise is stale.
- Open dogfood PRs on haulmatic-devteams/hydra (PRs #267, #265, #262, #258, #257, #253, #252, #234, #232, #231, #230, #228, #226, …) are **per-bead dogfood PRs awaiting human/admin review** — a normal human-review queue, not the publish-hydra aggregate wedge. Branch-protection merges are human-only by policy.

## 4. Disk 99% on / — STALE

`df -h /` → **215G/451G = 50%** (watermark 85%). No cleanup needed; no ENOSPC (I/O error 28) risk. `diskCleanupGuard` (a4d40f4) is deployed for real breaches.

## Disposition

- No code change: all anti-recurrence mechanisms (reconcileOrphanClaim, Charon sweep, digest grace/escalated/pan-filed exclusions, disk guard) landed and live.
- The recurring twin filings (3jxo/4p4d/6dbv/faeu/yc0m/rz63) are the pan-filed dedupe class; close as duplicate-of once their live workers exit.
- q207/rxi6: human/Pan `clear-escalation`.
