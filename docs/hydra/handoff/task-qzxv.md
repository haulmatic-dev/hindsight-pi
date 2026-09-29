# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-triage-qzxv-telematics-shared-wedge.md: triage report — root cause (stale checkout branch as integration base, fixed by landed integration-base resolver), clean merge-tree evidence per branch, disposition.
- internal/core/integration_breaker.go: NEW — IntegrationBreaker (FR-034): sliding window per project of integration-exhausted hard-parks; at threshold (default 5 / 30m) emits ONE `integration-wedge` event (beads list, threshold, window) + `[IntegrationBreaker]` log; optional project park via injectable callback (one signal per window, re-signals after window expiry); nil-safe, fail-open, never blocks the close path.
- internal/core/config.go: `overseer.integration_breaker` (IntegrationBreakerConfig: enabled/threshold/window/park_project); DefaultConfig enables observability-only (threshold 5, window 30m, park_project false).
- internal/core/bead_closer.go: `breaker` field + SetIntegrationBreaker; failIntegration exhaustion branch calls breaker.RecordExhaustion(repoName, beadID, stage, detail) after the per-bead hard-park is applied.
- internal/core/overseer.go: breaker constructed in NewOverseer (nil sink), event sink attached after EventSink creation.
- internal/core/service_starter.go: parkProject callback wired to registryStore.SetProjectPark when registry opens.
- internal/core/integration_breaker_test.go: 8 tests — trip at threshold, one-signal-per-window, window-expiry retrip, disabled-inert, nil-safe, park opt-in fires once, no park without opt-in, truncate helper.
- hydra-config.yaml.example + docs/architecture/shepherd.md: knob + `integration-wedge` event documented.
- CHANGELOG.md: Added entries (breaker + telematics triage).

Live-store actions (in /home/ajantha/projects/srv-telematics, all verified via br output):
- Removed labels `integration-exhausted` + `integration-failed` from ajantha-srv-telematics-{hig8,i6zt,5l11,38ot,f10l,dkqc,burr,otae,nygw,kher,u8z7} (✓ ×22). Post-check: 0 telematics beads carry integration-exhausted.
- Triage comment added to each of the 11 beads (✓ ×11 via `br comments add`).

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/` — clean.
- `go test ./internal/core/ -run 'TestIntegrationBreaker|TestTruncateForEvent' -count=1` — PASS (8 tests; Trip/OneSignal/Retrip/Disabled/NilSafe/ParkOptIn/NoParkWithoutOptIn/Truncate).
- `go test ./internal/core/ -run 'TestFailIntegration|TestIntegrationLoop|TestIntegrationExhausted|TestIntegrationRetest' -count=1` — PASS (incl. TestFailIntegration_EscalatesToExhaustedAtCap exercising the modified path).
- `git merge-tree --write-tree origin/development hydra/wt-ajantha-srv-telematics-<b>` for all 11 branches — rc=0, ZERO conflicts, ahead 23–58.
- `git symbolic-ref refs/remotes/origin/HEAD` in srv-telematics — refs/remotes/origin/development.
- PRs #324/#325 merged 2026-09-29 → pipeline healthy.
- Label re-scan after clears: 0 integration-exhausted in telematics store. PASS.
- hydra-242b: already closed as superseded (task-ng18); no action.

## Open edges for the next phase
- Breaker is in-memory (process-local), like the other integration caps; daemon restart clears the window — acceptable, wedges recur within the window if real.
- park_project uses registry store's project key (repo name as passed to failIntegration); scheduler ProjectParkInfo gate reads the same store — verify key format matches on a live wedge before enabling.
- Scheduler should retry the 11 cleared telematics beads; any loop → abandon-with-summary, not re-park.
- pan-filed meta duplicates (hig8/i6zt/5l11/dkqc/burr/nygw + m369) can be closed once retry pass confirms health.
