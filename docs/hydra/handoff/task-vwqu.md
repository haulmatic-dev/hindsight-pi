# task handoff (hydra-vwqu)

## What changed (files + one line each)
- docs/hydra/handoff/task-vwqu.md: this document (human-integration triage of hydra-4yuw; no code change in this worktree — the work itself landed upstream via PR #23).
- CHANGELOG.md: Documentation entry under Unreleased recording the disposition.

## Verified findings (hydra-4yuw integration state)

Task: bead hydra-4yuw ("Publisher wedge on hydra: repo 'haulmatic-dev/hydra' not found on push")
hard-parked with integration-exhausted; human review requested to resolve or abandon the integration.

Disposition: INTEGRATION COMPLETE — work landed via PR #23; bead closed as landed.

- Bead body described: git push to github.com/haulmatic-dev/hydra fails "remote: Repository not found."
  (exit 128), plus a stale disk-99% claim.
- Root cause of the push failures: org rename flap — origin is
  `https://github.com/haulmatic-devteams/hydra.git`, but push/PR attempts named
  `haulmatic-dev/hydra` (no "teams") after 2026-09-29 15:13 UTC (hydra-fan1 event).
  Anonymous and tokened ls-remote to haulmatic-dev/hydra both return
  "Repository not found" (renamed org); tokened access to haulmatic-devteams/hydra
  works (API permissions: admin/maintain/push/pull = true).
- The daemon credential (/opt/dev-tools/var/hydra/.git-credentials token) is valid for the
  renamed org: `curl -H "Authorization: token …" /repos/haulmatic-devteams/hydra` → 200 with push:true.
- Disk: `df -h /` = 451G total, 170G used, 40% — the bead's 99%-disk claim is stale (resolved earlier).
- Strand content: hydra/wt-hydra-4yuw carries 2 unique commits —
  e68a4a8 "fix(publisher): classify repository-not-found push failures as NOT_FOUND"
  (the very code change the bead proposed) and 5c4baad (org-transfer reconciliation docs).
- Landing evidence: `git merge-tree --write-tree origin/development hydra/wt-hydra-4yuw` → rc=0
  (clean after fetch of fresh origin/development d218837; earlier merge-tree vs stale local
  origin/development at 37e5680 falsely showed −726-line diffs).
- PR #23 (dogfood/hydra-4yuw → development): state=closed, **merged=true,
  mergedAt=2026-10-01T16:34:05Z**, merge commit d218837 = current development tip.
  Squash-merged: 047e1a3 (NOT_FOUND classification) + 0f2f237 (reconciliation docs) both on development.
  Note: PR numbering restarted after org rename (canonical repo now uses low numbers; PR #462/#22 in
  the branch's docs refer to the old-org numbering window).

Live-store actions (in /opt/dev-tools/components/hydra):
- `br doctor migrate-schema recover` — cleared WAL poison warning first (required).
- `br close hydra-4yuw -r "landed: work merged via PR #23 (merge commit d218837 …)"` — ✓ bead CLOSED.

## Verification (commands run + pass/fail)
- `git ls-remote https://x-access-token:$T@github.com/haulmatic-devteams/hydra.git HEAD` — PASS (200).
- `git ls-remote .../haulmatic-dev/hydra.git` — "Repository not found" (org renamed) — confirms root cause.
- `curl -H "Authorization: token $T" /repos/haulmatic-devteams/hydra` → 200, push:true — PASS.
- `git merge-tree --write-tree origin/development hydra/wt-hydra-4yuw` — rc=0, clean — PASS.
- GitHub API: PR #23 merged=true @ 2026-10-01T16:34:05Z, merge_commit d218837 — PASS.
- `br show hydra-4yuw` — CLOSED. PASS.

## Open edges for the next phase
- Stale-URL healing after org renames is not automatic for all clones: worktree origin URLs and
  publish-identity token scopes must be re-checked after a GitHub org rename. The landed
  repository-not-found classification (047e1a3) helps detect, but a heal-the-remote sweep could
  prevent recurrence.
- Delete `origin/dogfood/hydra-4yuw` branch after PR merge if policy requires (still present).
