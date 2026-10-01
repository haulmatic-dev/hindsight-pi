# Publisher wedge triage — hydra-4yuw (2026-10-01)

## Task
Publisher wedge on hydra: push to `haulmatic-dev/hydra` fails "remote: Repository
not found." (exit 128) for 3h8m; fleet disk at 99%.

## Verified findings

### Disk — RESOLVED
`df -h /` during this bead: 177G used / 451G (41%), far below the 85% watermark.
The 99% figure in the bead description is stale.

### The not-found wedge — repo/credential flap, human action still needed
Evidence (events `/opt/dev-tools/var/hydra/events/2026-10-01.jsonl`, 2026-10-01 UTC):

- 2,372 events carrying "Repository not found" against `https://github.com/haulmatic-dev/hydra.git`,
  first 06:03:21, last 14:26:04 (8h23m). All project_path `/opt/dev-tools/components/hydra`.
  Almost all (2,345) are the `dogfood/publish-hydra` publish tick; the rest are
  per-bead PR-first closes (gh7j, g0tb, tb3j, j2j7, 0qgi, y1g5).
- A push DID succeed at 13:23:35Z (`pushed_at` on the repo) — failures were
  intermittent, not constant, consistent with a repo transfer/rename in progress
  on GitHub (a transfer makes the repo briefly 404 under both names).

Credential/repo matrix probed live with buddhi's PAT (`github_token_owner_user:
buddhi`, daemon fallback `~/.git-credentials` — same token, verified identical):

| Probe | Result |
|---|---|
| `GET /repos/haulmatic-dev/hydra` (API, buddhi token) | 200, `push:true admin:true`, full_name unchanged (not a rename redirect) |
| `git ls-remote haulmatic-dev/hydra` | works; development = `a36ee76` (3 commits not on the other copy) |
| `git push --dry-run haulmatic-dev/hydra` | auth OK (non-FF hint only) |
| `GET /repos/haulmatic-devteams/hydra` (API) | 404 for buddhi token |
| `git push --dry-run haulmatic-devteams/hydra` | "remote: Repository not found." |
| earlier in session: `gh api repos/haulmatic-devteams/hydra` | 200 (flapped to 404 minutes later) |

Two org copies of hydra exist and DIVERGED: `haulmatic-dev/hydra` development
tip `a36ee76` (P0 spawn-funnel fixes, PR #458) vs the copy a parallel fetch saw
under `haulmatic-devteams` with 8 commits not in `a36ee76` (incl. the gh7j
integration + hydra-37d9 handoff). Common ancestor `9632d6d`.

Remotes are being live-edited by parallel remediation (main checkout origin
flipped `haulmatic-dev` → `haulmatic-devteams` → `haulmatic-dev` within this
session; parallel bead hydra-37d9 landed triage commits on the devteams copy).
This bead did NOT touch the main checkout remote (outside worktree scope).

## Human action required
1. Decide the canonical repo/org once and END the flap: either complete the
   transfer to `haulmatic-devteams` (and grant buddhi's PAT / the daemon token
   access there) or keep `haulmatic-dev/hydra` and drop the new org.
2. Reconcile the diverged development branches (8 vs 3 commits, common base
   `9632d6d`) before the next integration tick — otherwise one side's history
   is silently dropped by force pushes.
3. Repoint main checkout + worker worktree remotes to the canonical URL once.

## Code change shipped here
`ClassifyPushFailure` gains `NOT_FOUND` (phrases: "repository not found",
"fatal: repository", "does not appear to be a git repository", "not a git
repository"), routed to PARK_LOCAL_LOUD with its own class in
`publish_path_classified` events — so future flaps are distinguishable from
transient network failures without human log grepping.
