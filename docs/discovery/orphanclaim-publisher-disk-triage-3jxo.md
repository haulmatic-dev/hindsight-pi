# P0 fleet anomalies triage — orphaned claims + publisher wedge + disk (hydra-3jxo)

Triage date: 2026-10-06 13:44–13:50 UTC. Verdict: all three P0 claims verified stale or
known-class churn; no code change, no manual reap, no human merge required.

## 1. "12 orphaned in_progress claims"

**Verified: retry-gap churn snapshot of the documented hu12/ew7t class, not a broken
recovery path.**

- `br list --status in_progress` → 22 beads (recovered store with
  `br doctor migrate-schema recover` first; "Database error: database is busy
  (recovery in progress)" WAL poison cleared).
- Cross-check of every in_progress bead against
  `/opt/dev-tools/var/hydra/.hydra/registry.db` → 15 beads had completed rows with
  heartbeats 5–27 min stale; 7 had live `working` rows (fresh heartbeats).
- 5 live worker processes confirmed (`ps`: PIDs 3531150, 3550310, 3625622, 3628036,
  3641678, started 13:29–13:44). Worktrees for the cohort modified minutes ago
  (uppd 13:24, xmla 13:30, wwj2 13:32).
- Charon sweep LIVE and healthy: `stats scans=90, orphans_found=0`, 48
  `charon_orphan_reopened` events today (latest 13:34:36, reason dead_worker).
  The sweep is continuously reopening churn candidates; they respawn, run gates,
  and re-enter the backoff window. This is the known gate-fail churn loop
  (hydra-hu12 triage), not silent zero progress.
- Escalated beads (hydra-q207, hydra-rxi6) correctly skipped by Charon with
  `orphan_escalated_wedged` events — human/Pan gated by design (7548196).
- Zero `P0 campaign anomaly` digest filings in today's event stream.
- Reconciliation fixes (`reconcileOrphanClaim`, heartbeat-fresh liveness,
  claim grace, escalated + pan-filed digest exclusions) all deployed (daemon PID
  3172191 running the 12:16 UTC binary).

Manual reap NOT executed: playbook rule (hydra-uzmx/hydra-cgud triages) — do not
force-release claims while recovery is operating and the cohort is transient churn.

## 2. "Publisher wedge: locally-merged work unpublished for 47m"

**Verified: no unpublished work.**

- `git fetch origin development` → remote tip `c911a406` (PR #266 merge,
  `dogfood/hydra-gzkr`).
- `git merge-base --is-ancestor HEAD origin/development` → YES. Main checkout HEAD
  `9993250` is an ancestor of the remote tip; every local commit is published.
- Local checkout is `behind 6` of the remote tip — the daemon publish loop
  fast-forwards; not an operator action.
- Open PRs on `haulmatic-devteams/hydra` are pan-filed *proposal* PRs (#266, #265,
  #262, #258, #257, #253, #252, #234, #232, #231), not publish wedges awaiting a
  human merge.
- Note (not touched): main checkout has uncommitted changes (`M CHANGELOG.md`,
  `M docs/architecture/shepherd.md`, 2 deletions) — an in-flight strand by another
  worker; left alone per REPO HYGIENE.

## 3. "Disk 99% on / (watermark 85%)"

**Verified: stale.**

- `df -h /` → 214G/451G = **50%** (watermark 85%). No I/O-error-28 risk.
- `systemd-tmpfiles --clean` executed as hygiene (benign tmpfiles warnings, no
  failures). Disk-cleanup guard (`disk_cleanup.go`, a4d40f4) is deployed for real
  breaches.

## Remediation applied

- `br doctor migrate-schema recover` on the hydra bead store (WAL poison cleared).
- `systemd-tmpfiles --clean`.
- Triage documentation + changelog entry (this bead).
