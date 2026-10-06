# discovery handoff (hydra-m5j9)

## What changed (files + one line each)
- docs/discovery/orphanclaim-triage-m5j9.md: triage — orphan reconciliation verified live, digest count is provider-pool-queued churn + by-design escalated orphans, disk 99% claim stale at 49%; no code change.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.

## Verification (commands run + pass/fail)
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: 4 `working` rows all with heartbeats < 2 min old (95ia, oj1v, m5j9=self, uzmx). No dead-worker liveness desync. PASS.
- Events 2026-10-06.jsonl — 44 orphan events; 6 `charon_orphan_reopened` (dead_worker) 12:18–12:21 UTC (sh9h, nydy, lkbo, 2a3f, s8l7, lz3m); typed `orphan_escalated_wedged` correctly routing escalated beads (q207, ee5r, k991, kvsc, rxi6) to human/Pan clear-escalation. Recovery path NOT broken. PASS.
- `df -h /` → 209G/451G = **49%**, below 85% watermark — disk-99% claim STALE; no cleanup needed. PASS.
- Provider-pool saturation evidence: `[ProviderPool] No slot for bead hydra-uzmx (pool 10/10 (0 waiting)): provider pool: timed out after 2m0s waiting for 1 slot(s)` — reopened beads queue for slots; in_progress-without-worker is transient, not wedged. PASS.
- Deployed binary current: mtime 12:16:29 == daemon start 12:16:28 (PID 3172191); escalated-digest exclusion 7548196 ancestor of origin/development (merge-base check YES). PASS.
- br WAL poison recurred ("database is busy (recovery in progress)"); `br doctor migrate-schema recover` cleared. PASS.
- hindsight_search at bead start — not connected (skip rule applied). PASS.
- graphify query — graph.json absent in cwd → live-store/rg fallback per protocol. PASS.

## Open edges for the next phase
- Escalated orphans (q207/rxi6/k991/kvsc/ee5r) need Pan `clear-escalation` or human action — by design, not a defect.
- Idea (not implemented): digest grace for beads with a recent `charon_orphan_reopened` event would kill the remaining filing churn.
- cw35's own P0 (integrate-failure reopen loop) is a separate live bead, untouched here.
