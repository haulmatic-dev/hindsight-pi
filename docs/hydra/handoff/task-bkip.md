# task handoff (hydra-bkip)

## What changed (files + one line each)
- docs/discovery/orphanclaim-disk99-triage-bkip.md: full triage of the 14-orphan claim — every in_progress bead without a live worker dispositioned live; no code change (recovery verified self-healing).
- CHANGELOG.md: Documentation entry under Unreleased.
- Commit 641e5cb (2 files, +78), verified via git show --stat.

## Verification (commands run + pass/fail)
- `df -h /` → 210G/451G = 49% (watermark 85%) — disk-99% claim STALE, no cleanup needed. PASS.
- Registry `$HYDRA_HOME/.hydra/registry.db` + `br list --status in_progress` cross-check (25 in_progress, 17 without live working row): 8 pan-filed pseudo-beads (yt1y/uzmx/m5j9/oj1v/8gvw/ew7t/cgud/pqq5, digest-excluded by 7ef09f6), 5 escalated (rxi6/q207 fresh today; k991/kvsc/ee5r stale since 10-04, under 72h TTL — Charon skip by design, `orphan_escalated_wedged` events verified), 3 in-flight close gates (m3au/m72l/gcal, serialized per-project gate lock, Charon "close in progress" skip correct), 3 churn-recovered (cw35 ×2, bldo ×6, 95ia ×3 `charon_orphan_reopened` today). Zero true wedged orphans. PASS.
- Gate artifacts `/opt/dev-tools/var/hydra/gates/hydra/` inspected: 95ia test gate exit -1 (timeout) with all logged packages ok — contention-driven wrong verdict; cw35 real FAIL; bldo lint passed after 21m. PASS (evidence).
- `br doctor migrate-schema recover` — cleared WAL poison ("database is busy (recovery in progress)"). PASS.
- hindsight_search at bead start — not connected (skip rule applied). graphify-out/graph.json absent → rg/live-store fallback. PASS.

## Open edges for the next phase
- Churn cost owner is the filed task bead hydra-cw35 (park-on-integrate-failure), not orphan recovery.
- Escalated queue k991/kvsc/ee5r: Pan clear-escalation or 72h TTL demotion.
- Gate timeout under fleet contention (95ia exit -1 all-ok) worth a follow-up bead if backlog grows.
