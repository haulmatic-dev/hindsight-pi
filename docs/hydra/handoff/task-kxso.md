# discovery handoff (hydra-kxso)

## What changed (files + one line each)
- docs/discovery/orphanclaim-escalated-verify-triage-kxso.md: verification triage — both P0 anomaly claims stale/by-design, no code change; staged fresh binaries recorded.
- CHANGELOG.md: Documentation entry under Unreleased.
- hydra-staged-kxso / worker-staged-kxso (worktree artifacts, untracked): fresh daemon+worker binaries built from HEAD (7548196 = origin/development content) for the operator deploy.

## Verified findings
- The 4 in_progress beads (hydra-wiu6, hydra-1h4v, hydra-bxzr, hydra-cxpf): all carry escalated/needs-human/verified-close-escalated/verified-close-reject-tdd-validation-failed — left in_progress by design pending human review, NOT orphaned. Registry had 6 working rows, all heartbeats < 1 min, PIDs live.
- Charon live: 52 charon_orphan_reopened events today (dead_worker), incl. bxzr 07:47, cxpf 07:50, hydra-kxso itself 08:24. Recovery loop operating.
- No P0-campaign-anomaly digest filing after the 6d01 window in today's events.
- Duplicate-class: hydra-6d01 already triaged and fixed this exact false alarm — commit 7548196 "fix(digest): exclude human-escalated beads from OrphanClaims counter" (pan_health.go:177) landed on origin/development (tip 55102f3).
- Disk: df -h / → 188G/451G = 44% (watermark 85%); 99% claim stale.
- 21 wtbranches: normal churn, no stranded-work cleanup indicated.
- br WAL poison recurred ("Database error: database is busy (recovery in progress)") — cleared with br doctor migrate-schema recover.

## Residual open edge (operator action)
- Deployed /opt/dev-tools/var/hydra/bin/hydra (mtime 07:44) lacks orphan_claim_reset (PR #70), empty_diff_completed, and the 7548196 escalated-exclusion — predates today's fixes. Worker binary mtime 00:44 also predates.
- Staged rebuild VERIFIED: `go build ./...` PASS; hydra-staged-kxso contains orphan_claim_reset + empty_diff_completed literals. Operator: drain-restart with the staged binaries.
- Human review still required for the 4 escalated beads regardless of binary version.

## Verification (commands run + pass/fail)
- df -h / → 44%. PASS (stale disk claim).
- registry.db + .beads/beads.db queries → 4 escalated in_progress beads, 6 live working rows. PASS.
- events 2026-10-02.jsonl → 52 charon_orphan_reopened; no new P0 anomaly filings. PASS.
- git log origin/development → 55102f3 tip contains 7548196 exclusion. PASS (git merge-base ancestor check BB-LANDED).
- strings/grep on deployed daemon binary → orphan_claim_reset/empty_diff_completed ABSENT → stale binary confirmed. VERIFIED.
- go build ./... + staged binaries with fix literals present. PASS.
