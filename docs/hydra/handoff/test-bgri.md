# test handoff (test-bgri)

## What changed (files + one line each)
- internal/core/postclose_sweep.go: GREEN for the pinned `postclose_sweep` contract — scan() re-runs closed beads' `metadata["test_command"]` (24h window): flake guard (pass re-run → `postclose-flake` label, no reopen/comment), bounded reopen (two failures → StatusOpen + `postclose-reopened` + comment carrying BOTH outputs, error-string fallback when output empty), third failure on already-reopened → `needs-human` (no second reopen), fail-open throughout; hourly Start loop + nil-safe Stop; typed `postclose_swept` events.
- internal/core/overseer.go, internal/core/service_starter.go: sweeper field + Start/Stop wiring.
- internal/core/config.go, hydra-config.yaml.example: new `overseer.postclose_sweep` knob (default 0/disabled).
- CHANGELOG.md, docs/architecture/shepherd.md: operator docs + event row.
- internal/core/worktree_graphify_symlink_test.go (+ worktree_manager.go provisioning): cherry-picked 4e7e764c (hydra-ew6d GREEN re-land) — the worktree graphify-out symlink feature + its 4 RED tests were only on branch refs/quarantine, absent from this branch; TestWorktreeGraphifySymlink now present and passing (4/4).
- docs/hydra/handoff/test-bgri.md: this handoff.

## Verification (commands run + pass/fail)
- Gate failure root: `TestPostCloseSweep*` (4 tests) in internal/core — postclose_sweep.go was still the inert RED stub (GREEN never landed), `expected postclose-flake label, got []` etc. Fixed by implementing GREEN. Now PASS (TestPostCloseSweep suite, all tests).
- `go test ./internal/core/ -run 'TestWorktreeGraphifySymlink' -count=1` — PASS (4/4: CreatedAndReadable, GitignoreLine, StatusClean, AbsentGraphifyOut_NoOpNoError).
- `go build ./...` — PASS. `go vet ./internal/core/` — clean.
- hindsight_search at bead start — not connected (skip rule applied).
- graphify-out/graph.json absent in cwd → rg/handoff fallback per protocol.

## Open edges for the next phase
- `excerpt` name collided with verification_runner.go's helper — renamed to `trimOutput` here; future output-trimming helpers must check that namespace.
- postclose sweep disabled by default (`overseer.postclose_sweep: 0`); enable deliberately on a canary project first — it executes recorded commands in project dirs.
- addComment wired as nil for now (no br comment plumbing in BeadsClient); reopen bodies log to daemon log instead of store comments.
