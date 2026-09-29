# discovery handoff

## What changed (files + one line each)
- docs/discovery/rebase-conflict-triage-pmed.md — full triage of the hydra-pmed push-pending rebase conflict in internal/core/publisher.go: root cause, exact resolution steps, post-resolution checks.
- CHANGELOG.md — Documentation entry under Unreleased.
- No code change (task explicitly forbids automating the conflict resolution; discovery/triage bead).

## Verified findings
- Repo state moved past the bead description: no rebase/cherry-pick in progress; `development [ahead 3, behind 8]`, HEAD 9a905d3 (merge of d6c5752 + uewg docs), merge-base 33e1967.
- `git merge-tree --write-tree origin/development 9a905d3` → CONFLICT in internal/core/publisher.go ONLY; all other files auto-merge.
- Conflict hunks are comment-only (3), BUT the auto-merge silently reintroduces `prURLFromError` + the URL-from-error reuse that origin commit ecc6cb5 deliberately removed ("restore fail-closed existing-PR resolver after zpyj integration collision") — origin is net-unchanged vs base in that region, so d6c5752's addition wins textually. Trial merge confirmed 3 `prURLFromError` occurrences in the merged file.
- Correct resolution: take origin comment sides in the hunks, DELETE prURLFromError (call block + function), keep resolveExistingPR fallback; then rebase --continue / complete merge and push via PR route (development is GH013-protected).
- Why human-only: auto-merge textually succeeds and would regress fail-closed semantics (risk of merging a CLOSED PR).

## Open edges for the next phase
- Human must perform the resolution in /opt/dev-tools/components/hydra per the triage doc; after landing, verify `go test ./internal/core/ -run 'TestIsPRAlreadyExists|TestResolveOrCreatePR'` passes on the merged result.
- Publisher auto-merge hazard (modify/add vs deliberate revert reintroducing reverted code) is a general pattern — consider a gate that rejects merges reintroducing symbols a commit message says were reverted; separate bead.
