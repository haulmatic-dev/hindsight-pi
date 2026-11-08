# red handoff (wyel)

## What changed (files + one line each)
- internal/core/pan_mirror_health_test.go: FR-4 Pan filing RED contract tests appended to the existing FR-2/FR-3 suite — `TestMirrorHealthFiling_UserOwnedDivergentStoreFilesExactlyOne` (user-owned divergent store files exactly one pan bead: `mockBeadsClient.createCalls` length == 1, title contains the bead id, exactly one `mirror_drift_needs_human` event via `readEventsOfType(sink, "mirror_drift_needs_human")`) and `TestMirrorHealthFiling_FilingSinkFailureLoggedSweepContinuesNoPanic` (filing-sink failure (`mockBeadsClient.createErr = errors.New(...)`) never panics, emits no event (not a successful filing), and the sweep continues — a second tick with the sink healed (`mockBeadsClient.createErr = nil`) re-attempts the filing, proving the failure was logged, not fatal).
- internal/core/testhelpers_test.go: added `mockBeadsClient.createErr` (error injection on `CreateBead`) and `mockCreateCall`/`getCreateCalls()` (records filing-sink invocations). No behavior change to existing mocks.
- CHANGELOG.md: documentation entry under unreleased describing the FR-4 red tests and how to enforce (`MIRROR_HEALTH_ENFORCE=1`).

## Verification (commands run + pass/fail)
- `go build ./...` — PASS (build exits 0, no stderr output).
- `go vet ./internal/core/` — PASS (no stderr output).
- `go test -v ./internal/core/ -run 'TestMirrorHealthFiling' -count=1` — PASS (2 tests, exit 0, both SKIP loudly: "RED: mirror-health audit sweep contract not yet implemented (set MIRROR_HEALTH_ENFORCE=1 to fail)").
- `env MIRROR_HEALTH_ENFORCE=1 go test -v ./internal/core/ -run 'TestMirrorHealthFiling' -count=1` — FAIL as intended (RED detection): both tests fail asserting the missing feature — "user-owned divergent store must file exactly one pan bead, got 0 calls: []" and "filing sink must have been attempted exactly once, got 0".
- `go test ./internal/core/ -run 'MirrorHealth' -count=1` — exit 0 (parent AC satisfied for the red phase without the gate env var).

## Open edges for the next phase
- `mirrorResyncImplemented` remains `false` in `internal/core/pan_mirror_health.go` — the FR-4 tests are compiled in but SKIP until the GREEN bead flips it to `true`. The test seam `mirrorWriteProbe` (inert, `nil` = probe succeeds) is already wired for the GREEN implementation to use.
- The average filing-sink contract (`mockBeadsClient.createErr`) is confined to `internal/core/testhelpers_test.go`. If the GREEN implementation needs precise failure modes (e.g. partial sink failure), extend the mock with a per-call error map, don't repurpose `createErr`.
- The timeout contract for the filing sink was NOT pinned here (out of scope for this bead). If the gate adds a timeout constraint, it belongs in a new test comment + `mockBeadsClient` timeout field.

## Notes for the next phase (green)
- `pan_mirror_health_test.go` still contains the FR-2 and FR-3 contract tests from their respective beads: keep those intact (their skip gates are `mirrorHealthRequireImpl` and `mirrorGuardHooks`). Do NOT remove or silently skip them; only flip the `mirrorResyncImplemented` constant to `true` and wire the `mirrorWriteProbe` seam implementation.
