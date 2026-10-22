# red handoff (rhff)

## What changed (files + one line each)
- internal/core/pan_mirror_health_test.go: 5 new FR-3 guarded auto-resync RED contract tests + `spyBrUpdater` stub (asserts heal invocations through the `BrUpdater` seam) + `mirrorGuardHooks` test seam (`isDaemonOwned`, `writeProbe`) + `mirrorHealEnv` fixture builder (auto-resync enabled, daemon-owned happy path); test `TestMirrorHealthSweep_HealsDBAheadTerminalRecord` (db_ahead terminal record → exactly 1 heal with the terminal status and bead ID), `TestMirrorHealthSweep_NonTerminalRecordNeverHeals` (in_progress jsonl status → zero heals + CreateBead filing path asserted), `TestMirrorHealthSweep_UserOwnedStoreNeverHeals` (guard: zero heals + fail-closed filing still fires), `TestMirrorHealthSweep_WriteProbeFailureNeverHeals` (guard: zero heals + fail-closed filing still fires), `TestMirrorHealthSweep_AutoResyncOffByDefault` (auto_resync=false → zero heals + filing path still fires); second-pass edit adds the fail-closed filing-path assertion to both guard tests so RED cannot pass a guard by doing nothing at all.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'MirrorHealth' -count=1` — PASS (ok, exit 0; 3 contract tests pass, 22 RED-gated tests skip loudly pre-GREEN via `mirrorHealthRequireImpl`, including all five new FR-3 tests).
- `MIRROR_HEALTH_ENFORCE=1 go test ./internal/core/ -run 'TestMirrorHealthSweep_HealsDBAheadTerminalRecord' -count=1` — FAIL, exit 1 (RED confirmed: `mirror-health audit sweep contract not yet implemented`; failure reason = missing feature, not compile error).
- `MIRROR_HEALTH_ENFORCE=1 go test ./internal/core/ -run 'MirrorHealth' -count=1` — FAIL, exit 1 (9 failed: 6 audit-sweep contract tests + 5 FR-3 tests + FR-4 tail; RED detects the missing guarded-heal contract).
- Coverage (`-coverprofile=/tmp/mh.cov` on same guarded run): `pan_mirror_health.go sweepMirrorHealth 0.0% / sweepMirrorHealthForProject 0.0% / mirrorHealthSweepDue 60.0% / restoreMirrorHealthDefaults 100.0%` — GREEN's obligation to raise to ≥80% after implementing the heal body; RED coverage is 0 on unimplemented production code as expected.
- `go vet ./internal/core/` — clean.
- `gofmt -l internal/core/pan_mirror_health_test.go` — clean.
- `git show --stat HEAD` (e46b4c3) — 1 file, +12.

## Open edges for the next phase
- GREEN must implement (contract pinned by the five tests): FR-3 heal only when ALL of (a) `mirror_health.auto_resync=true` knob (default false — the OFF-by-default gate the `TestMirrorHealthSweep_AutoResyncOffByDefault` test asserts), (b) record status terminal (`closed`/`completed`/`tombstone` in jsonl OR db), (c) store is daemon-owned (via `mirrorGuardHooks.isDaemonOwned` seam), (d) write-probe succeeds (`mirrorGuardHooks.writeProbe` seam), then one idempotent `br update <id> --status <same>` through `o.brUpdater.BrUpdateStatus` (spy asserts the call) and a typed `mirror_resynced` event; any guard failure → no heal + fail-closed filing path via `filePanBead` (mock `getCreateCalls()` > 0 in the filing-path tests).
- `spyBrUpdater.calls` records every heal invocation; GREEN must surface the heal status from `rec.JSONLStatus` falling back to `rec.DBStatus` (asserted `== "closed"` from the db_ahead fixture).
- `mirrorGuardHooks` is package-level and reset per-test via `mirrorHealEnv`; GREEN wiring must read the probes at heal-decision time (not at NewOverseer time) so the seam stays swappable.
- The FR-4 tests (`TestMirrorHealthFiling_*`) and audit-green tests skip via the same `mirrorResyncImplemented=false` RED gate — flipping that const true activates the full suite, so GREEN must implement both the audit sweep and the guarded heal/filing bodies before flipping.
