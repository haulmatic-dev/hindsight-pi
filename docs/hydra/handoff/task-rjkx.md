# discovery handoff (task-rjkx)

## What changed (files + one line each)
- cmd/worker/close_hold.go (new): FR-004 bounded close-hold — `closeHoldFor` (done:false + summary triggers hold), `closeHoldCount` (label parser, max suffix), `closeHeldRecord`/`emitCloseHeldEvent` (typed `worker_close_held` JSONL event, fail-open on HYDRA_HOME/write errors), `runCloseHold` (below cap → hold + persist label; at cap 2 → forced close, `forced:true`), `persistCloseHoldLabel` (replaces previous `close-hold-N` via br label add/remove in project cwd).
- cmd/worker/close_hold_test.go (new): 9 tests — closeHoldFor table (nil/done/done:false-without-summary fail open; done:false+summary holds), closeHoldCount table (malformed/negative suffixes ignored, max taken), closeHeldRecord fields incl. forced:true, emit fail-open (no HYDRA_HOME / bad path), below-cap holds, at-cap forced, nil-verdict never holds, real-br label persistence (skip if br unavailable), replace-label assertion (exactly one close-hold label).
- cmd/worker/main.go: wiring — close-hold check at both close sites (main close + files+tests close), before `closeBeadFromWorker`.
- CHANGELOG.md: Unreleased/Fixed entry documenting the bounded hold contract and fail-open paths.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go test ./cmd/worker/ -run 'CloseHold|PersistCloseHold|EmitCloseHeld' -count=1` — PASS (9/9 incl. real-br integration test).
- Full suite `go test ./cmd/worker/ -count=1` — PASS (`ok ... 2.713s`, after transient EAGAIN instability earlier in the session settled).
- Committed as `43b9818` (4 files, +346/−2); verified via `git show --stat HEAD`.

## Open edges for the next phase
- `persistCloseHoldLabel` uses PATH-resolved `br`; production worker PATH must include it (same as other br calls).
- Hold label survives close only on the non-forced path; at cap the forced close removes no labels (bead closes, `close-hold-N` stays on a closed bead — same convention as other end-state labels).
- EAGAIN transient in this box: CASS bullet `b-mvgi421t-8opfwe` (score 0.9) already documents the environmental intermittent-failure pattern; no code change needed.
