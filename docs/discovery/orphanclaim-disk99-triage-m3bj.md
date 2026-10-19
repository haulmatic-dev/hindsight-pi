# P0 fleet-anomaly triage — hydra-m3bj (2026-10-06 ~14:25 UTC)

All three fleet-level claims in the bead body verified STALE at triage time. No
code change; recovery machinery demonstrably operating.

## 1. "23 orphaned in_progress claims wedging the DAG ladder"

- Store state (`br list --status in_progress`, /opt/dev-tools/components/hydra,
  after one `br doctor migrate-schema recover` for the recurring WAL poison
  "database is busy (recovery in progress)"): **18** in_progress, not 23.
- Disposition per bead:
  - 5 `pan-filed` meta beads (hydra-o331, hydra-m3bj = this bead, hydra-z0do,
    hydra-6t5v, hydra-rz63) — excluded from the digest counter by design
    (7ef09f6); they are the anomaly's own filings, not work items.
  - 2 `escalated` + `needs-human` (hydra-q207, hydra-rxi6) — human-gated by
    design (7548196); no worker expected; Charon emits
    `orphan_escalated_wedged` and correctly refuses reopen.
  - 11 remaining (bldo, lb7g, 7tzf, dyw2, n9nq, mt7e, 7w2f, xmla, n5rv, m72l,
    gcal) — every one has a registry row in
    `/opt/dev-tools/var/hydra/.hydra/registry.db`: 1 `working` with a fresh
    heartbeat (hydra-n9nq, 14:22:26Z) and 10 `completed` rows
    13:31–14:19Z (retry-gap churn snapshot, the well-documented digest grace
    class — 0a2a6c9/0d4aeaa).
- **Live orphans = 0.**
- Charon recovery demonstrably firing seconds before this query:
  `charon_orphan_reopened` hydra-bldo 14:22:30Z, hydra-xmla 14:22:31Z
  (reason `dead_worker`), 52 reopens so far today.
- Digest: **0 "P0 campaign anomaly" filings** in today's event stream.
- Daemon current: PID 3172191 started 12:16:28Z, binary mtime 12:16
  (`reconcileOrphanClaim` / escalated + pan-filed exclusions deployed).

## 2. "Publisher PR awaiting human merge for 1h20m"

- Local checkout HEAD `343b7377` is an **ancestor** of `origin/development`
  (`git merge-base --is-ancestor` YES; `git rev-list --count
  origin/development..HEAD` = 0) — nothing unpublished.
- No open PR with head `dogfood/publish-hydra` on haulmatic-devteams/hydra
  (`gh pr list --state open` → `[]`).
- Local checkout is 9 commits *behind* origin — daemon publish loop owns the
  fast-forward; not a human action. **No wedge.**

## 3. "Disk at 99% on / (I/O error 28 imminent)"

- `df -h /` → 167G/451G = **39%**, below the 85% watermark. Claim stale;
  no `systemd-tmpfiles` cleanup or cache cap required. Disk-cleanup guard +
  host housekeeping (hydra-cache-trim.timer, tmpfiles config) already landed
  for real breaches.

## Conclusion

Same class as the prior sibling triages (hydra-bkip, hydra-8gvw, hydra-uzmx,
hydra-ew7t, hydra-hu12): a retry-gap churn snapshot + by-design exclusions
read as a wedge. Scheduler orphan-claim reconciliation, Charon recovery, and
the digest exclusions are all deployed and verified live. No hydra code change
warranted; no bead is wedged; disk healthy.
