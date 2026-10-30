# red handoff (hydra-ai97h)

## What changed (files + one line each)
- internal/junksig/junksig.go (new): RED-phase inert stub — `const junksigImplemented = false`, `IsJunkPath(p string) bool` returns false, `IgnorePatterns() []string` returns nil, full contract doc comments. Package compiles, zero behavior.
- internal/junksig/junksig_test.go (new): 10 contract tests under `TestJunksig_`, all gated on `junksigRequireImpl` (`JUNKSIG_ENFORCE=1`): RealWorldIncidentPaths (observed incident junk matches), NegativeProbes (cache-suffixed code files, `template.tmp`, `snapshot.tmp.png`, in-code `task-breakdown` never match), StructuralNotSubstring (never substring matches), RenamedParentStillMatches (`renamed/.jest-cache/blob`, `renamed/.tmp-jest/b`), BasenamesSignatures (`haste-map-`/`jest-transform-cache-` prefixes, exact `.hydra-preflight-ok` basename any depth), HydraPreflightExact, PiTaskBreakdownPrefix (everything under `.pi/task-breakdown/`), IgnorePatternsReproduceMatcher (hermetic `git check-ignore` property test, `GIT_CONFIG_GLOBAL/SYSTEM=/dev/null`), CoverageResidualNotSignature (jest `coverage/` accepted residual recorded), IgnorePatternsWellFormed.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `gofmt -l internal/junksig/` — clean.
- `go vet ./internal/junksig/` — PASS (clean).
- `go build ./...` — PASS (BUILD_OK).
- `go test ./internal/junksig/ -count=1` — PASS, exit 0; 10/10 `--- SKIP` loudly ("RED: junk signature contract not yet implemented") → daemon gate stays green pre-GREEN.
- `JUNKSIG_ENFORCE=1 go test ./internal/junksig/ -count=1` — FAIL, exit 1; all 10 fail for the RIGHT reason: `RED: junk signature contract not yet implemented (IsJunkPath/IgnorePatterns are inert stubs)` — proves RED detects the missing feature, not a compile error.
- `go test ./internal/junksig/ -count=1 -cover` — `ok ... coverage: 0.0% of statements` (stub is inert and gated tests skip before invoking it; GREEN will lift this).

## Contract GREEN must implement
- `IsJunkPath(p string) bool` — segment-structural matcher: any path segment with prefix `.tmp`; directory segment `.jest-cache`; basename prefixes `haste-map-` and `jest-transform-cache-`; exact basename `.hydra-preflight-ok` at any depth; everything under a `.pi/task-breakdown/` segment. Never substring matching.
- `IgnorePatterns() []string` — gitignore-format lines that reproduce the matcher exactly (verified by the hermetic `git check-ignore` test).
- Flip `junksigImplemented = true`.

## Open edges for the next phase
- Verification command: `go test ./internal/junksig/ -run 'TestJunksig_' -count=1 -v` (default gate) and `JUNKSIG_ENFORCE=1 go test ./internal/junksig/ -run 'TestJunksig_' -count=1` (RED proof until implemented).

HYDRA_VERDICT recorded in final agent message.
