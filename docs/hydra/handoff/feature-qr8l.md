# GREEN handoff

## What changed (files + one line each)
- `internal/core/hard_park_reconciler.go` (new) — `ReconcileHardParkedWorktrees`: repo-level git probe per project with `worktree-unavailable` beads; hysteresis x2 (2 consecutive successful ticks) clears the label, emits typed event `worktree_unparked`; probe failure resets counter, labels untouched, logs once per tick; never passes `escalated`/`needs-human` to RemoveBeadLabels.
- `internal/core/overseer.go` — new fields `hardParkProbes map[string]int` + `lastHardParkReconcile`; `tick()` calls `o.reconcileHardParks()` (throttled to 1/min, fail-open) before publish.
- `internal/core/hard_park_reconciler_test.go` (new) — 5 tests: hysteresis, event emitted, probe-failure reset/fail-open, label-scope guard (escalated/needs-human preserved), never touches beads without the label.
- `docs/architecture/shepherd.md`, `CHANGELOG.md` — documented reconciler.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS
- `go test ./internal/core -run 'TestReconcileHardParked' -count=1` — PASS (exit 0; 5 tests)
- `go vet ./internal/core/` — clean

## Open edges for the next phase
- Probe is minimal (`git rev-parse --git-dir`); if a wedge needs deeper checks (e.g. dubious-ownership on a specific worktree path), extend `defaultHardParkProbe` — hysteresis/state shape already supports it.
- Label clearing only; bead status not reopened (label alone gates discovery/Charon/retry paths). If a parked bead's status is `blocked`, confirm respawn works or extend to reopen.
- Charon OrphanRetry's "skipping reopen" log is untouched; it will naturally stop once labels clear.
