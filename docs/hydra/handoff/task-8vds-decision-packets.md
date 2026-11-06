# refactor handoff (8vds — Spec-infeasible decision packets)

## What changed (files + one line each)
- internal/core/pan_decision_packet.go: single `gateFingerprintCounts(labels)` helper now backs both `gateFingerprintRejectionCount` and `dominantGateFingerprint` (removed the duplicated label-parse loops); dropped dead parameters — `now` from `decisionPacketCheck` (was `_ = now`) and `cmd` from `decisionPacketMeasurements` (was `_ = cmd`); unused `time` import removed. Behavior byte-identical.
- internal/core/pan_stale_filing.go: sole `decisionPacketCheck` call site updated to the new signature.
- CHANGELOG.md: Unreleased/Changed entry.
- docs/hydra/handoff/task-8vds-decision-packets.md: this file. (Note: `task-8vds.md` was already taken by an earlier chain's deps-preflight refactor handoff — that file was accidentally overwritten in a first commit and restored verbatim in the amend; this separate filename avoids the collision.)

## Verification (commands run + pass/fail)
- BEFORE (pre-edit baseline): `go test ./internal/core/ -run 'TestDecisionPacket_' -count=1` — `ok github.com/haulmatic-dev/hydra/internal/core 0.015s`, 0 FAIL.
- AFTER: `go build ./internal/core/` — PASS. `go vet ./internal/core/` — PASS (clean). `gofmt -l` on touched files — clean. `go test ./internal/core/ -run 'TestDecisionPacket_' -count=1 -v` — PASS 5/5 (`--- PASS: TestDecisionPacket_ThreeWorkRejectionsFilePacket`, _InfraRejectionsNeverTrigger, _FewerThanThreeRejectionsUntouched, _SpecDecidedLabelClearsCandidacy, _LabelPrefixContract), `ok ... 0.015s`, no `[no tests to run]`.
- No benchmarks exist for this path; no perf check run.

## Open edges for the next phase
- None for this bead. The `decisionPacketEligible` predicate extraction was considered but skipped to preserve the per-guard explanatory comments in `decisionPacketCheck`.
