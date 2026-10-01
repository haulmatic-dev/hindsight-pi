# discovery handoff (hydra-4yuw)

## What changed (files + one line each)
- internal/core/publish_path.go: new `ClassNotFound` ("NOT_FOUND") push-failure class — "Repository not found" / "fatal: repository" / "not a git repository" stderr routes to PARK_LOCAL_LOUD with its own class in `publish_path_classified` events instead of generic UNKNOWN.
- internal/core/publish_path_test.go: 2 classifier cases (repo-not-found, not-a-git-repo) + 1 decision row (ClassNotFound → ActParkLocalLoud).
- docs/discovery/publisher-wedge-triage-4yuw.md: full triage of the hydra publish wedge + live credential/repo probe matrix.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'TestClassifyPushFailure|TestResolvePublishPath' -count=1` — PASS (ok, incl. 3 new cases).
- `gofmt -l` on changed files — clean (after formatting the test file).
- Live probes: `gh api`/`git ls-remote`/`push --dry-run` matrix — haulmatic-dev/hydra 200 push:true with buddhi PAT; haulmatic-devteams/hydra 404 for the same token (details in triage doc). PASS.
- Disk: `df -h /` = 177G/451G (41%) — the 99% watermark condition in the bead description is stale. PASS.

## Open edges for the next phase
- Canonical repo decision + diverged development reconciliation (haulmatic-dev/hydra a36ee76 vs devteams copy 8 commits ahead) is human-only; parallel bead hydra-37d9 is actively remediating and main-checkout remotes flip-flopped mid-session — do NOT touch remotes from a worktree bead.
- 2,371 not-found events 06:03–14:26 UTC 2026-10-01: with NOT_FOUND classification, the next flap will surface as a distinct class in `publish_path_classified` events for automatic alerting.
