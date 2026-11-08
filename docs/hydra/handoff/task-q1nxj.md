# refactor handoff

Bead: hydra-q1nxj — REFACTOR: Optimize Junk signature package (content signatures + negative probes)

## What changed (files + one line each)

- `internal/junksig/junksig.go` — extracted `IsJunkPath`'s inline per-segment if-chain into a named `isJunkSegment(segs, i)` helper backed by table-driven signature sets (`junkExactSegments` map for `.jest-cache` / `.hydra-preflight-ok`, `junkSegmentPrefixes` slice for `.tmp` / `haste-map-` / `jest-transform-cache-`); the `.pi/task-breakdown` lookahead rule lives in the helper too. Behavior byte-identical; `IgnorePatterns()` output unchanged.
- `CHANGELOG.md` — Added the Unreleased/Changed entry for this behavior-preserving refactor.
- `docs/hydra/handoff/task-q1nxj.md` — this handoff.

No new signatures, no test edits, no contract changes. Known accepted residual (jest default `coverage/` not treated as junk) left as-is by design.

## Verification (commands run + pass/fail)

- BEFORE (pre-edit, same command): `go test ./internal/junksig/ -run 'TestJunksig_' -count=1 -v` → PASS, ok, 10/10 `--- PASS` (VERIFIED in this session, incl. negative probes thread-cache.service.ts, template.tmp, snapshot.tmp.png, src/pipeline/task-breakdown/step.ts, .pi/keep-config.json all correctly not ignored).
- AFTER: `go test ./internal/junksig/ -run 'TestJunksig_' -count=1 -v` → `--- PASS` count = 10 (grep -c), `ok github.com/haulmatic-dev/hydra/internal/junksig`. PASS.
- `gofmt -l internal/junksig/` → empty (clean). PASS.
- `go vet ./internal/junksig/` → exit 0. PASS.
- `go vet ./...` → exit 0, no output. PASS.
- Benchmarks: none exist in the package (`TestJunksig_` suite only) — nothing to check.

## Open edges for the next phase

- `junkExactSegments` is a small map; if more exact-segment signatures land it stays O(1) and name-driven — keep adding entries there rather than reopening `IsJunkPath`.
- `coverage/` residual remains deliberate; do not add it without a new bead/PRD decision.
