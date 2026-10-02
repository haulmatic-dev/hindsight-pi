# Triage: orphaned in_progress claim wedging hydra DAG ladder + disk 99% (hydra-12di)

Verified 2026-10-02T03:06–03:08 UTC.

## Orphan claim — FOUND AND REAPED (by Charon, before human action)

- Digest claimed 1 in_progress bead with no live registry worker. Live registry
  (`/opt/dev-tools/var/hydra/.hydra/registry.db`) shows 3 `charon_orphan_reopened`
  events on 2026-10-02: `fmsfe-sn5` (00:52), `hydra-1p1q` (02:45), `hydra-vk94`
  (02:47) — all reason `dead_worker`, beads reopened to open.
- At verification time every in_progress bead in the hydra project (12di, s9sg,
  1euz, wv2e, yz8z, t2e1, qmf7, l867) has a live `working` registry row with a
  heartbeat < 1 minute old. `orphans = 0`.
- Charon recovery + scheduler `orphan_claim_reset` reconciliation are working.
  The digest orphan figure was a point-in-time snapshot (hydra-1p1q / hydra-vk94
  between worker death 02:40–02:51 and the 02:45–02:47 Charon sweep).

## Digest P0 twins — noise from pre-fix Pan filings

Five of the open in_progress beads are P2 `task` pan-filed campaign-anomaly
twins describing this same incident (s9sg, 1euz, wv2e, yz8z, t2e1, qmf7 — filed
before `filePanBead` dedupe counted non-terminal beads; fix landed per
hydra-1p1q handoff). They will each reach the same conclusion; no new repair
verb needed. Disposition: close as duplicate once their workers finish, or let
each worker's triage close them.

## Event storm (secondary observation)

2026-10-02.jsonl has 6,721 `publish_identity_resolved` and 2,709×4
(push_attempted / push_rejected / pr_invoked / path_classified) events — a
hot publish-retry loop worth a follow-up look, not part of this bead.

## Disk 99% — STALE

`df -h /` = 185G/451G = **43%** (watermark 85%). No systemd-tmpfiles cleanup
required; matches prior verification (hydra-mvcx, hydra-1p1q: 42–43%).

## Disposition

- No code change. No repair verb required — recovery already fired.
- Optional hygiene: close duplicate P0 twin beads (s9sg, 1euz, wv2e, yz8z,
  t2e1, qmf7) as duplicate-of hydra-12di once their in-flight workers exit.
