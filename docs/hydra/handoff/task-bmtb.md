# task handoff

## What changed (files + one line each)
- docs/discovery/escalation-hygiene-triage-bmtb.md — full triage: 6 beads carried stale `escalated` (98–298h), all duplicates of open originals; daemon-side TTL scanner + Pan clear-escalation already landed upstream, so disposition = live-store label hygiene, no code change.
- CHANGELOG.md — Unreleased/Documentation entry pointing at the triage.

Live-store actions (in /opt/dev-tools/components/hydra, all verified via br output):
- `br doctor migrate-schema recover` ×4 — cleared recurring WAL-index poison ("database is busy (recovery in progress)") between write bursts.
- Removed `escalated` from hydra-wdp, hydra-1ly, hydra-mt8, hydra-cip, hydra-gvh, hydra-ol7 (✓ ×6).
- Removed `needs-human` + `verified-close-escalated` from hydra-mt8 and hydra-ol7 (✓ ×4).
- Triage comment added to each bead: one fair scheduler retry, duplicate-of pointer, abandon-with-summary on loop.

## Verification (commands run + pass/fail)
- `br list --json | jq` label scans — before: 6 beads with `escalated`; after: 0. PASS.
- Post-clear status: hydra-mt8/ol7 retain only `closed-failed,verified-close-reject-empty-diff`; wdp/1ly/cip/gvh retain `closed-failed` (1ly +pr-open). PASS.

## Open edges for the next phase
- Scheduler should retry the six cleared beads; if any loops the same closed-failed death, Pan abandon-with-summary is the exit, not re-escalation.
- escalation_ttl scanner still uses UpdatedAt as age proxy (label touch resets clock — conservative).
- WAL-index poison recurred 4× during the session; every br write burst may need `br doctor migrate-schema recover` first.
