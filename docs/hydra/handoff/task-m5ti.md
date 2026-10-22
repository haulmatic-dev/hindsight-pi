# Handoff: task-m5ti (GREEN — Deterministic payload drift-check at spawn preflight)

Bead: hydra-m5ti · Phase: GREEN · Branch: `hydra/wt-hydra-kzlu` · Final commit: `9f563db`

## What changed

- `internal/core/bead_claims.go`: real implementation replacing the RED stub.
  - `BeadClaim{Hard, Assertion, VerifyCmd}`; `parseBeadClaims` parses `claim: <assertion> (verify: <cmd>)` lines (optional `hard:` prefix, verify clause optional).
  - `defaultClaimRun`: `sh -c` via `exec.CommandContext` with `claimVerifyTimeout = 2s` and `WaitDelay`; deadline wraps error as `context.DeadlineExceeded`.
  - `beadClaimChecker.checkBeadClaims`: hard fail → allow=false + `claim_hard_failed`; soft fail → allow + `claim_soft_failed`; tooling failure (deadline, exec.Error) or missing verify clause → allow + `claim_unknown` (fail-open). Zero claims → no-op, no events.
  - `isClaimToolingFailure`; `claimEventRecorder` package seam for tests.
- `internal/core/spawn_preflight.go`: `spawnPreflightBeadClaims` returns `SpawnSuppressedError{Reason:"hard claim failed: payload drift-check refused spawn"}`.
- `internal/core/overseer.go`: preflight hook wired after `spawnPreflightPublishIdentity`.
- `internal/core/events.go`: `EventSink.Emit` self-heals zero-value sink (nil `now` → neutral stub) — fixes panic on `&EventSink{}`.
- Tests: `bead_claims_test.go` (contract) + `bead_claims_default_runner_test.go` (real runner: exit code evidence, 2s timeout → unknown, stdout trim, stderr capture). Non-placeholder marker wording applied to comment.
- `CHANGELOG.md`: GREEN entry under `## [Unreleased]`.

## Verification

- `go build ./...` — exit 0.
- `go test ./internal/core/ -run 'BeadClaims|DriftCheck' -count=1` — exit 0 (18 subtests).
- Coverage: `defaultClaimRun` 93.8%, `isClaimToolingFailure` 87.5%, `checkBeadClaims` 88.2%, `parseBeadClaims` 84.6%, `emit` 100%.
- gofmt / `go vet` clean. No TODO/FIXME/stub markers.

## Open edges

- Full-package suite at HEAD not re-run locally (daemon gates own it); narrow scope green.
- Full-suite earlier run showed 2 unrelated failures attributed to wrapper flakiness; unresolved if they recur at gate.
- Environment intermittently throws `spawn /bin/bash EAGAIN` — retried where hit.
