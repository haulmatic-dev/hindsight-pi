# task handoff

## What changed (files + one line each)
- docs/discovery/integration-wedge-triage-37d9.md — triage of hydra-gh7j integration-exhausted: root cause = remote repo `haulmatic-dev/hydra` unreachable since ~00:46 UTC 2026-10-01 (exit 128 "remote: Repository not found."), not retry capacity; proposal (a) rejected, (b) adopted.
- CHANGELOG.md — Documentation entry under Unreleased.
- Live-store triage comments added to hydra-gh7j and hydra-37d9 (br comments add, ✓). Park HELD; no code change (discovery bead).

## Verification (commands run + pass/fail)
- `git merge-tree --write-tree origin/development hydra/wt-hydra-gh7j` — rc=0, clean. PASS.
- `git log --right-only origin/development...hydra/wt-hydra-gh7j` — 3 commits (ace73e5, 75e4f3a, 670baa0); `remoteSymrefDefault` absent on origin/development → unlanded, not superseded. PASS.
- gates/hydra/hydra-gh7j/test.log — all packages `ok`, `grep -c FAIL` = 0. PASS.
- GitHub API (daemon PAT, haulmatic-devteams): `/repos/haulmatic-dev/hydra` 404; `/orgs/haulmatic-dev/repos` lacks hydra (only traccar, todo-app-test-team-01-*, hindsight-pi); `HMT-Dev-Teams` has only fms-frontend/fms-backend. PASS.
- /var/log/hydra/hydra-error.log — 44 `Repository not found` hits 00:46:20–06:25:01 UTC, 9 beads (fmsfe-d1b/q70/53g/d68/hd7/4co/49s, hydra-gh7j ×4, hydra-y1g5). PASS.
- Last successful landing a36ee76 on origin/development 22:24 UTC 09-30 → outage window identified. PASS.
- `br comments add` on both beads — ✓. PASS.

## Open edges for the next phase
- Human-only: restore/relocate `haulmatic-dev/hydra` (repo deleted/renamed/moved between 22:24 UTC 09-30 and 00:46 UTC 10-01), then unpark; IntegrationRetest (max_attempts=5, 1h) + fast-path notifier will retry automatically.
- On remote return: cherry-pick `ace73e5` + `75e4f3a` onto origin/development — do NOT merge hydra/wt-hydra-gh7j wholesale (branch carries stale-integration deletions mirroring the already-resolved ad20 semantic conflict; integration must keep landed spawn_preflight.go/epic_close_guard.go).
- Sibling beads fmsfe-d1b/q70/53g/d68/hd7/4co/49s + hydra-y1g5 share this cause — do not triage as independent wedges. NOTE: fms-frontend remote is alive, so those fmsfe failures at 04:17 UTC may have been transient overlap with the same outage window or a second infra issue; re-verify each before closing.
- Push of THIS worktree's triage commit is blocked by the same 404 remote — work is committed locally on `hydra/wt-hydra-37d9` and must ride the next successful integration.
