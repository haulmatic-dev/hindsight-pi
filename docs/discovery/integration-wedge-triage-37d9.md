# Integration-exhausted triage — hydra-37d9 / hydra-gh7j (2026-10-01)

## Bead under triage

`hydra-gh7j` ("originDefaultBranch trusts stale refs/remotes/origin/HEAD — fms PRs
targeted main while GitHub default is development") hard-parked with
`integration-exhausted` at 2026-10-01T06:09:03Z
(`consecutive_failures_capped`). Its implementation work is COMPLETE and on disk.

## Candidate work state (all VERIFIED in this worktree)

- Branch `hydra/wt-hydra-gh7j` tip `670baa0`, 3 commits:
  - `ace73e5` fix: resolve integration base from remote truth (`git ls-remote
    --symref origin HEAD`) before stale local `origin/HEAD`
  - `75e4f3a` fix: opportunistically reconcile stale `refs/remotes/origin/HEAD`
    after successful ls-remote symref
  - `670baa0` docs: bug-gh7j handoff
- `git merge-tree --write-tree origin/development hydra/wt-hydra-gh7j` → rc=0,
  zero conflicts.
- Not superseded: `git grep mergeTreeConflicts|remoteSymrefDefault
  origin/development` — `remoteSymrefDefault` absent upstream; feature unlanded.
- Gate log `/opt/dev-tools/var/hydra/gates/hydra/hydra-gh7j/test.log`: all
  packages `ok`, zero `FAIL` lines; build.log and lint.log clean.
- Diff vs origin/development is the +168-line feature plus stale-integration
  deletions (spawn_preflight.go, epic_close_guard.go) — the stale deletions
  mirror strand ad20's already-resolved semantic conflict (see fix-458-merge);
  integration must cherry-pick the two feature commits, not merge the branch
  wholesale.

## Root cause of the exhaustion: remote repository gone, NOT retry capacity

Every integration attempt for `hydra-gh7j` (and siblings) fails identically:

```
pr-first close hydra-gh7j via dogfood/hydra-gh7j → development: pr merge:
push dogfood/hydra-gh7j: git push --force --no-verify ...github.com/haulmatic-dev/hydra.git
hydra/wt-hydra-gh7j:refs/heads/dogfood/hydra-gh7j: exit status 128:
remote: Repository not found.\nfatal: repository 'https://github.com/haulmatic-dev/hydra.git/' not found
```

Evidence:

- `/var/log/hydra/hydra-error.log`: 44 `Repository not found` occurrences,
  00:46:20 → 06:25:01 UTC on 2026-10-01, across 9 beads (fmsfe-d1b/q70/53g/
  d68/hd7/4co/49s, hydra-gh7j ×4, hydra-y1g5). 27 are `IntegrationRetest`
  fetch failures; the rest are `[Integrate]` push failures.
- Last successful push to `haulmatic-dev/hydra`: commit a36ee76 landed on
  `origin/development` 2026-10-01T03:54:41+05:30 (= 22:24 UTC 09-30). Failures
  begin 00:46 UTC 10-01 → the remote was deleted/renamed/moved in that window.
- GitHub API with the daemon PAT (haulmatic-devteams, scopes `admin:org,repo`):
  `GET /repos/haulmatic-dev/hydra` → 404. `GET /orgs/haulmatic-dev/repos` lists
  only traccar, todo-app-test-team-01-{front,back}end, hindsight-pi — no hydra.
  Under users/orgs `HMT-Dev-Teams` only fms-frontend/fms-backend remain. No
  SSO header hint; the repo simply is not reachable by any path from this host
  (https+token, ssh → Permission denied).
- Contrast: `HMT-Dev-Teams/fms-frontend` ls-remote works from this host —
  the outage is specific to the hydra repo's remote location.

## Verdict on the bead's proposals

- **(a) increase retry ceiling / extend wait windows — REJECTED.** The failure
  is deterministic exit-128 "Repository not found": the repo does not exist at
  the configured remote. More attempts or longer waits cannot succeed and
  would burn tokens (the 2026-10-01 fms campaign burned 18.5M tokens this way
  — see hydra-fkui). `integration_exhausted` durable-park is the correct
  outcome for an unreachable remote.
- **(b) surface for senior corrective effort — ADOPTED.** Restoring the remote
  (recreate/move `haulmatic-dev/hydra`, or point the project's
  `output.remote`/origin at the repo's new home) is a human action; once the
  remote is reachable, `IntegrationRetest` + the fast-path notifier will retry
  gh7j automatically (max_attempts=5, 1h interval; `retry-quarantine`/repair
  verbs also available).

## Disposition

1. Do NOT clear the `integration-exhausted` park while the remote is 404 —
   re-triggering integration now just re-exhausts.
2. hydra-gh7j work is intact and integration-ready; when the remote returns,
   cherry-pick `ace73e5` + `75e4f3a` onto `origin/development` (branch
   carries stale-integration deletions; do not merge wholesale).
3. Operator action (human-only): restore or relocate the `haulmatic-dev/hydra`
   GitHub repo, then unpark via `POST /api/v1/projects/:name/unpark` or let
   IntegrationRetest retry.
4. Cross-check: every `Repository not found` bead in the 00:46–06:25 window
   (fmsfe-d1b, fmsfe-q70, fmsfe-53g, fmsfe-d68, fmsfe-hd7, fmsfe-4co,
   fmsfe-49s, hydra-gh7j, hydra-y1g5) shares this single cause; do not triage
   them as independent wedges.

## Verification (commands run in this bead)

- `git merge-tree --write-tree origin/development hydra/wt-hydra-gh7j` → rc=0 (clean).
- `git log --right-only origin/development...hydra/wt-hydra-gh7j` → 3 commits.
- `grep -c FAIL` gate test.log → 0; all packages `ok`.
- GitHub API 404 matrix and org-repo inventory (curl, PAT from
  `$HYDRA_HOME/.git-credentials`).
- Event/log greps quoted above.
