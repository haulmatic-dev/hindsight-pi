# discovery handoff (hydra-v5xq)

## What changed (files + one line each)
- docs/discovery/orphanclaim-escalation-loop-triage-v5xq.md: triage — the 4 "orphaned in_progress claims" are escalated verified-close beads (k991/kvsc/ee5r), not orphans; reconciliation and digest behaved correctly; no code change.
- CHANGELOG.md: Documentation entry under Unreleased.

## Live-store actions (verified)
- Triage comments added to hydra-k991, hydra-kvsc, hydra-ee5r (✓ ×3).
- `br doctor migrate-schema recover` cleared br WAL poison ("database is busy (recovery in progress)", exit 6).
- `br close` ×3 with not_planned landed reasons (✓ ×3). Wedged bead IDs listed: hydra-k991, hydra-kvsc, hydra-ee5r.

## Verification (commands run + pass/fail)
- Registry /opt/dev-tools/var/hydra/.hydra/registry.db: 19 rows; 17 in_progress beads cross-checked → only k991/kvsc/ee5r had no live worker, all carry `escalated`+`needs-human`+`verified-close-escalated`. Charon skip of escalated beads verified in internal/core/charon.go (fail-closed by design). PASS.
- Landed checks on origin/development: emitGateEvent/closeDepOpenEvent (k991) 8 hits; applyVerificationContract (kvsc) 5 hits + handoff present; ErrFileBeadDuplicate (ee5r) 3 hits + persistent normalized-title dedupe pan_health.go:439. PASS.
- Events: zero "P0 campaign anomaly" filings 10-02→10-06; deployed binary rebuilt 2026-10-06 12:16 (7548196 landed, ancestor check PASS). Digest quiet. PASS.
- Re-escalation loop evidence: k991/kvsc `bead.escalated` on 10-04 AND again 10-06 after re-queue; ee5r empty-diff escalation 10-06. Force-release would loop — closes are the correct release. PASS.
- Disk: `df -h /` → 49% (watermark 85%) — 99% claim stale. PASS.

## Open edges for the next phase
- Churn edge (recorded, not fixed): re-queue of an escalated verified-close bead whose delta is already integrated always dies at close — consider a pre-spawn "already integrated" check before honoring re-queues.
- br WAL poison recurred again (exit 6); chronic, prior triages 2vuc/8py6/qqtl.
