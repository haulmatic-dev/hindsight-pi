# refactor handoff

## What changed (files + one line each)
- internal/beadfiles/resolve.go: named package vars (`metadataKeys`, `testFileSuffixes`, `filesLinePrefixes`) replace inline slices; `contextJSONFiles` split into `contextJSONBlock` (fence scan) + JSON decode. Behavior identical.
- CHANGELOG.md: Unreleased/Changed entry.

## Verification (commands run + pass/fail)
- BEFORE: `go test ./internal/beadfiles/ -run 'TestResolve_|TestIsTestFile_' -count=1` — ok.
- AFTER: same with -v — 34 `--- PASS` lines (parents + subtests), 0 fail; `go test ./internal/beadfiles/` ok; `go vet` and `gofmt -l` clean; `go build ./internal/beadfiles/` ok.
- No benchmarks exist for this package.

## Open edges for the next phase
- No adopters yet; adoption of `Resolve` in internal/core is a later bead.
