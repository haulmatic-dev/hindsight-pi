# discovery handoff (hydra-3od1)

## What changed (files + one line each)
- docs/discovery/publisher-wedge-triage-3od1.md: triage — PR #158 closed unmerged, content already landed via cherry-pick strand; no code change.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.

## Verified findings (PR #158 publisher wedge)
- `gh pr view 158 -R haulmatic-devteams/hydra` — `state: CLOSED`, `mergedAt: null`, `mergeStateStatus: UNSTABLE`, closed 2026-10-02T09:25:09Z. The PR was closed unmerged, not left open.
- All 8 PR #158 commits (b1dfbdee, a4d40f46, ccd9f547, ffe0f4e1, bbb1bc58, 0fec7f8d, 1d8bbc2b, 79507afa) are ancestors of origin/development — `git merge-base --is-ancestor` LANDED ×8.
- Development tip 5ee5ae9 "Merge pull request #162 from dogfood/publish-158" (2026-10-02T09:35:07Z); PR #161 (dogfood/cherry-158-continue) landed the unlanded strand via cherry-pick. Work published 10 minutes after the PR closed.
- origin/development tip on GitHub = 5ee5ae9 — publication current.
- Open PRs 155–163 are pan-filed duplicate filings of the stale PR #148/#158 wedge; disposition = duplicate triage comments / supersession closure.
- The "38m unpublished" premise is stale: wedge self-resolved before this bead's start.

## Open edges for the next phase
- None for the wedge. Optional hygiene: close duplicate PR-filing beads (155–159, 163) as superseded.
