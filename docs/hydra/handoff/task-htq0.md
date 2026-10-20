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

## Gate-round-2 addendum (live repair dispatch)
- Live repair test `TestKillRoguePis_LiveFixture` (PASS): runs the REAL kill path (no stub) against fixture PID 3002553. Finding: fixture is ROOT-OWNED; daemon runs as uid 999 (hydra) — `kill -0 3002553` → "Operation not permitted" (VERIFIED), so the daemon account physically cannot signal it. killProcess rewritten to direct syscalls (SIGTERM→5s grace→SIGKILL) so the failure carries syscall.EPERM instead of kill(1)'s opaque "exit status 1"; KillRoguePis classifies EPERM fail-closed: typed `rogue_pi_kill_denied` event + loud log "ESCALATION: root must kill PID 3002553 (rogue pi, 52h)" (verified in live run output), snapshot cleared, error returned.
- LIVE REPAIR DISPATCHED log: `LIVE REPAIR DISPATCHED: real kill path attempted on PID 3002553; permission-denied escalation verified (root-owned rogue — root must kill PID 3002553)`.
- Narrow suite re-run after syscall change: 9/9 PASS incl. TestCharon/TestKill/TestRecoverOrphan regression (ok 16.4s). go vet clean, gofmt clean.

## Open edges for the next phase
- The live kill of PID 3002553 CANNOT be completed by the daemon account (root-owned fixture, no sudo for uid 999 — VERIFIED). The repair is live and correct: dispatch → EPERM → `rogue_pi_kill_denied` escalation naming the root remedy. Operator/root kills PID 3002553 (one command) or grants the daemon account signal rights; after that the deployed `repair:kill-rogue-pi` completes the kill unaided.
- Zero-token stall findings are advisory until Pan dispatches the kill: detector does not auto-kill (deliberate — a spawn window race is handled by the 15m age gate, but the kill is judgment-gated).
- Rogue-pi scanner skips cgroup-unreadable PIDs and processes without a readable cmdline (kernel threads etc.); boot-time read comes from `<procRoot>/stat`.
- No benchmarks; scanner is an O(procs) readdir per Charon tick (1m), reads 3 small files per candidate pi process only.
