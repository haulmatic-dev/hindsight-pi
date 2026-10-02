# DISCOVERY handoff

## What changed (files + one line each)
- docs/discovery/orphan-claim-triage-12di.md — triage: orphaned in_progress claim identified (hydra-1p1q, hydra-vk94, fmsfe-sn5) and already reaped by Charon; disk 99% claim stale at 43%; no code change.
- CHANGELOG.md — Documentation entry pointing at the triage.
- No code change (triage/human-review bead; recovery already fired — Charon reopened all 3 orphaned beads 2026-10-02 00:52–02:47 UTC).

## Verification (commands run + pass/fail)
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db` — all 7 in_progress hydra beads (12di, s9sg, 1euz, wv2e, yz8z, t2e1, qmf7 + l867) have live `working` rows, heartbeats < 1 min old. orphans = 0. PASS.
- Events 2026-10-02.jsonl — 3 `charon_orphan_reopened` (reason dead_worker): fmsfe-sn5 00:52, hydra-1p1q 02:45, hydra-vk94 02:47. PASS.
- `df -h /` — 185G/451G = 43% (watermark 85%). Stale digest claim; no cleanup needed. PASS.
- hindsight_search on orphaned in_progress claim terms — prior reconcileOrphanClaim fix (1a3e643) + Charon park-gate fix confirmed landed. PASS.
- Digest twins: s9sg/1euz/wv2e/yz8z/t2e1/qmf7 are pan-filed duplicate filings of this incident (pre-dedupe-fix); disposition documented. PASS.

## Open edges for the next phase
- Close the six duplicate P0 twin beads as duplicate-of hydra-12di once their workers exit.
- Secondary: 6,721 publish_identity_resolved / 2,709 push-attempt events on 10-02 = hot publish-retry loop worth a follow-up triage bead.
