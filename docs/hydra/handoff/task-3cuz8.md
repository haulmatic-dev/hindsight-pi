# syntax handoff (task-3cuz8 — Unverifiable-scar exit)

## What changed (files + one line each)
- None (verification-only SYNTAX pass). Unverifiable-scar exit GREEN (quarantine_unverifiable.go, `unverifiableImplemented=true`) and the refactor-25kh scarLabels dedupe already landed at HEAD; all target files conform. No edits required.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS (BUILD_OK, rc=0).
- `go vet ./internal/core/` — PASS (clean, rc=0).
- `gofmt -l internal/core/quarantine_unverifiable.go internal/core/quarantine_unverifiable_test.go internal/core/quarantine_reconciler.go` — clean (no output).
- `go test ./internal/core/ -run 'Unverifiable' -count=1` — PASS (`ok github.com/haulmatic-dev/hydra/internal/core 0.852s`).
- Verbose: 6/6 `--- PASS` — OnlyNoCommandIncrements, ThirdNoCommandStampsAndEmits, ThreeTicksStampAndWhoSkipsMember, StreakSurvivesRestartViaRegistry, SweepResetsStreakWhenLabelGone, OperatorRemovalResetsAndReEnables. Covers all four parent ACs (runner faults never increment; 3-strike closed-failed-unverifiable + quarantine_unverifiable + who-skip; RanFalseStreaks registry persistence + sweep reset; operator removal re-enables).
- Placeholder grep (`// TODO|FIXME|// Placeholder`) over the three target files — 0 hits.
- Doc comments verified: exported `SetRevalidationClassified`, `RevalidateVerdict`, and function docs present (comment lines precede 5 func decls); package-head constants documented.

## Open edges for the next phase
- None for this chain. Verify-only close; parent ACs green on the pinned 6-test suite.
