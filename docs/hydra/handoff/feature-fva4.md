# feature handoff (feature-fva4)

## What changed (files + one line each)
- internal/core/gate_doctor.go (new): `GateDoctor` (LLM failure-family classifier) + `GateDoctorPrompt`/`ParseGateDoctorClass`/`Triage`/`FileFixBead`.
- internal/core/bead_closer.go: wired `gateDoctor` into the close path; on RED it delegates to `handleVerificationRed`.
- internal/core/gate_close.go: added `SetGateDoctor`.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'GateDoctor' -count=1` — PASS (2 tests).

## Open edges for the next phase
- The "environment" / "flaky" / "stale-gate" branches currently fall back to the standard `rejectClose` escalation path because the rerun-once/escalate loop lives outside the doctor's scope.
