# test handoff (test-ikf7)

## What changed (files + one line each)
- internal/core/bead_closer_hard_gates_test.go (new, cherry-pick b06d73e): RED suite — 10 tests pinning runCloseHardGates contract (verification-command reject/pass/fail-open-missing, quarantine reject/pass/no-branch, closeDep chain-member reject/pass/singleton-warn, VerifiedClose hard-gate round-cap escalation).
- internal/core/bead_closer_hard_gates.go (new, cherry-pick eae3601): GREEN implementation — executable-acceptance, quarantine-contradiction, dependency-order hard gates; fail-open on missing config.
- internal/core/bead_closer.go: Close() success/completed path runs runCloseHardGates before VerifiedClose (gate failure never costs an LLM judge round).
- CHANGELOG.md, docs/hydra/handoff/task-3xpz.md, docs/hydra/handoff/task-b9xf.md: entries carried by the cherry-picks.

## Verification (commands run + pass/fail)
- Verify-first: prior GREEN strand (commits b06d73e/eae3601 on hydra/wt-hydra-b9xf) was NOT landed on this branch (merge-base ancestor check: NOT-LANDED ×3) — cherry-picked both commits into this worktree (clean applies).
- `go test ./internal/core/ -run 'TestCloseGate|TestQuarantineClose|TestCloseDep|TestVerifiedClose' -count=1` — PASS (ok, all 10 hard-gate tests).
- `go build ./...` — PASS. `go vet ./internal/core/` — PASS. `gofmt -l` on touched files — clean.
- Wiring verified: `runCloseHardGates` call at bead_closer.go:259 in Close() success path.
- The bead's TDD gap ("RED bead hydra-3xpz has no test files") is stale/metadata: the RED test file now exists on this branch (internal/core/bead_closer_hard_gates_test.go, 10 tests). If the gate still flags it, fix is bead metadata (br update target_files), not the suite (same disposition as test-6lmz / test-izp2 / test-82sj).

## Open edges for the next phase
- Commits are on this worktree branch only; integration/PR landing handled by the daemon publish path.
- Sibling strand hydra/wt-hydra-b9xf also carries a11cb48 ("RED absorbed GREEN") — do not merge wholesale; these cherry-picks are the canonical RED/GREEN separation.
