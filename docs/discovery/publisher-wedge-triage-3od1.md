# Publisher wedge triage — PR #158 (hydra-3od1)

**Bead:** hydra-3od1 · **Date:** 2026-10-02 · **Disposition:** VERIFIED-RESOLVED, no action needed.

## Claim vs reality

Bead claimed: hydra work unpublished 38m because PR
[haulmatic-devteams/hydra#158](https://github.com/haulmatic-devteams/hydra/pull/158)
awaited a human merge.

Verified state:

- PR #158 (`dogfood/publish-hydra` → `development`) is `CLOSED` with `mergedAt: null`
  (closed 2026-10-02T09:25:09Z) — closed unmerged, not pending.
- Every commit carried by the PR landed on origin/development via other PRs:
  `git merge-base --is-ancestor <oid> origin/development` → LANDED for all 8
  (b1dfbdee, a4d40f46, ccd9f547, ffe0f4e1, bbb1bc58, 0fec7f8d, 1d8bbc2b, 79507afa).
- origin/development tip is `5ee5ae9` "Merge pull request #162 from
  dogfood/publish-158", committed 2026-10-02T09:35:07Z — 10 minutes after the PR
  closed. PR #161 (`dogfood/cherry-158-continue`) published the unlanded strand
  via cherry-pick; the `docs: PR #158 publisher-wedge triage` commit (14c8dfc)
  already records this.

## Conclusion

No human merge action remains: the publication wedge self-resolved. The
premise ("38m unpublished") predates the resolution. No code change, no merge
automation.

## Open PRs still referencing the wedge

PRs 155–159, 163 are pan-filed duplicate filings of the stale PR #148/#158
wedge and should be closed as superseded duplicates.
