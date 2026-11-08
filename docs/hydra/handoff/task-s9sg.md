# task handoff (hydra-s9sg)

## What changed (files + one line each)
- internal/core/publish_path.go: new `CREDENTIAL` PublishPathClass — invalid/expired-token auth failures ("Invalid username or token", "Password authentication is not supported", "Authentication failed", "permission denied (publickey)") no longer classified TRANSIENT; routes PARK_LOCAL_LOUD like NOT_FOUND but names the credential-rotation remedy.
- internal/core/overseer.go: per-project publish backoff — `publishBackoff` map + `publishBackoffEntry{attempt,next}`; `publishOneRepo` and `publishOneProjectRepos` skip the whole publish tick (including identity resolution) while in cooldown; `notePublishBackoff` (base 2m, exponential, cap 15m) after any non-benign publish failure; cleared on success/benign; nil-safe; in-memory only (daemon restart = one fair retry).
- internal/core/publish_path_test.go: stale-token srv-telematics stderr case → CREDENTIAL; "auth" case moved to CREDENTIAL.
- internal/core/publish_backoff_test.go (new): throttle after failure, per-project scoping, exponential growth, clears-on-success, cap after 30 attempts, nil receiver safety.
- CHANGELOG.md: Fixed entry under Unreleased.

## Root cause (the actual anomaly behind "orphaned in_progress claim")
- Charon orphan recovery IS live and working (3 `charon_orphan_reopened` events 2026-10-02: fmsfe-sn5 00:52, hydra-1p1q 02:45, hydra-vk94 02:47). Orphan-claim reconciliation verified deployed.
- Disk claim stale: `df -h /` = 44% (watermark 85%). PASS.
- Real churn found: 7 in_progress pan-filed P0-anomaly beads in hydra store are duplicate filings of each other (s9sg/1euz/hxaj/wv2e/yz8z/t2e1/qmf7) — the anomaly self-perpetuates because each Pan filing spawns another triage worker.
- Underlying retry storm: `publishProjects` runs every scheduler tick; a persistent publish failure (stale token on srv-telematics classified TRANSIENT; PRWaitingError "waiting on human merge" on fms-frontend; missing remote 'origin' on tiny-greeter) re-attempted full publish + identity resolution every ~48s: 2,709 classified failures + 6,721 `publish_identity_resolved` events in 12h, zero backoff. Fixed by the publish backoff + CREDENTIAL class.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l` on changed files — clean (after gofmt -w).
- `go test ./internal/core/ -run 'TestClassifyPushFailure|TestResolvePublishPath|TestPublishBackoff' -count=1` — PASS (33 test cases, 0 FAIL).
- `df -h /` — 187G/451G = 44%. PASS.
- Event analysis /opt/dev-tools/var/hydra/events/2026-10-02.jsonl — class counts + per-project loop rates + charon_orphan_reopened timestamps (sandbox scripts). PASS.

## Open edges for the next phase
- srv-telematics owner PAT is stale/invalid — human must rotate it (project parked local + loud; CREDENTIAL class now names the remedy).
- fms-frontend PR #66 waiting on human merge (tier policy leaves non-allowlisted repos open); backoff now slows retries to 15m max instead of ~48s.
- tiny-greeter checkout has no 'origin' remote — config error, human must fix remote or remove project.
- The 7 duplicate pan-filed P0 anomaly beads: close 6 as duplicates of s9sg (or whichever resolves first) — dedupe of pan-filed filings across projects/beads is a separate gap.
