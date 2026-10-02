# bug handoff (hydra-ns8y)

## What changed (files + one line each)
- docs/discovery/preflight-park-triage-ns8y.md: triage of webapp-nipuna credential-preflight auto-park — park verified CORRECT (repo does not exist under any token-visible org), no code change.
- CHANGELOG.md: Documentation entry under Unreleased.

## Verified findings (webapp-nipuna park)

- Bead: webapp-nipuna auto-parked 2026-10-01 22:04Z: "GitHub credential failed repo-level preflight probe".
- Origin URL of /home/nipuna.j/projects/webapp: `https://github.com/haulmatic-dev/webapp.git` — old org after the org-rename flap.
- nipuna.j token (/home/nipuna.j/.hydra/.github-token) is VALID: `/user` → login `haulmatic-devteams`, 200.
- Probe results with that token:
  - `/repos/haulmatic-dev/webapp` → 404
  - `/repos/haulmatic-devteams/webapp` → 404
  - `git ls-remote` on both URLs → "Repository not found."
- Repo enumeration (token-visible): haulmatic-dev org = traccar, todo-app-test-team-01-*, hindsight-pi (no webapp); HMT-Dev-Teams org = fms-frontend, fms-backend; user repos = hydra, hydra-governor-livefire, tms-mock-server, fms-backend, fms-frontend.
- Conclusion: NO webapp repo exists anywhere the token can see. This is the NOT_FOUND class (same as hydra-gh7j / hydra-4yuw), not a token-scope bug. Fixing/remotepointing cannot help — the repo itself is gone or renamed to something the token cannot see.
- Disposition: PARK STANDS. Do NOT unpark (unpark → immediate re-fail, another NOT_FOUND). Human action required: locate the webapp repo's current slug (org rename flap moved haulmatic-dev → haulmatic-devteams; webapp may have been deleted or moved outside this identity's reach) and either restore access for nipuna.j or point the project at the correct repo.

## Verification (commands run + pass/fail)
- `git -C /home/nipuna.j/projects/webapp remote -v` — origin = haulmatic-dev/webapp. PASS.
- `curl /user` with nipuna token → 200, login haulmatic-devteams. PASS (token valid).
- `curl /repos/{haulmatic-dev,haulmatic-devteams}/webapp` → both 404. PASS.
- `git ls-remote` both URLs → "Repository not found." PASS.
- Org + user repo enumeration → no webapp repo. PASS.

## Open edges for the next phase
- Human/operator: determine where webapp went (deleted? moved to another org not visible to nipuna.j?). If a new slug exists and nipuna should own it, grant the PAT access to that repo, fix the origin URL, then unpark via operator API.
- ajantha's webapp + srv-smart-engine parks same minute are SEPARATE and correct (his PAT is 401-dead — operator T3 renewal).
