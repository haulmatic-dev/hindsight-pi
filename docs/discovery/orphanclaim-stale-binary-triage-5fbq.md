# Triage: "4 orphaned in_progress claims" digest claim — stale binary, hydra-5fbq

Snapshot 2026-10-02 08:47 UTC.

## Claim

Hydra project digest: 4 in_progress beads with no live registry worker
(orphans), wedging the DAG ladder. Plus host disk 99% on /.

## Verified live state

- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: 7 `working` rows
  (fmsfe-06j, fmsfe-6w7, fmsfe-npa, hydra-ybhh, hydra-rdju, hydra-5fbq,
  hydra-ek7m), every heartbeat < 1 min old. Zero live orphans.
- The 4 counted beads (hydra-bxzr, hydra-1h4v, hydra-wiu6, hydra-cxpf) are
  `tdd-validation-failed` HUMAN ESCALATIONS (escalated label) — in_progress
  with no worker by design, never orphaned. The digest exclusion for
  escalated beads landed in commit 7548196 (08:31 UTC, PR #144).
- Daemon PID 1731164 started 08:19:41 UTC from binary built 07:44 UTC →
  running binary PREDATES 7548196 → digest still counts escalated beads as
  orphan claims. Same class as hydra-x60p / hydra-yusp (stale binary).
- Charon orphan recovery + scheduler reconcileOrphanClaim + 5m digest grace
  are all present on origin/development (tip ec0fc59) and symbol-verified in
  the deployed binary strings — recovery machinery is live; only the digest
  false-positive remains.
- Disk: `df -h /` → 190G/451G = **44%**, below the 85% watermark. The 99%
  claim is stale; no systemd-tmpfiles cleanup or cache caps needed.

## Remedy executed

- `go test ./internal/core/ -run TestCollectHealthDigest...` — PASS on this
  branch (HEAD == origin/development, empty diff).
- Built `./cmd/hydra` from origin/development tip (ec0fc59, includes the
  escalated-label exclusion; differs from deployed binary per `cmp`),
  installed atomically to `/opt/dev-tools/var/hydra/bin/hydra` (mtime
  08:49:28Z).
- Drain-restart EXECUTED 08:49Z: `kill -TERM 1731164` ("Received shutdown
  signal, stopping Hydra..." / "👋 Hydra stopped" in hydra.log); systemd
  Restart=always brought up new daemon PID 2176865 at 08:54:50Z on the new
  binary. Same procedure as hydra-yusp 03:55Z restart.
- Digest verified post-restart: 0 "P0 campaign anomaly" filings after
  08:54Z (last filing 08:29:53Z under old binary); 4 fresh `working`
  registry rows with new work already claimed. PASS.

## No code change

All proposed remedies already exist upstream: daemon-side orphan recovery
(Charon, 5m grace, fail-closed label skips), scheduler orphan-claim
reconcile, digest grace + escalated exclusion. Re-landing would duplicate
them.
