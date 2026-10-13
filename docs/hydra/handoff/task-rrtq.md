# feature handoff (hydra-rrtq)

## What changed (files + one line each)
- internal/core/pr_aging.go (new): `PRWaitingRecord` + `PRWaitingLedger` (JSON ledger at `$HYDRA_HOME/pr-waiting.json`, upsert-by-bead, nil/inert without HYDRA_HOME, corrupt file fails open to empty) + `PRAgingScanner` — weekly cadence (`PRAgingInterval` 7d) + once at startup; probes each recorded PR via `gh pr view <url> --json state` (15s timeout, GH_TOKEN = owner identity token via `resolvePublishIdentityFor`), removes MERGED/CLOSED records, and for PRs still open past `pr_aging_days` emits one typed `pr_aging_digest` event per owner with per-PR `class`: `stuck-auto-merge` (repo allowlisted via `Config.IsPRAutoMergeRepo` = output.pr_auto_merge_repos / deploy.repo_path — automated merge wedged) or `t2-human-merge-queue` (designed Tier-2 stop state). Probe/identity/ledger failures fail open and only skip that record's entry for the week.
- internal/core/bead_closer.go: `SetPRWaitingRecorder` + `recordPRWaitingCapture` — both `PRWaitingError` close sites (publishAfterIntegrate PR-merge path + PR-first close path) now record {bead, project, owner, URL, since} into the ledger when a close leaves the PR open waiting on human merge.
- internal/core/overseer.go: `prWaiting`/`prAging` fields; constructed when HYDRA_HOME set and `overseer.pr_aging_days > 0` (default 7); recorder wired onto beadCloser.
- internal/core/config.go: `overseer.pr_aging_days` (default `PRAgingThresholdDays` = 7, 0 disables).
- internal/core/service_starter.go: Start/Stop wiring alongside escalation TTL / integration sweep.
- internal/core/pr_aging_test.go: 7 tests — ledger upsert/persist/remove, inert-nil-safe, corrupt-file fail-open, digest classification matrix (aged human-queue + aged auto-merge + young skipped + merged removed), probe-failure fail-open (record retained, no digest), disabled nil-safe, closer recorder nil-safety.
- CHANGELOG.md, docs/architecture/shepherd.md, hydra-config.yaml.example: operator docs for the knob and `pr_aging_digest` event.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l` on all touched files — clean.
- `go test ./internal/core/ -run 'TestPRWaiting|TestPRAging|TestBeadCloser_SetPRWaitingRecorder' -count=1` — PASS (7/7).
- Commit verified via `git show --stat HEAD` (9 files, +559).

- Gate-failure fix: `SetPRWaitingRecorder` panicked on nil `*BeadCloser` receiver in tests that build an Overseer without a closer (graph TestPauseOverseer_Success, api TestGetGraphMetricsHandler, core TestComplianceCycleManager_areAllBeadsClosed) — added nil guard; scoped reruns PASS.

## Open edges for the next phase
- Ledger only captures PRs from closes that go through `PRWaitingError` on this branch — historical Tier-2 waiting PRs (closed before this feature) are not backfilled; a one-shot bootstrap could scan open dogfood/* PRs per repo into the ledger.
- The digest is event-stream + log only (no Pan-digest integration); if Pan should act on `stuck-auto-merge`, add it to collectHealthDigest anomaly inputs later.
- `getProjects` field on the scanner is currently unused (kept for future project-name attribution in the digest payload).
