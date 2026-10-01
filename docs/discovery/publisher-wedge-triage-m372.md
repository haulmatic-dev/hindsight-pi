# Publisher wedge triage — hydra-m372

Date: 2026-10-01. Bead: hydra-m372 (task: push to haulmatic-devteams/hydra fails "remote: Repository not found", GH013-class; secondary disk-99% claim).

## Verified findings

- Repo path CORRECT and HEALTHY: `GET /repos/haulmatic-devteams/hydra` with the daemon token (`/opt/dev-tools/var/hydra/.git-credentials`) → 200; `git ls-remote` and `git push --dry-run HEAD:refs/heads/dogfood/publish-hydra` both succeed with that token. The daemon-token push path is not broken.
- Actual failing credential: the publish-identity resolver picks the project owner's PAT — event `publish_identity_resolved {"project":"hydra"…}` shows the hydra project resolves via `token_source:"owner-pat-file"`. `/home/buddhi/.hydra/.github-token` returns **200 for haulmatic-dev/hydra but 404 for haulmatic-devteams/hydra** — the token predates the 2026-09-29 org rename (haulmatic-dev → haulmatic-devteams) and lacks visibility into the renamed org.
- 91 `publish_push_rejected` events with "remote: Repository not found." on 2026-10-01, latest 18:26 UTC — ongoing futile retry loop on the pr-merge path.
- Disk: `df -h /` = 174G/451G (41%) — the 99% watermark claim is STALE (already triaged in 4yuw).
- Error-surfacing gap (code): the direct-push path classifies failures via `ClassifyPushFailure` (ClassNotFound landed in task-4yuw / PR #23 chain), but the **pr-merge path** (`PublishRepo` prMerge=true → `publishWithRebaseRetry`) emitted only a generic `publish_push_rejected reason=pr_merge_failed` with no classification — the not-found failures were indistinguishable from ordinary merge failures in the stream.

## Hydra-side fix (this bead)

- `internal/core/publisher.go`: the pr-merge failure branch now classifies the error; ClassNotFound → typed `publish_path_classified` event (class NOT_FOUND) plus a loud credential-class message in the event detail, activity log, and returned reason ("credential-class failure … human must fix the remote URL or the publish credential; retrying cannot succeed").
- Regression test `TestPublishRepo_PRMergeNotFound_ClassifiedCredential` pins the classification + surfacing.

## Disposition

- Root cause is credential-side: buddhi's owner PAT needs re-creation against haulmatic-devteams (fine-grained PAT scoped to the renamed org, push+PR scope). Hydra cannot repair a PAT. Until then the hydra project's pr-merge publishes will park loud instead of looping silently.
