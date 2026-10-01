# task handoff (hydra-m372)

## What changed (files + one line each)
- internal/core/publisher.go: pr-merge failure branch now classifies via ClassifyPushFailure, emits `publish_path_classified` (class NOT_FOUND for repository-not-found), and surfaces a loud credential-class message in event detail, activity log, and returned reason.
- internal/core/publisher_test.go: TestPublishRepo_PRMergeNotFound_ClassifiedCredential — real-git repo + fake gh error "remote: Repository not found." → classification event + credential-class reason pinned.
- docs/discovery/publisher-wedge-triage-m372.md: full triage (root cause, credential probe matrix, disposition).
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go test ./internal/core/ -run TestPublishRepo_PRMergeNotFound -count=1` — PASS (3 passed).
- `go vet ./internal/core/` — PASS; `gofmt -l` changed files — clean.
- `go test ./internal/core/ -count=1` — PASS (ok, 68.8s).
- Commit 3d629b6 verified via `git show --stat` (4 files, +92/−3).
- Live probes: daemon token → haulmatic-devteams/hydra API 200, ls-remote OK, push --dry-run OK; `/home/buddhi/.hydra/.github-token` → 200 on haulmatic-dev/hydra, **404 on haulmatic-devteams/hydra** (pre-rename PAT). Events: 91 "Repository not found" push failures 2026-10-01, latest 18:26 UTC; `publish_identity_resolved` shows hydra project publishes via owner-pat-file (buddhi).

## Open edges for the next phase
- Human action: re-create buddhi's fine-grained PAT scoped to haulmatic-devteams (push + PR) and update `/home/buddhi/.hydra/.github-token`; publishes then recover with no code change.
- Disk-99% claim stale: host at 41% (174G/451G). No tmpfiles action needed.
- Retry suppression for NOT_FOUND (skip publish ticks entirely until credential repaired) deliberately not added — loud classification + existing integration gate cooldowns make the loop visible; revisit if the loop recurs post-fix.
