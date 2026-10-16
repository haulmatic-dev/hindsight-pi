# bug handoff (bug-6xhn)

## What changed (files + one line each)
- internal/core/gate_runner.go: `gateCmdProcGroup` (Setpgid + ctx-watcher that SIGKILLs the gate process group on timeout) + `killGateProcessGroup` (post-`Wait` group kill) wired into both `RunGate` and `RunCoverageTruthGate` — gates are no longer in the daemon's PGID and grandchildren (jest/pytest) can no longer outlive the artifact.
- cmd/worker/sysprocattr_linux.go (new) + sysprocattr_other.go (new): `piSysProcAttr()` — Pi child launches with `Setpgid: true` and (Linux) `Pdeathsig: SIGKILL`; cmd/worker/main.go uses it instead of the inline attr.
- internal/core/charon.go: `sweepReparentedWorktreeProcesses` / `listProcsTable` (injectable `psRunner` field) / `parseReparentedWorktreeProcs` / `sweepReparentedWorktreeProcs` — every `ps` row with `.hydra-worktrees/` in args and PPID 1 gets its whole process group SIGKILLed (dedup by PGID; daemon's own group skipped; `ps` failure fail-open). Called in `performCleanup` before the no-tracked-workers early return.
- Tests: internal/core/gate_runner_test.go (+2: grandchild-reap on completion, timeout kills group), internal/core/charon_reparented_sweep_test.go (new, 4), cmd/worker/sysprocattr_test.go (new).
- CHANGELOG.md: two Fixed entries under Unreleased.

## Verification (commands run + pass/fail)
- RED (pre-fix): `TestGateRunner_KillsGrandchildrenOnCompletion` — "grandchild leaked past gate completion: pgrep -f 'sleep 2912731' = 7 PIDs" (reproduces defect (b)); `TestGateRunner_TimeoutKillsProcessGroup` — RunGate blocked past timeout with leaked children (reproduces absent group-kill/timeout enforcement). Daemon-PGID defect (a) verified by inspection: gate commands had no `SysProcAttr` at all pre-fix (quoted code: `cmd := exec.CommandContext(ctx, "sh", "-c", cmdLine)` with no Setpgid).
- GREEN: `go test -c ./internal/core/` + test binary `-test.run 'TestGateRunner_KillsGrandchildren|TestGateRunner_TimeoutKillsProcessGroup' -test.v` — both PASS (0.05s / 0.75s).
- `go test ./internal/core/ -run 'TestGateRunner|TestGateClose|TestCharon|TestRecoverOrphan|TestParseReparented|TestSweepReparented' -count=1` — ok, 3.160s.
- `go test ./cmd/worker/ -run TestPiSysProcAttr -count=1` — ok. `go test ./cmd/worker/ -count=1` — ok.
- `go build ./internal/core/`, `go build ./cmd/worker/` — PASS. `go vet ./internal/core/ ./cmd/worker/` — clean. `gofmt -l` on touched files — clean.

## Open edges for the next phase
- PDEATHSIG fires when the forking *thread* dies (Go runtime caveat); covered additionally by the Charon reparented sweep, so the combination is belt-and-braces rather than strictly ordered.
- The sweep keys on the literal `.hydra-worktrees/` substring in `ps args`; a project path containing that string elsewhere would be a false positive (none known).
- Live soak: the vv3-class gate leak should be confirmed gone on the next real close gate run (jest finishing within the group after the artifact).

## AC mapping
- (a) gates in daemon PGID → Setpgid on both gate execs (gate_runner.go), pinned by timeout/kill tests exercising the group behavior.
- (b) gate processes killed (whole group) on completion/timeout → `killGateProcessGroup` + ctx watcher; RED→GREEN tests prove both paths.
- (c) worker-death node/jest leaks → Pdeathsig on Pi + Charon reparented-worktree group sweep (parse/kill/dedup/daemon-group-skip/fail-open tests).
