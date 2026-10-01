# Publish target verification: haulmatic-devteams/hydra "Repository not found" (hydra-fdd9)

## Symptom
Pushes to https://github.com/haulmatic-devteams/hydra.git on branch `dogfood/publish-hydra`
failed with `remote: Repository not found. fatal: repository ... not found` (exit 128).

## Root cause (already fixed)
GitHub org rename flap: pushes/PRs named `haulmatic-dev/hydra` (no "teams") after the
org was renamed to `haulmatic-devteams`. Verified and remediated in the hydra-4yuw
strand (landed via PR #23): daemon token rotated to a fine-grained PAT scoped to
`haulmatic-devteams` with push access.

## Verification (this bead, 2026-10-01)
- `git ls-remote origin HEAD` → rc=0 (d218837...), origin = `haulmatic-devteams/hydra.git`.
- `curl /repos/haulmatic-devteams/hydra` with `$HYDRA_HOME/.git-credentials` token →
  HTTP 200, permissions `{admin, maintain, push, pull: true}`.
- `git push --dry-run origin hydra/wt-hydra-fdd9:dogfood/hydra-fdd9` → accepted.
- `git push --dry-run origin hydra/wt-hydra-fdd9:dogfood/publish-hydra` → accepted
  (`882b0b7..6e2969d`), i.e. the exact branch named in the failure now pushes.

## Disposition
No code change. The publish target exists and the current credentials can push to it.
Should the error recur, verify (1) repo still exists under `haulmatic-devteams`,
(2) the PAT in `/home/<owner>/.hydra/.github-token` / daemon `.git-credentials`
includes the repo in its repository list, (3) no further org/rename flap. The
publish-path classifier (internal/core/publish_path.go, `ClassNotFound` → PARK_LOCAL_LOUD)
already fails loud for this class.
