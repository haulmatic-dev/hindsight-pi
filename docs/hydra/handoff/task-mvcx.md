# discovery handoff (hydra-mvcx)

## What changed (files + one line each)
- docs/hydra/handoff/task-mvcx.md: this handoff (triage/ops bead; no code change — deliverable was PR #34 disposition + live-store closure).

## Verified outcome (PR #34 publisher wedge)

Task: PR #34 needs human merge to unblock hydra publisher wedge.

Disposition: PR #34 CLOSED as superseded duplicate — no human merge needed.

- PR #34 (dogfood/publish-hydra → development, opened 2026-10-01T19:27Z) carried 3 files:
  CHANGELOG.md fdd9 entry, docs/discovery/publish-target-repo-not-found-fdd9.md,
  docs/hydra/handoff/task-fdd9.md.
- All 3 already landed on origin/development via PR #29 (dogfood/fdd9-land, merge 7ad0d2e):
  both fdd9 doc blobs identical (git diff between refs shows no fdd9 entries), CHANGELOG
  entry present (wording variant only).
- `git merge-tree --write-tree origin/development origin/dogfood/publish-hydra` — rc=0 clean;
  GitHub mergeStateStatus reported DIRTY/CONFLICTING (stale/inconsistent — local merge-tree
  over fetched refs authoritative).
- Actions: `gh pr close 34 --comment <supersession note>` ✓ CLOSED 2026-10-01T19:35:15Z;
  `git push origin --delete dogfood/publish-hydra` ✓ deleted.
- Live store: closed hydra-6h33, hydra-oqnl, hydra-mvcx with supersession/resolution reasons (all ✓).
- Disk: `df -h /` = 179G/451G = **42%** — the bead's 99% claim is stale; no systemd-tmpfiles
  cleanup required. Watermark trigger not active.

## Open edges for the next phase
- If publisher re-reports "waiting on human merge" for another PR, check duplicate/supersession
  FIRST (cat-file + merge-tree vs origin/development) before treating as human-gate.
- GitHub DIRTY-vs-local-clean mismatch worth watching; may reflect GitHub merge-base staleness.

## Verification (commands run + pass/fail)
- gh pr view 34 (pre): state OPEN, mergeable CONFLICTING, mergeStateStatus DIRTY. PASS.
- git cat-file -e origin/development:<both fdd9 docs> — both exist (landed). PASS.
- git diff origin/development origin/dogfood/publish-hydra --stat — no fdd9 doc diffs → blobs identical. PASS.
- git merge-tree --write-tree origin/development origin/dogfood/publish-hydra — rc=0. PASS.
- gh pr close 34 → ✓ Closed; gh pr view 34 → state CLOSED. PASS.
- git push origin --delete dogfood/publish-hydra → deleted. PASS.
- br close hydra-6h33 / hydra-oqnl / hydra-mvcx → ✓ ×3. PASS.
- df -h / → 42%. PASS.
