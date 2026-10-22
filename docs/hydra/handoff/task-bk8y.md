# green handoff (hydra-bk8y)

## What changed (files + one line each)
- No new code change this pass. FR-004 "Bounded close-hold on self-reported `done:false`" was already implemented and committed at 43b98187 on branch `hydra/wt-hydra-8uw4` (cmd/worker/close_hold.go, cmd/worker/close_hold_test.go, cmd/worker/main.go wiring at both close sites, CHANGELOG.md entry). This bead is the verify-first green close: tests re-run green, build green, tree clean.

## Verification (commands run + pass/fail)
- `go test ./cmd/worker/ -run 'CloseHold' -count=1 -v` — PASS (9 test funcs / ~15 sub-cases, ok ~0.007s). Includes TestCloseHoldFor, TestCloseHoldCount, TestCloseHeldRecordFields, TestCloseHoldEventMarshals, TestEmitCloseHeldEvent*, TestRunCloseHoldHoldsBelowCap, TestRunCloseHoldForcedAtCap, TestRunCloseHoldNilVerdictNeverHolds.
- `go build ./...` — PASS (BUILD_OK, exit 0).
- `git status --short` — clean at HEAD df4db599 (integration base; close-hold commits are ancestors, landed via wt-hydra-8uw4 integration).
- Coverage (prior full-coverage run, CloseHold profile): closeHoldFor 100%, closeHoldCount 100%, closeHeldRecord 100%, runCloseHold 100%, emitCloseHeldEvent 50% (marshal/append error branches), persistCloseHoldLabel 21.4% (br CLI paths need a real beads workspace; accepted as non-AC edge).
- Handoff write note: `spawn /bin/bash EAGAIN` intermittent on this host during final commit attempt — known transient (same class as prior task-ktqp outage); file content written via non-shell tool, shell recovered for earlier verification commands this pass.

## AC mapping (parent criteria)
1. Self-reported `done:false` verdict gates close (`closeHoldFor`, fail-open on nil/unparseable verdict) — MET (TestCloseHoldFor, TestRunCloseHoldNilVerdictNeverHolds).
2. Typed `worker_close_held` event {bead, hold_count, verdict_summary} + `close-hold-N` label persisted — MET (TestCloseHoldEventMarshals, TestCloseHeldRecordFields, label-persist WARN non-fatal path).
3. Forced close at `hold_count == maxCloseHolds` (2) — MET (TestRunCloseHoldForcedAtCap).
4. `closeWithHold` is the single close entrypoint, wired at both close sites in cmd/worker/main.go — MET (main.go:522 + auto-close guard site).
5. Tests green: `go test ./cmd/worker/ -run 'CloseHold' -count=1` exit 0 — VERIFIED.
6. `go build ./...` exit 0 — VERIFIED.

## Open edges for the next phase
- None blocking. persistCloseHoldLabel br-CLI branches (21.4%) remain untested against a real beads workspace — optional follow-up if a test workspace fixture is added.
- Host shell intermittently fails with `spawn /bin/bash EAGAIN`; if a future bead on this worktree needs commits, retry or use the Python-subprocess alternate path.
