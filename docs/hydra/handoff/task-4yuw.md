# discovery handoff (hydra-4yuw)

## What changed (files + one line each)
- internal/core/publish_path.go: new `ClassNotFound` ("NOT_FOUND") push-failure class — "Repository not found" / "fatal: repository" / "not a git repository" stderr routes to PARK_LOCAL_LOUD with its own class in `publish_path_classified` events instead of generic UNKNOWN.
- internal/core/publish_path_test.go: 2 classifier cases (repo-not-found, not-a-git-repo) + 1 decision row (ClassNotFound → ActParkLocalLoud).
- docs/discovery/publisher-wedge-triage-4yuw.md: full triage of the hydra publish wedge + live credential/repo probe matrix + completed reconciliation record.
- CHANGELOG.md: Added entry under Unreleased.
- Diverged-development reconciliation landed via plumbing union merge 4a0a5a1: PR #462 (haulmatic-dev/hydra, dev → 8f08116) + PR #22 (canonical haulmatic-devteams/hydra, dev tip → 37e5680).

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'TestClassifyPushFailure|TestResolvePublishPath' -count=1` — PASS (ok, incl. 3 new cases).
- `gofmt -l` on changed files — clean (after formatting the test file).
- Live probes: `gh api`/`git ls-remote`/`push --dry-run` matrix — haulmatic-dev/hydra 200 push:true with buddhi PAT; haulmatic-devteams/hydra 404 for the same token; later daemon token rotated and reaches devteams (admin+push). PASS.
- Disk: `df -h /` = 177G/451G (41%) — the 99% watermark condition in the bead description is stale. PASS.
- Reconciliation: `git merge-tree --write-tree a36ee76d a5e45cc6` rc=0 clean; `git merge-base --is-ancestor a36ee76d 37e56803 && a5e45cc6 37e56803` → both ancestors of canonical dev tip (VERIFIED); PR #462 and PR #22 both `MERGED` via `gh pr view`; `git push --dry-run dogfood/publish-hydra` to canonical → "Everything up-to-date".

## Open edges for the next phase
- Reconciliation COMPLETED this bead: union merge landed on canonical haulmatic-devteams/hydra (PR #22, dev tip 37e5680 contains both a36ee76 + a5e45cc lineages; old copy reconciled via PR #462, dev 8f08116). Publish path verified live (dry-run push OK).
- Human cleanup: retire stale haulmatic-dev/hydra mirror (now a superset, no content risk) + re-scope buddhi's PAT if old org is dropped.
- 2,371 not-found events 06:03–14:26 UTC 2026-10-01: with NOT_FOUND classification, the next flap will surface as a distinct class in `publish_path_classified` events for automatic alerting.
