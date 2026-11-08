# discovery handoff (hydra-hu12)

## What changed (files + one line each)
- docs/discovery/orphanclaim-churn-triage-hu12.md: triage — orphanclaims=6 is a retry-gap snapshot of a gate-failure churn loop, not a reconciliation bug; no code change.
- CHANGELOG.md: Documentation entry under Unreleased.

## Verification (commands run + pass/fail)
- `br list --status in_progress` (hydra store, after `br doctor migrate-schema recover` for WAL poison "database is busy (recovery in progress)") — 19 in_progress: 5 escalated (excluded by design), 5 pan-filed meta (excluded), 9 churn candidates. PASS.
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db` — 8 of 9 candidates have `completed` rows (workers exited normally post gate-fail); 95ia/hu12/v5xq live `working`, heartbeats fresh. Progress live. PASS.
- Events 2026-10-06.jsonl — 44 `charon_orphan_reopened` (dead_worker); per-bead reopen counts: bldo 6×, sh9h 4×, lkbo 4×, 95ia/nydy/s8l7/lz3m 3×, cw35 2×. Charon recovery LIVE. PASS.
- `df -h /` → 208G/451G = 49% (watermark 85%) — 99% claim stale. PASS.
- pan_health.go grace verified: claimsByID keyed on bead UpdatedAt; worker-exit → respawn backoff window (>5m) is what the digest counts. No code defect. PASS.
- hindsight_search at bead start — not connected (skip rule applied). PASS.
- graphify-out/graph.json absent in cwd → rg + registry/event fallback per protocol. PASS.

## Open edges for the next phase
- Churn cost: 9 beads burn one worker each per retry pass failing the same gate-test; convergence via verified-close escalation cap exists but is slow. Optional future bead: key digest orphan grace on the registry row's terminal-state timestamp (worker exit) instead of bead UpdatedAt, or exclude recently-completed-row beads.
- The escalated cohort (k991/kvsc/q207/rxi6/ee5r) needs human review or Pan clear-escalation; escalation_ttl owns aging.
