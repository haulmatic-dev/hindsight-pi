# discovery handoff (task-rjkx)

## What changed (files + one line each)
- docs/hydra/handoff/task-rjkx.md: this handoff (discovery bead for FR-004: ContextPack chain scan terminal exclusion verification; no code change).
- CHANGELOG.md: Documentation entry under Unreleased pointing at the verification results.

## Verified findings (FR-001: ContextPack chain scan)
- Feature ALREADY IMPLEMENTED at HEAD; no code change required. Verified via code inspection + test run:
  - `internal/core/worker_spawner.go` (`ensureContextPack` region): chain members fetched via `s.beadsClient.GetBead`; terminal members (`b.Status.IsTerminal()`) counted separately in `nonTerminal`; the pack-skip cap compares only `nonTerminal` against `cfg.MaxChainMembersOrDefault()`, so terminal fix-beads cost a fetch but never contribute to cap pressure nor to `MemberDigests`/terms collection.
  - Cap is config: `internal/core/config.go` `ContextPackConfig.MaxChainMembers` (`yaml:"max_chain_members"`), default `DefaultMaxChainMembers = 50`, accessor `MaxChainMembersOrDefault()` (nil-safe).
  - RED tests landed locally: `internal/core/context_pack_chain_scan_test.go` (commit `03e3e764`) with 6 tests pinning terminal exclusion, both-counts skip log, config field, nil-safe accessor, log format, marshal.
- `go test ./internal/core/ -run 'TestContextPack_' -count=1` — PASS 8/8 (all ContextPack tests incl. the 6 chain-scan tests). VERIFIED.
- `hindsight_search`/CASS: queried twice; no anti-pattern matching this approach; only generic workflow rules returned.
- graphify-out/graph.json absent in cwd → rg fallback per protocol.

## Open edges for the next phase
- Commit `03e3e764` (RED tests) is NOT an ancestor of `origin/development` at last successful check — the tests are uncommitted-with-regards-to-upstream; a PR/merge must carry them. Note fork-limit (`EAGAIN`) prevented re-verification after first successful check.
- Optional GREEN hardening (NOT done, out of scope): terminal beads are still fetched per member (one `br show` each) even though excluded from the cap; could skip fetching beads already known terminal from chain metadata. Low value; current cost is bounded by MaxChainMembers.
