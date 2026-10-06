# discovery handoff

## What changed (files + one line each)
- docs/discovery/orphanclaim-disk99-triage-cgud.md: triage — both P0 claims verified stale; no code change.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.

## Verification (commands run + pass/fail)
- `hindsight_search` at bead start — not connected; skip rule applied. PASS.
- `graphify query` — graph.json absent in cwd; rg/handoff fallback per protocol. PASS.
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db` (workers table): 7 `working` rows, all heartbeats <1 min old (incl. hydra-cgud). No in_progress bead without a live/fresh row. PASS.
- Orphan cohort from the digest filings recovered live: multiple `charon_orphan_reopened` (dead_worker) events 2026-10-06 for hydra-lz3m, cw35, bldo, 95ia, sh9h, nydy, lkbo, 2a3f, s8l7. PASS.
- Remaining in_progress beads without a working row: hydra-q207 + hydra-rxi6 carry `escalated`/`needs-human` — human-gated by design (7548196 exclusion). PASS.
- `df -h /` → 208G/451G = **49%** (watermark 85%) — disk-99% claim false; no ENOSPC risk, no cleanup needed. PASS.
- Disk guard already landed: diskCleanupGuard (disk_cleanup.go, a4d40f4) + collectDiskWatermark (85%, "/") — the hydra-side preflight proposal is covered by the fail-open cleanup + digest anomaly design. PASS.

## Open edges for the next phase
- None. Recovery chain operating; the recurring pan-filed twin beads (0ndi/2a3f/ew7t/hu12/pqq5/v5xq) are historical digest-filing churn of the already-fixed class (qqtl/oqp8/qmf7/6d01 triages).
- Optional hygiene: close the twin pan-filed P0 meta beads as duplicates once their workers exit.
