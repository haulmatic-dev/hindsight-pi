# red handoff

## What changed (files + one line each)
- No code changes in this bead. The RED tests and the implementation for
  "Store-ownership preflight precision" (FR-005) were already landed in
  commit `110aa7f4` ("Fix store ownership preflight precision (FR-005)"),
  which is the parent of this worktree's HEAD.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'StoreOwnership' -count=1` — PASS (all 6 tests green).
- Coverage for new preflight code (FR-005):
  - `storeOwnerUID`: 80.0%
  - `spawnPreflightStoreOwnership`: 87.5%
  - `brListProbe`: 100.0%
- One test (`TestSpawnPreflightStoreOwnership_ForeignOwnerWarnsAndPasses`) is
  env-gated SKIP in the current container (uid 999 cannot create a
  foreign-owned store), but the same scenario is covered by
  `TestStoreOwnershipPreflight_ProbeFailureWarnsWithPaths` (which asserts the
  warn + typed event on probe failure) and
  `TestStoreOwnershipPreflight_CapsOffendingPathsAtThree`.

## Open edges for the next phase
- None. Feature is complete and verified in the worktree at HEAD
  (`03e3e764`).
