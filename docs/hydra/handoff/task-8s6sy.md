# red handoff (ai97h2 — close-time junk gate)

## What changed (files + one line each)
- internal/core/gate_junk_test.go (new): 6 tests — 5 require-gated RED contract tests + 1 unconditional clean-worktree fail-open wiring test pinning the FR-003 close-time junk gate — committed `.tmp-jest/x` drop-once (exactly one removal commit, junk off the index, file kept on disk, typed `close_gate_junk_dropped` naming the path verbatim), staged-only `.jest-cache` junk unstaged with zero commits (file kept), junk re-committed after two drops rejected with reason/label exactly `junk-artifacts` and `escalated` untouched (event `close_gate_rejected` reason junk-artifacts), `thread-cache.service.ts` chain edit never hits the gate, non-junk off-scope `README.md` edit stays warn-only from the junk gate.
- internal/core/gate_junk.go (new): inert RED stub — `junkArtifactsReason="junk-artifacts"`, `junkDropEventType="close_gate_junk_dropped"`, `maxJunkPathsInEvent=50`, `gateJunkImplemented=false`, fail-open `gateJunkArtifacts` (no drop, no event, no rejection).
- internal/core/bead_closer_hard_gates.go: one-line wiring of `gateJunkArtifacts` into `runCloseHardGates` before the dependency gate (inert until GREEN flips `gateJunkImplemented`).
- CHANGELOG.md: Added entry under Unreleased describing the RED suite + ENFORCE gate.

## Contract GREEN must implement
- Real `gateJunkArtifacts(bead, projectPath, repoName, worktreePath)` in `internal/core/gate_junk.go`: changed set = committed `base...HEAD` unioned with staged (`git diff --cached --name-only`), base = `ResolveIntegrationBase(c.config, repoPath)`; per `junksig.IsJunkPath` hit: staged-only paths unstaged via `git restore --staged --` (no commit), committed paths `git rm --cached --` + one removal commit (file stays on disk); drop events typed `close_gate_junk_dropped` with paths capped at 50; re-check — junk persisting after the drop pass → `rejectViaGate`/hard reject with the distinct reason and label `junk-artifacts`, escalation ladder untouched; drop history across close attempts must make a re-offense (third junk generation after two drops) a reject, not an infinite drop loop.
- Flip `gateJunkImplemented = true`.
- GREEN-side `docs/architecture/shepherd.md` gate table row still owed by the next phase.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS (exit 0).
- `go vet ./internal/core/` — PASS (VET_OK, clean).
- `go test ./internal/core/ -run 'TestGateJunk_' -count=1 -v` — PASS, exit 0, 0 failures; all 5 tests SKIP loudly with `RED: close-time junk gate contract not yet implemented` (daemon gate green pre-GREEN); no `[no tests to run]`.
- `GATE_JUNK_ENFORCE=1 go test ./internal/core/ -run 'TestGateJunk_' -count=1` — FAIL 5/5, each on the missing-contrract guard — RED detects the missing feature (failure reason = absent contract, not compile error). VERIFIED.
- Coverage: `go test ./internal/core/ -run 'TestGateJunk_' -count=1 -coverprofile` → `gateJunkArtifacts` 100.0% (fail-open stub path, via unconditional `TestGateJunk_CleanWorktreeFailOpenCoverage`); package stmt coverage 0.1% — nothing else exercised pre-GREEN. GREEN must re-measure with the real drop/reject paths.

## Open edges for the next phase
- The drop-history mechanism (how "once" survives close attempts/reopens: label counter vs. gitignore+rename ledger) is GREEN's design choice; tests pin only observable behavior.
- `junkDropEventType` naming and the ≤50-path event cap are pinned by the suite; deviation requires test edits.
