# fix handoff (qb50)

## What changed (files + one line each)
- docs/hydra/handoff/fix-qb50.md: this handoff. No source change — verify-first close. The original hydra-hrmx failure was a model-call infra error ("prompt is too long: 1031985 tokens > 1000000 maximum"), not a code defect.

## Verification (commands run + pass/fail)
- Classifier (bead_closer.go `getVerificationFingerprint`/`trackVerificationFingerprint`/`verificationFingerprintSource`, pan.go prompt exclusion) present at HEAD.
- In a scratch worktree of HEAD with the unrelated, non-compiling mirror-health files removed plus a no-op `sweepMirrorHealth` stub: `go test ./internal/core/ ./internal/pan/ -run 'Fingerprint|VerifiedClose' -count=1 -v` — PASS (6 fingerprint tests incl. FirstOccurrence, Repeat_EmitsSubstantiveEvent, DifferentFingerprint, NilClient, Deterministic, IgnoresStderr); `go vet ./internal/core/` clean. Scratch worktree removed.

## Open edges for the next phase
- BLOCKER on this branch (not part of this bead): `internal/core/pan_mirror_health.go` does not compile at HEAD — `pan_mirror_health.go:256:4: syntax error: unexpected newline in argument list; possibly missing comma or )`, then undefined `mirroraudit.DefaultComparer`, `mirrorGuardHooks`, `mirroraudit.Diverness`, `now`. `go build ./...` and the core test package fail until the mirror-health chain (FR-3/FR-4) lands a coherent file. Not fixed here (scope).
