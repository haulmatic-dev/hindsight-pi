# test handoff (test-ave1)

## What changed (files + one line each)
- docs/hydra/handoff/test-ave1.md: this handoff (validation round 1 verify-first close; no code change — the Token-budgeted pack digests test suite already exists and passes).

## Verification (commands run + pass/fail)
- Verify-first: `go test ./internal/core/ -run 'ContextPack|PackChain' -count=1` — PASS (`ok github.com/haulmatic-dev/hydra/internal/core 0.303s`). The claimed gap "RED bead hydra-3f5c has no test files (target_files metadata missing)" is stale: `internal/core/context_pack_test.go` exists with 9 `ApplyPackTokenBudget` references (incl. TestApplyPackTokenBudget_CurrentNeverElided, _120OneLineDigestsNoElisions), implementation in context_pack.go + worker_spawner.go (pack_trimmed event) confirmed by grep.
- Provenance: FR-002 landed on this branch (context_pack.go:120-152 ApplyPackTokenBudget, DefaultMaxPackTokens=40000; config.go MaxPackTokens knob; worker_spawner.go event emission) — recorded in discovery-8uw4.md / fix-8d3h / syntax-6vpk handoffs as all-6-AC MET.
- `git status --short` — only this handoff added; no source changes.
- hindsight_search at bead start — tool unavailable (skip rule applied). graphify query ran (BFS over pack-token Budget/PackChain nodes).
- Note: shell intermittently hit `fork: retry: Resource temporarily unavailable` (host resource exhaustion); verification commands succeeded on retry.

## Open edges for the next phase
- Same disposition as sibling handoffs (test-6lmz/test-udlg/test-izp2): if the validation gate still flags missing target_files metadata for the RED bead hydra-3f5c, the fix is bead metadata (br update target_files), not the test suite.
