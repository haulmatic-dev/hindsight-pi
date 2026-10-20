# green handoff (htq0)

## What changed (files + one line each)
- internal/core/process_hygiene.go (new): zero-token stall detector (working + tokens_so_far==0 + age>15m — precise "never completed a turn" signature; long tool calls carry tokens>0 and are never flagged), rogue-pi scanner (/proc walk: argv0 basename `pi`, cgroup not under hydra.service, PID absent from tracked set, etime>2h; events deduped per PID), explicit-PID repair actions KillStalledWorker (registry row → PID, kill, MarkCompletedWithReason "zero-token-stall-killed") and KillRoguePis (kills recorded rogues, clears snapshot); procStartAge/boot-time parsing injectable via procRoot for tests.
- internal/core/charon.go: `hygiene *processHygiene` field, `killFn` injectable kill seam + killPID, hygiene sensor calls in performCleanup BEFORE the no-tracked-workers early return (idle fleet is exactly when stalls/rogues survive).
- internal/core/pan_health.go: digest collector reads Charon's sensor snapshots (host-level, before the beadsClient nil early-return) → ZeroTokenStalls/RoguePis + host anomaly lines naming the repair verbs.
- internal/pan/health.go: HealthDigest.ZeroTokenStalls/RoguePis fields + Text() `host: process-hygiene stall=... rogue=...` line.
- internal/pan/pan.go: repairWhitelist + judgment prompt gain kill-stalled-worker / kill-rogue-pi; exported IsAllowedRepair test seam.
- internal/core/service_starter.go: repair handlers for both verbs (stalled-worker reopens bead + resets retries after the kill; rogue-pi kills recorded PIDs).
- docs/architecture/shepherd.md: event rows (zero_token_stall_detected, rogue_pi_detected) + repair-table entries.
- CHANGELOG.md: Added entry.
- internal/core/process_hygiene_test.go (new): 8 tests.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestZeroTokenStall|TestKillStalledWorker|TestRoguePIScanner|TestKillRoguePis|TestRepairVocabulary_IncludesProcessHygiene|TestDigest_SurfacesProcessHygiene|TestNilHygieneSafe|TestRoguePIScanner_LiveFixture' -count=1` — PASS (8 tests). Detector matrix: stalled flagged; tokens>0 and young and completed NOT flagged.
- LIVE PROOF: `TestRoguePIScanner_LiveFixture` PASS — scanner caught live rogue PID 3002553 (cmdline "pi", cgroup `0::/user.slice/user-0.slice/session-13176.scope`, age 52h, outside hydra.service). Fixture left alive for the operator deploy; Pan repair `kill-rogue-pi` kills it post-deploy.
- `go test ./internal/pan/ -count=1` — PASS (21 tests; whitelist/prompt assertions unaffected).
- `go test ./internal/core/ -run 'Digest' -count=1` — PASS (ok 30s, whole Digest set incl. new digest test).
- `go build ./...` — PASS. `go vet ./internal/core/ ./internal/pan/` — clean after adding missing `log` import. gofmt clean.
- Commit verified via `git show --stat HEAD` (9 files, +737/−1).

## Open edges for the next phase
- The live kill of PID 3002553 happens post-deploy via Pan `repair:kill-rogue-pi` (operator must NOT kill it manually first, per bead).
- Zero-token stall findings are advisory until Pan dispatches the kill: detector does not auto-kill (deliberate — a spawn window race is handled by the 15m age gate, but the kill is judgment-gated).
- Rogue-pi scanner skips cgroup-unreadable PIDs and processes without a readable cmdline (kernel threads etc.); boot-time read comes from `<procRoot>/stat`.
- No benchmarks; scanner is an O(procs) readdir per Charon tick (1m), reads 3 small files per candidate pi process only.
