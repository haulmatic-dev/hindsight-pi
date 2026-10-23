# GREEN handoff

## What changed (files + one line each)
- `internal/beadfiles/resolve.go` (new): shared, single-source target-file resolver package. `Resolve(bead *types.Bead) []string` mirrors `internal/core`'s `beadTargetFiles` exactly — metadata `files` has precedence over metadata `target_files` (only a non-blank metadata value is consulted), then the first description line whose lowercase form starts with `files:`, whose remainder is passed to `SplitFileList`. Returns nil for a nil bead, a zero-value bead, a `Files: TBD` line and prose that does not start with the `files:` prefix. `SplitFileList` mirrors `internal/core`'s `splitFileList` — comma/semicolon/space/tab separators, `TBD` skipped case-insensitively, blanks dropped.
- `internal/beadfiles/resolve_test.go` (new): 13 unit tests — `TestResolve_NilBead`, `TestResolve_ZeroValue`, `TestResolve_MetadataFiles`, `TestResolve_MetadataTargetFiles`, `TestResolve_MetadataFilesTakesPrecedence`, `TestResolve_MetadataBlankSkipped`, `TestResolve_DescriptionFallback`, `TestResolve_DescriptionFirstFilesLineWins`, `TestResolve_DescriptionCaseInsensitivePrefix`, `TestResolve_DescriptionTBDIgnored`, `TestResolve_DescriptionNotInLine`, `TestResolve_MetadataBeatsDescription`, `TestSplitFileList` (13 sub-cases: empty, spaces, commas, semicolons, tabs, mixed, `TBD` variations, whitespace-only, duplicate).
- `CHANGELOG.md`: "### Added" entry under Unreleased describing the new package and deferring adoption.

## Verification (commands run + pass/fail)
- `go test ./internal/beadfiles/ -run 'TestResolve_|TestSplitFileList' -count=1 -v` — PASS, 13 tests, all `--- PASS`, `ok github.com/haulmatic-dev/hydra/internal/beadfiles`.
- `gofmt -l internal/beadfiles/` — clean.
- `go build ./...` — exit 0 (BUILD_OK).
- `go vet ./internal/beadfiles/` — exit 0 (VET_OK).
- Floating-point comparisons — not applicable to this package (no floating-point arithmetic).

## Open edges for the next phase
- No caller in `internal/core` imports `internal/beadfiles` yet — adoption (replacing `beadTargetFiles`/`splitFileList` in `internal/core/bead_closer.go` and re-routing `internal/core/gate_close.go`, `prespawn_probe.go`, `fix_audit.go`, `satisfied_close.go`, `integration_retest.go`, `fix_cause.go` clones) is the next bead.
- `internal/core/bead_closer_verifiedclose_test.go`'s `TestBeadTargetFiles` remains the core-scope test until then.
