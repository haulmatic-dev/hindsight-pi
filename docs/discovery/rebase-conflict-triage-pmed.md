# Rebase conflict triage — hydra-pmed (push-pending, internal/core/publisher.go)

## State at inspection (VERIFIED, 2026-09-29, /opt/dev-tools/components/hydra)

- `git status`: `development...origin/development [ahead 3, behind 8]`. No rebase or
  cherry-pick is currently in progress — the mid-rebase state named in the bead
  description was already cleared; local HEAD is now `9a905d3` (merge of
  `d6c5752` + `979ba44` uewg docs), merge-base with origin = `33e1967`.
- `git merge-tree --write-tree origin/development 9a905d3` → rc=1,
  CONFLICT in `internal/core/publisher.go` only (everything else auto-merges,
  including CHANGELOG.md).

## Root cause

`d6c5752` ("hydra: integrate hydra/wt-hydra-6zwl — publisher PR-reuse
reconciliation") added `prURLFromError` + an early-return reuse of the URL
embedded in `gh pr create` "already exists" errors.

Independently, origin/development landed `ecc6cb5` ("fix(publisher): restore
fail-closed existing-PR resolver after zpyj integration collision") which
DELIBERATELY REMOVED that same shortcut (76bf0c5 added it, ecc6cb5 removed it)
in favor of `resolveExistingPR` (gh pr view, state-checked, open/merged only).

Because origin's net change vs merge-base `33e1967` in that region is
"unchanged", a textual merge silently KEEPS `d6c5752`'s re-addition of
`prURLFromError` — reintroducing the exact behavior origin's fail-closed fix
reverted. Verified: a trial merge of `origin/development` + `9a905d3` produced
a merged publisher.go containing `prURLFromError` (3 occurrences: call site at
~line 684, function definition at ~line 696).

The three textual conflict hunks are comment-only:
1. Two error-path comment blocks above `return "", fmt.Errorf("pr merge: create PR: %w", err)`
   in `PublishViaPR` and `publishBranchViaPRCreate` — both sides rewrote the same
   3-line comment.
2. One comment above `return publishBranchViaPRCreate(...)` in
   `publishBranchViaPROnce` (local side added it; origin has none).

## Required human resolution (do NOT take the auto-merge result)

In the conflict, resolve as follows:

1. Comment hunks: take the **origin/development side** ("resolveOrCreatePR
   already resolved-and-returned an existing open/merged PR; anything surfacing
   here (no PR found, CLOSED PR, real create error) is a genuine failure —
   fail-closed, do not proceed to merge."). Drop the empty-hunk local comment in
   `publishBranchViaPROnce`.
2. **Remove `prURLFromError` entirely**: delete the call block inside
   `resolveOrCreatePR` (the `if u := prURLFromError(err); u != "" { ... }` with
   its comment) and the function definition (`prURLFromError extracts the first
   http(s) URL ...` through its closing brace). Keep the
   `resolveExistingPR(repoPath, head, token)` fallback.
   This restores origin's fail-closed semantics; `strings` remains used
   elsewhere in the file (verify with `go build ./internal/core/`).
3. `git add internal/core/publisher.go && git rebase --continue` (or complete the
   merge) → the local branch will then be a fast-forward over origin except for
   the docs commits (`979ba44` uewg dedup, handoffs) — push `development`.
4. Direct push to `development` is protected (GH013): use the established
   PR route — push `dogfood/publish-hydra`-style branch and
   `gh pr merge --admin`, or have a human with push rights push directly.

## Why human-only

Publisher reconciliation logic; the auto-merge textually succeeds and would
silently regress the fail-closed PR resolver (risk: merging a CLOSED PR —
exactly what ecc6cb5 fixed).

## Post-resolution sanity

```bash
go build ./...
go test ./internal/core/ -run 'TestIsPRAlreadyExists|TestResolveOrCreatePR|TestPublishViaPR' -count=1
git log --oneline origin/development..development   # should show only intended local commits
```
