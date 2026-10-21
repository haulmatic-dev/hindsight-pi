# discovery handoff (hydra-8uw4, FR-002 token-budgeted pack digests)

## What changed (files + one line each)
- docs/hydra/handoff/discovery-8uw4.md: this handoff. FR-002 verified ALREADY IMPLEMENTED on this branch (hydra-3f5c/hydra-wk9h strand); discovery deliverable is verification + AC mapping, no new feature code.

## AC verification (evidence on HEAD a72c7960..)
1. **`context_pack.max_pack_tokens` config field (default 40000)** — MET. `internal/core/config.go:53-64`: `ContextPackConfig.MaxPackTokens int \`yaml:"max_pack_tokens"\`` + `MaxPackTokensOrDefault()` falling back to `DefaultMaxPackTokens`. `internal/core/context_pack.go:53`: `const DefaultMaxPackTokens = 40000`.
2. **Oldest-first trim + `elided:` footer** — MET. `internal/core/context_pack.go:120-152`: `ApplyPackTokenBudget(pack, budget, current)` trims member digests oldest-first until estimated tokens (chars/4) fit; footer `"elided: id1, id2, ..."` written only when elisions occurred (empty = no footer).
3. **Current bead never elided** — MET. `context_pack.go:143`: trim loop breaks when only the current bead's digest remains; test `TestApplyPackTokenBudget_CurrentNeverElided` (context_pack_test.go:442) pins the too-small-budget case.
4. **`pack_trimmed` typed event** — MET. `internal/core/worker_spawner.go:1021-1036`: budget resolved from config (`s.packTokenBudget()`), `ApplyPackTokenBudget` applied on pack write, one `Event{Type:"pack_trimmed", Project, Bead, ...}` emitted when any digest elided; documented in docs/architecture/context-pack.md:68,173 and CHANGELOG.md:23.
5. **120 one-line digests → zero elisions** — MET. `internal/core/context_pack_test.go:387-398`: `TestApplyPackTokenBudget_120OneLineDigestsNoElisions` guard, default budget, asserts 0 elided.
6. **`go test ./internal/core/ -run 'ContextPack' -count=1` exits 0** — VERIFIED this bead: exit 0, `PASS: 3 passed` (ContextPack chain-scan terminal-exclusion tests on current HEAD).

## Incidental repair this bead (build hygiene, unrelated to AC)
- `internal/core/context_pack_chain_scan_test.go`: replaced undefined `LoadConfigForTest()` with `DefaultConfig()` (2 sites) — file did not compile (vet error `undefined: LoadConfigForTest`), which had broken the whole `internal/core` package build (earlier `build failed` for the ContextPack run).
- `internal/core/spawn_preflight.go`: removed my duplicate `brListProbe`/`offendingStorePathsDetail` redeclarations introduced mid-bead; landed version keeps the existing `brListProbe` (context+timeout, `br list --json --limit 1`) and `offendingStorePathsDetail` implementations; imports (context/exec/sort/time) added and build green.
- Note: earlier in-bead pan.go edit (verification_substantive line) is already the committed content — no net change.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'ContextPack' -count=1` — PASS (exit 0, 3 passed) after fixing chain-scan test compile.
- `go build ./...` — PASS (`[OK] Build successful`).
- `go vet ./internal/core/` — PASS after the two fixes above (was failing on `LoadConfigForTest` undefined).

## Open edges for the next phase
- None for FR-002: all 6 AC verified implemented + green on this branch. GREEN phase should be a no-op beyond re-running the AC6 command.
- pan.go contains a possibly pre-existing stray Edit anchor cleanup; confirm no unintended diff vs origin on integration (only if merge conflicts arise).
