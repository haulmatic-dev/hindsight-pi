# green handoff

## What changed (files + one line each)
- internal/core/context_pack.go: ApplyPackTokenBudget — trims member digests oldest-first until chars/4 fits budget (default 40000); current bead's digest never elided; writes `elided: id1, id2, ...` footer.
- internal/core/config.go: ContextPackConfig.MaxPackTokens + MaxPackTokensOrDefault.
- internal/core/context_pack_test.go: budget/trim/never-elide tests + 120-one-line-digest guard (zero elisions).

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'ContextPack' -count=1` — ok, exit 0. PASS.
- `go build ./...` — exit 0. PASS.
- Coverage: ApplyPackTokenBudget 89.5%, MaxPackTokensOrDefault 100.0%.

## Open edges for the next phase
- pack_trimmed typed event emission: verify caller (worker_spawner) emits event with {bead, elided_count, budget} — apply site exists; event emission is caller-side.
