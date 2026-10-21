# discovery handoff (task-hqva)

## What changed (files + one line each)
- docs/hydra/handoff/task-hqva.md: this handoff (discovery bead; verify-first close — FR-001 ContextPack chain-scan terminal exclusion is ALREADY IMPLEMENTED and green on this branch; no code change).
- CHANGELOG.md: discovery entry for FR-001 (added in prior turn, commit d8966468).

## Verified findings (AC mapping)
1. **Terminal exclusion in member count** — MET. `internal/core/worker_spawner.go:969`: only `!b.Status.IsTerminal()` members count toward the cap; terminal fix-beads are fetched (one `br show` each, line 523 GetBead path) but never inflate the count.
2. **Config field** — MET. `internal/core/config.go`: `ContextPackConfig.MaxChainMembers int \`yaml:"max_chain_members"\``, `DefaultMaxChainMembers = 50`, nil-safe `MaxChainMembersOrDefault()` (0 → 50).
3. **Both-counts skip log line** — MET. `worker_spawner.go:974`: `log.Printf("[ContextPack] chain has %d members (%d non-terminal > %d); skipping pack for %s", ...)` exactly as AC3 specifies.
4. **Tests** — `internal/core/context_pack_chain_scan_test.go` (commit 03e3e764) pins all three behaviors: TestContextPack_ChainScanExcludesTerminalBeads, _ChainCapSkipLineReportsBothCounts, _MaxChainMembersConfigField, _MaxChainMembersOrDefaultNil, _SkipLogFormat, _PackJSONMarshal.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'ContextPack|PackChain' -count=1` — PASS (re-verified on HEAD 19470b4c).
- `git status --short` — clean; strand committed (daemon/ctx_execute path; fork-limit EAGAIN resolved by running commit through ctx_execute sandbox).

## Open edges for the next phase
- Terminal beads still cost one `br show` fetch each before exclusion; bounded by the cap, low value to optimize.
- If the validation gate flags missing target_files metadata on the RED bead, fix is bead metadata (br update), not code (same disposition as test-6lmz/test-udlg).
