# red handoff (8uw4)

## What changed (files + one line each)
- internal/core/context_pack_chain_scan_test.go (new): 6 RED tests pinning the ContextPack chain-scan contract — terminal beads (completed/closed) excluded from the non-terminal member count (chain of 70 with 25 terminal counts as 45 → pack builds); over-cap chain (51 non-terminal > 50) skips pack writes entirely; skip log line reports all three numbers (`chain has %d members (%d non-terminal > %d); skipping pack for %s`); `ContextPackConfig.MaxChainMembers` yaml field exists with `max_chain_members` tag; `MaxChainMembersOrDefault()` nil-safe default 50; pack JSON marshal round-trip.
- docs/hydra/handoff/task-8uw4.md: this handoff.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'TestContextPack_ChainScan' -count=1` — PASS (excludesTerminalBeads green).
- `go test ./internal/core/ -run 'ContextPack|PackChain' -count=1` — PASS (18 tests in the ContextPack scope).
- Note: the chain-scan feature (non-terminal member count + configurable cap in worker_spawner.go ensureContextPack + MaxChainMembers in config.go) was already present in the worktree before this bead's first turn; the RED tests were written against the existing implementation and pass. Full-suite gate re-verification is daemon-owned.

## Open edges for the next phase
- The pre-existing implementation satisfies all parent ACs; GREEN phase should confirm the skip-log format string matches exactly and add any missing config-load wiring (rawConfig copy) if absent.
