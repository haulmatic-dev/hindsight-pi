# fix handoff (fix-7ph9)

## What changed (files + one line each)
- internal/core/bead_closer_hard_gates.go: requeueDepOpen now labels the bead with a "dep-open-requeue-N" counter (cap 5 → escalate) instead of the old single-strike "dep-open"; requeueDepOpen emits "dep-open-requeued" event and clearDepOpenRequeueLabels wipes prior counters once the dep is resolved.
- internal/core/dep_open_requeue_test.go (new): 7 test functions covering cap-escalation, counter-increment, post-resolution label wipe, and escalate-at-cap; 259 lines.
- CHANGELOG.md: fixed entries for both.

## Verification (commands run + pass/fail)
- go build ./... — PASS.
- go vet ./internal/core/ — PASS (no output).
- gofmt -l on the two files — clean.
- go test ./internal/core/ -run 'TestDepOpen' -count=1 — PASS ("ok ... 0.217s", RC=0, 0 FAIL).
- No TODO/FIXME/Placeholder strings — VERIFIED (0 hits, both files).

## Open edges for the next phase
- None; counter labels are self-cleaning via clearDepOpenRequeueLabels.
