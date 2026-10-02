# discovery handoff (task-py8j)

## What changed (files + one line each)
- docs/discovery/publisher-wedge-pr158-triage-py8j.md: triage — PR #158 CLOSED unmerged (superseded by PR #148 which predates local tip); disk-cleanup feature + docs unlanded; disk 99% claim stale at 45%.
- docs/hydra/handoff/task-py8j.md: this handoff.
- CHANGELOG.md: Documentation entry.
- Cherry-picked unlanded strand onto origin/development as `dogfood/cherry-158-continue`: 281165c (disk-watermark cleanup guard feat a4d40f4), 958bbad (kxso triage docs b1dfbde), 22de0ec (pf9c re-triage docs ccd9f54), 9714039 (i6we handoff ffe0f4e), a41cbcb (pf9c handoff bbb1bc5), plus this bead's docs commit. No wholesale merge — local HEAD (79507af) predates dev's PR #147/#148 work (spawn-failure purge etc.), which must not be reverted.

## Verified findings
- PR #158 (dogfood/publish-hydra → development): state CLOSED, mergedAt null, closedAt 2026-10-02T09:25:09Z, no comments — closed without merge, 13m after PR #148 merged (09:12:30Z). The wedge is resolved differently than the bead assumed: #148 merged an earlier tip; the 8 local commits were never published.
- Unlanded content verified: `git cat-file -e origin/development:internal/core/disk_cleanup.go` → NOT LANDED. Feature commit a4d40f4 (diskCleanup guard + tests + docs) plus 4 docs commits are the unique delta.
- Wholesale merge REJECTED as strategy: `git diff origin/development..HEAD` shows D internal/core/spawn_failure_purge_test.go and M service_starter.go −65 — merging HEAD would revert PR #147 landed work.
- Disk: `df -h /` → 192G/451G = 45% (watermark 85%) — the 99% claim is stale; no systemd-tmpfiles cleanup needed; diskCleanup guard itself still worth landing (prevents the class).
- Code-change suggestion in bead (single human-action bead for 'waiting on human merge' class instead of re-firing per cycle): recorded as proposal in triage doc — dedupe key PR number + base; SupersededPRDetector pattern (mvxcx precedent) shows the check "PR closed unmerged + newer publish PR merged" resolves most of these mechanically before filing.

## Verification (commands run + pass/fail)
- `gh pr view 158` — CLOSED/mergeable MERGEABLE/mergedAt null. PASS (evidence).
- `gh pr list --state merged` — #148 merged 09:12:30Z, #147 08:52:06Z. PASS.
- `git cat-file -e origin/development:internal/core/disk_cleanup.go` — absent. PASS.
- `git cherry-pick a4d40f4 b1dfbde ccd9f54 ffe0f4e bbb1bc5` onto origin/development — all clean (no conflicts). PASS.
- `go build ./internal/core/ ./internal/pan/` — PASS.
- `go test ./internal/core/ -run TestDiskCleanupGuard -count=1` — PASS (ok).
- `df -h /` — 45%, watermark not breached. PASS.
- Main checkout incident: cherry-pick briefly ran in /opt/dev-tools/components/hydra; branch restored to development @79507af, `git status` clean — VERIFIED, no residue.

## Open edges for the next phase
- Proposal for follow-up implement bead: publisher-wedge 'waiting on human merge' anomaly should open ONE human-action bead keyed by PR number (dedupe + auto-resolve when PR merges or a newer publish PR lands), instead of re-firing each digest cycle.
