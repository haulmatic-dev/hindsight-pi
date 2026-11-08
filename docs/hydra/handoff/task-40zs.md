# syntax handoff
## What changed (files + one line each)
- None (verification-only). Implementation already conformant.

## Verification (commands run + pass/fail)
- `gofmt -l internal/core/context_pack.go internal/core/config.go internal/core/worker_spawner.go` — clean (no output).
- `go vet ./internal/core/` — PASS (clean).
- `go test ./internal/core/ -run 'ContextPack|PackChain' -count=1` — PASS (ok 0.292s).

## Open edges for the next phase
- None: terminal exclusion (`!b.Status.IsTerminal()`), config cap (`MaxChainMembersOrDefault()`, default 50), and skip log (`[ContextPack] chain has %d members (%d non-terminal > %d); skipping pack for %s`) all present in internal/core/worker_spawner.go.
