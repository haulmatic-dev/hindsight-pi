# test handoff (test-hv5n)

## What changed (files + one line each)
- docs/hydra/handoff/test-hv5n.md: this handoff (validation round 1, verify-first close; no code change — FR-004 bounded close-hold implementation and test suite already exist and pass).

## Verification (commands run + pass/fail)
- Verify-first: the claimed gap "RED bead hydra-2bht has no test files (target_files metadata missing)" is stale — `cmd/worker/close_hold_test.go` exists (163 lines, 9 test funcs: TestCloseHoldFor, TestCloseHoldCount, TestCloseHeldRecordFields, TestCloseHoldEventMarshals, TestEmitCloseHeldEventWritesCentralStream, TestEmitCloseHeldEventFailOpenWithoutHydraHome, TestRunCloseHoldHoldsBelowCap, TestRunCloseHoldForcedAtCap, TestRunCloseHoldNilVerdictNeverHolds) and `cmd/worker/close_hold.go` (170 lines) implements the bounded close-hold contract.
- Provenance: FR-004 landed at commit 43b98187 ("feat(worker): FR-004 bounded close-hold on self-reported done:false"; close_hold.go + close_hold_test.go + main.go wiring + CHANGELOG entry). NOT ancestor of origin/development — integration's job, work is intact on hydra/wt-hydra-8uw4.
- `go test ./cmd/worker/ -run 'TestCloseHoldFor|TestCloseHoldCount|TestCloseHeldRecordFields|TestCloseHoldEventMarshals|TestEmitCloseHeldEvent|TestRunCloseHold' -count=1 -v` — PASS (15 cases).
- `gofmt -l cmd/worker/close_hold.go` — clean (gofmt whitespace normalization was landed by a concurrent strand mid-bead; `git status --short` clean at close).
- `go build ./cmd/worker/` — PASS.
- hindsight_search at bead start — not connected (skip rule applied). graphify-out/graph.json absent in cwd → rg/handoff fallback per protocol. cm context (CASS) queried: FR-004 close-hold rule matched the verified implementation exactly.

## AC mapping
- done:false with summary → holds close, emits `worker_close_held` {bead, hold_count, verdict_summary}, persists `close-hold-N` label replacing the prior one — MET (closeHoldFor/closeHeldRecord/persistCloseHoldLabel; TestRunCloseHoldHoldsBelowCap).
- Bounded at 2 then forced close with `forced:true`, fail-open close — MET (maxCloseHolds=2, TestRunCloseHoldForcedAtCap).
- Malformed JSON / done:false-without-summary fail open — MET (closeHoldFor nil-verdict + no-summary guards; TestRunCloseHoldNilVerdictNeverHolds).
- Test files exist for the RED bead's chain — MET (close_hold_test.go, 9 funcs, 15 cases pass).

## Open edges for the next phase
- Same disposition as sibling handoffs (test-3zrz/test-6lmz/test-udlg): if the validation gate still flags missing target_files metadata for the RED bead hydra-2bht, the fix is bead metadata (br update target_files), not the test suite.
- `persistCloseHoldLabel` coverage is low (~21% per the earlier FR-004 handoff); br CLI paths need a real beads workspace — optional future integration test.
