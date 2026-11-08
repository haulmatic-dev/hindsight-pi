# green handoff (task-gq4d)

## What changed (files + one line each)
- Verify-first close: FR-004 "Spec-infeasible decision packets" GREEN was already implemented and committed on this branch as `ec3e1aaf` ("feat(core): spec-infeasible decision packets + durable counted gate-fp labels (FR-004 GREEN)") — this bead's deliverable is verification + handoff only.
- internal/core/pan_decision_packet.go: `decisionPacketCheck(project, bead, now)` off the stale-filing sweep tick — durable `gate-fp-<fp8>-<N>` label reaching N ≥ 3 (same-fingerprint `work` rejections), no `infra-reject-*`/`infra-requeue-*` marker, latest verdict says `"done": false` → `spec-infeasible` label + exactly one `decision_request` event (`command`, `fingerprint`, `gate_output` ≤4 KB, `measurements` parsed from jest coverage table); `spec-decided:<x>` clears candidacy; pending packet refuses `clear-escalation` (`clear-escalation: <id> is spec-infeasible; awaiting spec-decided`); fail-open throughout.
- internal/core/bead_closer.go: `trackVerificationFingerprint` writes counted `gate-fp-<fp8>-<N>` labels (bare label counts as 1) so daemon restarts cannot reset the ladder.
- internal/core/pan_stale_filing.go: sweep evaluates each listed bead via `decisionPacketCheck`.
- internal/core/service_starter.go: wiring.
- CHANGELOG.md, docs/architecture/shepherd.md: operator docs.

## Verification (commands run + pass/fail)
- `go test -c ./internal/core/ && /tmp/core-gq4d.test -test.run 'TestDecisionPacket_' -test.v` — PASS, RC=0: `--- PASS: TestDecisionPacket_ThreeWorkRejectionsFilePacket`, `_InfraRejectionsNeverTrigger`, `_FewerThanThreeRejectionsUntouched`, `_SpecDecidedLabelClearsCandidacy`, `_LabelPrefixContract` (7 subtests, no `[no tests to run]`).
- `go build ./...` — PASS (BUILD_OK).
- `go vet ./internal/core/` — PASS (VET_RC=0, clean).
- Coverage (narrow run, -coverprofile): `decisionPacketCheck` 82.5%, `decisionPacketMeasurements` 85.7%, `gateFingerprintRejectionCount`/`dominantGateFingerprint`/`hasInfraRequeueLabel`/`hasSpecDecidedLabel`/`verdictSaysDoneFalse`/`clearEscalationRefusalFor` 100% each — ~96% weighted on new code.
- Placeholder grep (`// (Placeholder|TODO|FIXME)`) over target files — 0 hits; all non-empty.
- `git status --short` — clean; GREEN landed at ec3e1aaf, HEAD 58884197 (sibling RED-fix commit touching gate_deps_preflight.go only).

## Open edges for the next phase
- `decisionPacketCheck` uncovered lines are the fail-open fault branches (list/label-write errors) — same br-CLI-seam disposition as the stale-filing sweep.
- Real-workspace path (label persistence via br) exercised by the daemon sweep in production, not in unit tests.

HYDRA_VERDICT: {"done": true, "ac_met": ["3 same-fingerprint work rejections + done:false -> spec-infeasible label + exactly one decision_request", "infra-class rejections never trigger the packet", "fewer than 3 matching fingerprints untouched", "spec-decided:<x> clears candidacy", "Test Command output lists --- PASS lines, no [no tests to run]", "go build ./... exit 0", "go vet clean", "coverage reported for new code (~96% weighted; check 82.5%, helpers 100%)"], "ac_unmet": [], "files_changed": ["docs/hydra/handoff/task-gq4d.md"], "verification": [{"cmd": "go test ./internal/core/ -run 'TestDecisionPacket_' -count=1 -v", "result": "PASS 7/7, exit 0"}, {"cmd": "go build ./...", "result": "PASS"}, {"cmd": "go vet ./internal/core/", "result": "PASS"}, {"cmd": "go test -coverprofile (narrow)", "result": "decisionPacketCheck 82.5%, decisionPacketMeasurements 85.7%, helpers 100%"}]}
