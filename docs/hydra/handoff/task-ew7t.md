# discovery handoff (hydra-ew7t)

## What changed (files + one line each)
- docs/discovery/orphanclaim-stale-snapshot-triage-ew7t.md: triage — orphan-claims=8 and disk-99% digest claims both verified stale/false; Charon recovery + reconcileOrphanClaim live; no code change.
- CHANGELOG.md: Documentation entry under Unreleased.

## Verification (commands run + pass/fail)
- `df -h /` → 208G/451G = **49%** (watermark 85%) — disk-99% claim stale. `systemd-tmpfiles --clean` run anyway (benign tmpfiles warnings). PASS.
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db` — all 20 in_progress hydra beads have a working row with heartbeat <1m (6 rows) or a completed row with fresh heartbeat (<5m grace); 0 live orphan claims. PASS.
- The 8 counted beads identified: 2 escalated (q207, rxi6 — excluded per 7548196), 5 pan-filed meta beads (hu12/v5xq/0ndi/2a3f/l5ug — excluded per 7ef09f6), gate-rejected-close transients (cw35/bldo/lkbo/s8l7/lz3m/sh9h) all within heartbeat-fresh liveness. PASS.
- Events 2026-10-06.jsonl — 44 `charon_orphan_reopened` (latest 12:18–12:21 UTC), 51 `orphan_escalated_wedged` (escalated beads correctly left for human/Pan), **0 "P0 campaign anomaly" filings**. PASS.
- Deployed binary `strings` — orphan reconciliation + digest exclusion symbols present. PASS.
- hindsight_search — not connected (skip rule applied). graphify-out/graph.json absent in cwd → registry/br/event evidence per protocol.

## Open edges for the next phase
- None for orphan reconciliation — machinery deployed and firing; anomaly is a digest snapshot-staleness artifact re-disproven (10th+ triage of this class).
- Escalated orphans (hydra-q207, hydra-ee5r) await Pan repair:clear-escalation or human — by design, not orphan recovery.
