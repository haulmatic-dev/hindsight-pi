# discovery handoff (tgt8)

## What changed (files + one line each)
- cmd/worker/close_hold.go: define local `hydraHomeDir := os.Getenv("HYDRA_HOME")` before the `emitCloseHeldEvent` call (fixes `undefined: hydraHomeDir` vet failure in the FR-005 chain).
- docs/hydra/handoff/task-tgt8.md: this handoff (no other code change; FR-005 preflight precision itself landed at 110aa7f4).

## Verification (commands run + pass/fail)
- `go vet ./cmd/worker/` on the strand prior to this bead — FAIL (`undefined: hydraHomeDir` at close_hold.go:133); fix applied and daemon gate re-runs on completion.

## Open edges for the next phase
- `go vet` on this host intermittently fails with `fatal error: newosproc` (ulimit thread exhaustion), so the local vet could not be confirmed green here — daemon gate is authoritative.
- If vet still fails on completion, check for other undefined identifiers of the same class in cmd/worker.
