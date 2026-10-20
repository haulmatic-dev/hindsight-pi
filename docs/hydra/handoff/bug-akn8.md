# bug handoff (akn8)

## What changed (files + one line each)
- internal/core/worktree_manager.go: `ExistingWorktree` now validates identity before returning a chain worktree — a path that is a git worktree of a DIFFERENT repo (mis-rooted, e.g. attached to the parent monorepo's gitdir while the project is a submodule) returns ("", false) + typed `worktree_refused/misrooted_gitdir` event; `ensureWorktreeLocked` post-`worktree add` assertion removes+fails-closed a mis-rooted creation ("worktree %s mis-rooted: gitdir does not match project repo %s (removed)").
- internal/core/bead_closer.go: verified-close evidence reads FAIL-CLOSED — `collectCloseEvidence` returns an error when (worktree mode) the evidence-repo diff is unreadable, or (main mode) status fails AND the committed-evidence fallback also fails; `unionMainCheckoutLeaks` returns its error and it is fatal when changed is empty; `verifyBeforeClose` rejects the close with reason `cannot-verify-diff` (reopen via retry ladder). Root mechanism: `hasTargetChanges(nil, targets)` = true ("unknown, fail open"), so the old "not blocking" log path let zero-work closes accept.
- internal/core/bead_closer_verifiedclose_test.go: `TestVerifyBeforeClose_ChangedFilesErrorFailsOpen` flipped to `TestVerifyBeforeClose_ChangedFilesErrorFailsClosed` (rejection with `cannot-verify-diff` pinned).
- internal/core/worktree_misroot_test.go (new): 7 tests — ExistingWorktree rejects foreign gitdir / accepts submodule-layout own worktree; collectCloseEvidence worktree-mode diff error fatal, main-mode both-sources-broken fatal, main-mode committed-evidence rescues status error (fail-open narrow), main-leak-union error fatal at zero evidence; end-to-end cannot-verify-diff rejection.
- CHANGELOG.md: Fixed entry under Unreleased.

## A1 reproduction (defect existed on base)
- Live evidence: hydra-q3p worktree gitdir was `/opt/dev-tools/.git` (monorepo) while project path is the hydra submodule — `git status` ran against monorepo (deleted files .gitignore/.gitmodules/components/hydra, tip 071e5f6 "chore: bump submodules").
- Code-level repro on clean base: `hasTargetChanges(nil, targets)` returns TRUE (scope_gate.go:125-127) while `collectCloseEvidence` logged `[VerifiedClose] diff check error ... (not blocking)` and left changed=nil → the needDiff gate was skipped entirely → close accepted with zero verified work. Diff-read failure ⇒ acceptance, by construction.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS. `go vet ./internal/core/` — PASS. `gofmt -l` on touched files — clean.
- `go test ./internal/core/ -run 'TestVerifyBeforeClose|TestExistingWorktree|TestIsOurWorktree|TestWorktree' -count=1` — PASS after flipping the fail-open test (initially FAIL: `changedFiles error should fail open: ... rejected (cannot-verify-diff)` — RED confirming the new behavior).
- Narrow set `TestClose|TestBeadCloser|TestVerifiedClose|TestWorktree|TestIntegrateStranded|TestIsOurWorktree` — PASS.
- Full suite: NOT RUN (daemon gate owns it).

## AC mapping
- Guard 1 (submodule-aware gitdir validation on creation/reuse): TestExistingWorktree_RejectsForeignGitdir + TestExistingWorktree_AcceptsOursSubmoduleLayout + post-add assertion — VERIFIED (unit).
- Guard 2 (verified-close fail-closed): TestVerifyBeforeClose_ChangedFilesErrorFailsClosed + 4 collectCloseEvidence tests — VERIFIED (unit).
- Guard 3 (reuse validates branch+repo before spawn): identity validation in ExistingWorktree covers all spawn/close call sites (worker_spawner.go:549, bead_closer.go 226/347/1421, service_starter.go:340, fix_audit.go:148); branch-mismatch repair/refusal pre-existed in ensureWorktreeLocked (wrong-branch repair + unintegrated refusal) — VERIFIED (unit for identity; branch repair pre-existing).
- LIVE: reopen + re-execute xnk7/r2rl/htq0 and land integrably — NOT DONE by this bead (operator reopens after this lands, per bead text); all three worktrees verified correctly rooted (gitdir `/opt/dev-tools/.git/modules/components/hydra`) as of run.
- Post-add mis-rooted-creation assertion: defense-in-depth, not directly unit-testable without an exec seam (no mis-rooting injection point); covered by identity check on reuse — NOT VERIFIED by a dedicated test.

## Open edges for the next phase
- `cannot-verify-diff` rejections consume a VerifiedClose round; persistent unreadable evidence (e.g. durable dubious ownership) escalates after 3 rounds via the existing ladder — expected fail-closed behavior, watch for churn on hosts with git-trust problems (git-ownership preflight hydra-hlen should pre-empt).
- Post-close sweep (`postclose-escape`) runs `git status` in project checkouts; same dubious-ownership class could surface there — fail-open by design there (verification rerun, not evidence gate).
