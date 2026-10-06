# task handoff (hydra-8gvw)

## What changed (files + one line each)
- docs/discovery/orphanclaim-disk99-triage-8gvw.md: triage — both P0 claims verified stale; full landed-fix chain table + re-fire playbook; no code change.
- CHANGELOG.md: Documentation entry under Unreleased.
- docs/hydra/handoff/task-8gvw.md: this handoff.

## Verified findings
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: all 21 in_progress hydra beads have rows, heartbeats 0–21m old, **0 orphans** under digest semantics. PASS.
- Deployed `/opt/dev-tools/var/hydra/bin/hydra` built 2026-10-06 12:16 UTC from 3322928 (origin/development tip); `reconcileOrphanClaim`/`orphan_claim_reset` strings present ×11. PASS.
- Events 2026-10-06.jsonl: 44 `charon_orphan_reopened` (latest 12:21 UTC); 0 `P0 campaign anomaly` filings today. PASS.
- `df -h /` → 208G/451G = **49%** (watermark 85%) — disk claim stale; diskCleanupGuard deployed for real breaches. PASS.
- Escalated-orphan wedge (rxi6/q207/ee5r): by-design steady state; resolution via Pan `clear-escalation` or revived EscalationTTLScanner (2f10a0f, `SetLiveWorkerFunc` wired overseer.go:500). ee5r already CLOSED.
- `br doctor migrate-schema recover` — cleared WAL poison ("database is busy (recovery in progress)") once at bead start. PASS.
- hindsight_search at bead start — not connected (skip rule applied).

## Open edges for the next phase
- If the anomaly re-fires: check deployed binary mtime vs origin/development tip first (recurring historical root cause), then `grep orphan_escalated_wedged` for escalated orphans.
- Escalated beads rxi6/q207 age out via TTL (72h default) unless Pan clear-escalation fires sooner.
