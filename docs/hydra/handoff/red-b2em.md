# RED handoff

## What changed (files + one line each)
- internal/core/bead_claims.go (new): RED-phase inert stub for the deterministic payload drift-check — `BeadClaim{Hard, Assertion, VerifyCmd}`, `parseBeadClaims` (returns nil), `claimVerifyTimeout = 2s`, `osLookupEnv` helper, `beadClaimRunner` func type, `beadClaimChecker{events, run}` + `newBeadClaimChecker(events, run)` + `checkBeadClaims(beadID, payload)` (always allows spawn, no events). Enough surface for the package to compile; zero behavior.
- internal/core/bead_claims_test.go (new): contract tests pinning all parent ACs — parse (`claim:` / `hard: claim:` lines, `hard:` prefix maps to Hard=true, multiple mixed claims, non-claim lines ignored, verify optional, parens preserved in verify cmd), check (zero claims → no-op no events, soft fail → allow + `claim_soft_failed` event, hard fail → refuse + `claim_hard_failed` event, hard pass → allow no events, hard unknown/timeout → fail-open + `claim_unknown` event, mixed: hard failure refuses after soft pass, runner invoked with 2s timeout).
- CHANGELOG.md: Added entry under Unreleased describing the RED stub + ENFORCE gate.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'BeadClaims|DriftCheck' -count=1` — PASS (suite exits 0; contract subtests skip loudly with "RED: bead claims drift-check contract not yet implemented" so the daemon gate stays green pre-implementation).
- `BEAD_CLAIMS_ENFORCE=1 go test ./internal/core/ -run 'BeadClaims|DriftCheck' -count=1` — FAIL 6/14 (intentional RED proof: skips become loud failures pinning the unimplemented contract — parse returns nil, runner never invoked, no events emitted, hard refusal absent).
- `go build ./internal/core/` — PASS.
- `go vet ./internal/core/` — PASS (after fixing `t.Fatalf`/`t.Skipf` non-constant-format vet errors).

## Open edges for the next phase
- GREEN must implement `parseBeadClaims` (line regex: `^(\s*hard:\s*)?claim:\s*(<assertion>)\s*(\(verify:\s*(<cmd>)\s*\))?$` — captures assertion + optional verify cmd, `hard:` prefix sets Hard) and `beadClaimChecker.checkBeadClaims` (run each claim's verify cmd via the injectable `run` with `claimVerifyTimeout` (2s); pass → silent allow; fail → `claim_soft_failed` event + allow, or `claim_hard_failed` event + refuse when Hard; timeout/parse-error → `claim_unknown` event + fail-open allow; zero claims → no runner calls, no events, spawn proceeds).
- Wire the checker into the spawn preflight chain in `internal/core/overseer.go` (`spawnWorker` — after `spawnPreflightPublishIdentity`, before `prespawnCloseIfGreen`) and pass the bead description payload.
- Test harness detail: `beadClaimsImplemented` self-probe calls `parseBeadClaims("claim: a (verify: echo a)")` and returns (implemented) only when the parse yields 1 claim with VerifyCmd `echo a`; remove or keep the helper per GREEN's preference — the skip gate is driven by `BEAD_CLAIMS_ENFORCE`.
- Coverage target from phase_specific criteria: measure and report coverage for the new code (`go test ./internal/core/ -run 'BeadClaims|DriftCheck' -count=1 -cover`).
