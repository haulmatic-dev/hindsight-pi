# Triage: orphaned in_progress claims + publisher hold + disk 99% (hydra-o331)

Date: 2026-10-06 14:21 UTC. All three P0 claims in the fleet digest verified STALE/BY-DESIGN. No code change — reconciliation is NOT failing; all remediation mechanisms already landed and are live.

## Claim 1: 14 orphaned in_progress claims wedging the DAG ladder — FALSE (churn snapshot)

Registry `/opt/dev-tools/var/hydra/.hydra/registry.db` at 14:21 UTC, cross-checked against `br list --status in_progress` (21 beads in the hydra store):

- 4 live `working` rows, heartbeats < 60s: hydra-n9nq, hydra-o331 (this bead), hydra-xcye, hydra-z0do.
- Every other in_progress bead has a `completed` registry row with a heartbeat 5–60 min old (retry-gap churn: worker exited after a gate-failed close reject; respawn backoff window is what the digest counts). Freshness samples: 7w2f 14:19, n5rv 14:18, 6t5v 14:16, 7tzf/dyw2 14:03, bldo 13:32, jdvp 13:20.
- hydra-q207 + hydra-rxi6 carry `escalated` + `needs-human` — human-gated by design (7548196 digest exclusion; Charon correctly emits `orphan_escalated_wedged` instead of reopening). **Human-review answer:** these two are the only beads needing a human, via Pan `repair:clear-escalation` (one fair retry) or explicit judgment. q207/rxi6 have been escalated since 10-04, inside the 72h `overseer.escalation_ttl` window.
- `grep -c "P0 campaign anomaly" events/2026-10-06.jsonl` = **0** — the digest has not filed the anomaly today; the fleet-digest snapshot this bead was filed from predates the current state.
- Charon recovery LIVE: `charon_orphan_reopened` (dead_worker) events firing at bead-start instant (hydra-ngui, hydra-96th reopened 14:21:32Z). Scheduler `reconcileOrphanClaim` + digest grace (qmf7) + pan-filed exclusion (7ef09f6/qqtl) + escalated exclusion (7548196) + gate-close-in-progress probe all landed on origin/development; deployed binary `/opt/dev-tools/var/hydra/bin/hydra` built 2026-10-06 12:16 UTC from tip content.

Conclusion: orphanclaims=14 is a pre-restart churn snapshot, not a wedge. Reconciliation path verified operating end to end.

## Claim 2: publisher holds locally-merged work 1h24m (PR awaiting human merge) — FALSE

- Local checkout `/opt/dev-tools/components/hydra`: `git rev-list --count origin/development..HEAD` = **0** (nothing unpublished).
- `git ls-remote origin development` → `5b6f28b0` = merge commit of PR #270 — remote tip IS the publish merge; work published.
- Open PR scan (`gh pr list -R haulmatic-devteams/hydra --state open`): **no dogfood/publish-hydra PR open**. Remaining open dogfood/* PRs are per-bead strands, not the publish head.

No human merge action pending. Wedge absent.

## Claim 3: disk at 99% on / (watermark 85%) — FALSE

- `df -h /` → 167G/451G = **39%**, well below the 85% watermark. No I/O-error-28 risk.
- `systemd-tmpfiles --clean` run as hygiene (rc=0; expected permission noise on other-user /tmp dirs).
- Automated guardrail already landed: `diskCleanupGuard` (internal/core/disk_cleanup.go, a4d40f4) runs cleanup + emits `disk_cleanup_run` on real watermark breaches. No new guardrail needed.

## Live-store actions

- None required beyond hygiene; no beads closed or labels cleared (all in_progress beads carry live/fresh registry rows or by-design exclusions).

## Open edges

- q207/rxi6: human/Pan decision (clear-escalation for one fair retry, or abandon-with-summary). Only true human-gated items.
- Recurring digest churn twins remain an operational cost; the landed exclusions (pan-filed, escalated, grace, gate-in-progress) have silenced today's filings.
