# test handoff (test-od7z)

## What changed (files + one line each)
- docs/hydra/handoff/test-od7z.md: this handoff (validation round 1 verify-first close; no code change — the graphify-out symlink feature + refactor + tests already exist on hydra/wt-hydra-t084).

## Verification (commands run + pass/fail)
- Verify-first: the claimed gap "RED bead has no test files (target_files metadata missing)" is stale — `internal/core/worktree_graphify_symlink_test.go` (2 tests) exists on branch `hydra/wt-hydra-t084`, which holds the full chain: 219ce7f `feat(worktree): symlink main checkout's graphify-out into chain worktrees` + 62ac1c5 `refactor(worktree): decompose linkGraphifyOut into removeStaleGraphifyLink + ignoreGraphifyOut`.
- `go test ./internal/core/ -run 'Graphify' -count=1` (branch archive in /tmp/t084-val) — PASS, ok 12.535s.
- `-run 'Graphify|TestWorktree' -v` — 2/2 symlink tests PASS (TestWorktreeGraphifySymlink_LinksGraphFromMainCheckout, TestWorktreeGraphifySymlink_NoGraphNoSymlink); 13 TestGraphifyIndexer tests PASS.
- `go build ./...` (branch archive) — PASS.
- Refactor confirmed in tree: `removeStaleGraphifyLink` (worktree_manager.go:326) + `ignoreGraphifyOut` (:347); grep count = 1 each of the call sites at :298/:308.
- Validation worktree cwd (hydra/wt-hydra-od7z) intentionally NOT modified — the fix is bead metadata (`br update target_files` on the RED bead), not code (same disposition as test-6lmz / test-82sj / test-udlg / test-izp2 handoffs).
- hindsight_search at bead start — not connected (skip rule applied).

## Open edges for the next phase
- The t084 strand is NOT yet landed on origin/development (`git merge-base --is-ancestor 62ac1c5 origin/development` → no). Integration/publish of hydra/wt-hydra-t084 is the next step, not rework.
- If the validation gate still flags missing target_files metadata, the fix is bead metadata (br update), not the test suite.
