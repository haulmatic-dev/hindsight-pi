# task handoff

## What changed (files + one line each)
- docs/discovery/integration-wedge-triage-mjws.md: full triage — root cause of the 14+-bead fleet-wide integration-exhausted wedge (duplicate `gh pr create` "already exists" failures against a daemon binary 37 min older than the landed fix), remedy (staged drain-restart deploy), park-clearing guidance.
- CHANGELOG.md: Unreleased entry recording the triage + deploy.

## Verification (commands run + pass/fail)
- Event log /opt/dev-tools/var/hydra/events/2026-09-29.jsonl 15:48–15:51 UTC: all integration-exhausted/integration-failed events share one verbatim failure — `pr merge: create PR: gh pr create: exit status 1 … a pull request for branch "dogfood/<id>" into branch "<base>" already exists: <URL>` (hydra: y7p9/242b; srv-telematics: hig8/0po4/srch/f10l/5gq/dkqc/awtj/otae/qj74/38ot/04jq/i6zt). PASS (shared root confirmed).
- Deployed daemon: `/opt/dev-tools/var/hydra/bin/hydra` mtime + systemd ExecMainStartTimestamp both 14:48:40 UTC; bwyn fix `3d6b9ef` landed 15:25:05 UTC on origin/development → daemon ran pre-fix code during the burst. PASS (stale deploy proven).
- `isPRAlreadyExists` + integration_gate present in worktree (internal/core/publisher.go, internal/core/integration_gate.go); `go build ./...` PASS; `go test ./internal/core/ -run 'TestIsPRAlreadyExists|TestPublishAfterIntegrate_PRAlreadyExists|TestIntegrationGate' -count=1` PASS.
- Staged binaries carry markers (`existing PR for`, `a pull request for branch`) — strings hits 1/1 each.
- Overseer paused (POST /api/v1/overseer/pause → 200 {"status":"paused"}); drain-restart v2 staged+launched (pid 1310541), counts live workers via /proc/*/exe readlink (pgrep -f over-counts: matches prompt text inside live pi sessions), swaps via `.new`+`mv` (direct cp → "Text file busy"), restarts, resumes. Post-restart proof pending (runs after this worker exits).

## Open edges for the next phase
- Confirm deploy: `tail /var/log/hydra/drain-restart-20260929-mjws.log` (expect "resumed, DONE") + `scripts/verify-deploy.sh "$(date -u -d '1 hour ago' +%s)" "existing PR for"`.
- Then clear parks: Pan `repair:clear-hard-park` / operator unpark — but superseded-branch beads (see docs/discovery/integration-wedge-triage-10ka.md and siblings) must stay abandoned-with-summary, not cleared.
- Tier B agentic conflict resolution remains unimplemented (separate bead).
