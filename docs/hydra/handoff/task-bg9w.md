# green handoff

## What changed (files + one line each)
- internal/core/bead_closer_hard_gates.go: dep-open close rejections no longer take a verification strike — `gateDependencyOrder` routes chain-member dep-open rejections through new `requeueDepOpen` (no `rejectClose`): reopens the bead, keeps a single durable `dep-open-requeue-N` label, escalates on the 5th consecutive rejection (`escalated` + `needs-human` + `verified-close-escalated`, one `bead.escalated` event); a passing dependency gate (or no deps) clears `dep-open-requeue-*` via `clearDepOpenRequeueLabels`. Constants named `depOpenRequeueLabelPrefix` / `depOpenMaxRequeues` (the test file owns `depOpenRequeuePrefix` / `depOpenEscalateAt`). Terminal beads are never reopened.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestDepOpen_|TestCloseDep_' -count=1 -v` — PASS, 9/9 (6 TestDepOpen_*, 3 TestCloseDep_*).
- `go test ./internal/core/ -run 'TestCloseGate|TestQuarantineClose|TestCloseDep|TestVerifiedClose' -count=1` — PASS (regression on the wider close-gate family, rejectClose paths unchanged for other reasons).
- `go build ./...` — PASS (rc 0).
- `go vet ./...` — PASS (rc 0). `gofmt -l` on touched files — clean.
- Coverage (`go test -coverprofile` scoped to TestDepOpen_): depOpenRequeueCount 100%, depOpenLabels 75%, clearDepOpenRequeueLabels 81.8%, requeueDepOpen 77.3%, gateDependencyOrder 73.7% (full family run lifts gateDependencyOrder to 100%).
- `internal/beadfiles/` untracked-dir check: contains `resolve.go` + `resolve_test.go` from commit 4b2780e5 "feat(beadfiles): shared target-file resolver package (no adoption)" — benign, tracked feature work, unrelated to this bead's close-gate contract.

## Open edges for the next phase
- Terminal-bead skip and store-error fallback branches in `requeueDepOpen` / `clearDepOpenRequeueLabels` remain partially covered (77.3% / 81.8%).
- beadfiles resolver adoption (internal/core imports) is intentionally deferred; close-path scope gate still uses local `beadTargetFiles`.
