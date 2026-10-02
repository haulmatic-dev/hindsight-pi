# discovery handoff (hydra-pf9c)

## What changed (files + one line each)
- docs/discovery/orphanclaim-disk99-triage-pf9c.md: triage — the 4 named "orphaned" beads are human-escalated (bxzr/1h4v/wiu6/cxpf, `escalated` label), not orphans; fix already landed (7548196); disk 99% claim stale at 44%; no code change.
- CHANGELOG.md: Documentation entry under Unreleased.

## Verification (commands run + pass/fail)
- `br show hydra-bxzr/1h4v/wiu6/cxpf` — all four carry `escalated, needs-human, verified-close-escalated` (tdd-validation-failed escalations); no worker expected. PASS.
- Registry /opt/dev-tools/var/hydra/.hydra/registry.db — 7 live `working` rows with heartbeats <1 min; no rows for the four escalated beads (by design). PASS.
- Charon reaper live-verified: fmsfe-x4w/q2t/35c/qbn registry rows completed err_message=`orphan-reaped` at 08:19 UTC. PASS.
- `df -h /` → 189G/451G = 44% (watermark 85%) — disk-99% claim false; no cleanup needed. PASS.
- Deployed binary staleness: /opt/dev-tools/var/hydra/bin/hydra built 07:44:39 UTC; fix 7548196 committed 08:31:13 UTC; `strings` finds 0 `orphan_claim_reset` → binary predates the escalated-exclusion fix; daemon restart on rebuilt binary is the operator step. VERIFIED.
- Commit ccd9f54 in worktree (2 files, +46), verified via `git show --stat`.

## Open edges for the next phase
- Operator: rebuild + restart daemon so 7548196 (escalated digest exclusion) is live; digest otherwise keeps filing the P0 anomaly.
- hydra-i6we + hydra-kxso are duplicate pan-filed filings of this incident; close as duplicates of hydra-6d01 once their chains resolve.
