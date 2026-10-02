# Orphan-claim + disk-99% triage (hydra-pf9c)

2026-10-02 ~08:40 UTC.

## Claim 1: "4 in_progress beads with no live registry worker"

FALSE — the 4 beads (hydra-bxzr, hydra-1h4v, hydra-wiu6, hydra-cxpf) all carry the
`escalated` label (tdd-validation-failed close rejections escalated to human).
Human-gated beads never get a worker; absence of a registry row is expected, not
orphaned. This exact class was fixed by hydra-6d01: commit 7548196 "fix(digest):
exclude human-escalated beads from OrphanClaims counter" — present in this
worktree HEAD and on origin/development.

- `br show` ×4 — all four labels include `escalated`, `needs-human`, `verified-close-escalated`.
- Registry /opt/dev-tools/var/hydra/.hydra/registry.db: no `working` rows for the
  four (expected), 7 live rows for active beads with heartbeats <1 min old.
- Charon orphan recovery live: fmsfe-x4w/q2t/35c/qbn rows state=completed
  err_message=`orphan-reaped` at 08:19 — the reaper ran minutes before this bead.

The filers (hydra-i6we, hydra-kxso) are duplicate pan-filed filings of hydra-6d01's
finding; no action beyond this triage.

### Deployed-binary staleness (operator note)

`/opt/dev-tools/var/hydra/bin/hydra` built 2026-10-02 07:44:39 UTC;
fix 7548196 committed 08:31:13 UTC → deployed binary PREDATES the escalated
exclusion, so the digest may keep filing the P0 anomaly until the daemon is
restarted on a rebuilt binary. Verify with
`strings /opt/dev-tools/var/hydra/bin/hydra | grep -c orphan_claim_reset`
(in the new binary this is ≥1; in the deployed one it is 0).

## Claim 2: "host disk 99%, exceeds 85% watermark"

STALE — `df -h /` = 189G/451G = **44%**. No systemd-tmpfiles cleanup or cache cap
required; no I/O-error-28 risk.

## Disposition

- No code change: the only real remedy (escalated-label exclusion) already landed
  (7548196). The orphan-claim reaper itself (Charon recoverOrphanInProgressBeads +
  scheduler reconcileOrphanClaim) is live and verified.
- Disposition: duplicate of hydra-6d01; i6we/kxso can be closed as duplicates once
  their own chains resolve.
- Operator action: rebuild + restart daemon so the deployed binary includes
  7548196 (and any later commits).
