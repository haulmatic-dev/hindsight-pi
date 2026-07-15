# PRD: Worktree Integration Reliability

## 1. Overview

**Problem:** Per-bead work committed into chain worktrees never reaches the working branch: integration fires only on a chain-completion event, which never fires for singleton beads (worktree key = the bead's own ID) and is silently lost when the event is missed — a live run left ~300 worktrees with work stranded across divergent branches, requiring manual patch archaeology.
**Solution:** Integrate singleton worktrees immediately on verified close, and add a periodic reconciliation sweep that integrates any orphaned `hydra/wt-*` branch whose worktree is no longer owned by an active worker.
**Success Metrics:**
- A singleton bead's committed work is merged into the working branch as part of its close.
- Orphaned `hydra/wt-*` branches (no active worker) are integrated or explicitly left with a logged conflict within one sweep interval.
- No committed work remains stranded solely because a completion event was missed.

---

## 2. User Stories

### US-001: Committed worker output reaches the working branch without manual archaeology
**As a** Hydra operator
**I want** every closed bead's committed worktree work merged back automatically
**So that** a finished run yields an up-to-date working branch instead of hundreds of stranded worktree branches.

**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Singleton Integrate-on-Close
- FR-002: Orphaned-Branch Reconciler
- FR-003: Reconciler Scheduling

**Acceptance Criteria:**
- `go test ./internal/core/...` exits 0
- After a singleton bead's commit-on-close, its `hydra/wt-<bead-id>` branch no longer exists and its commit is reachable from the working branch
- No mocking of internal dependencies in the integration test

---

## 3. Functional Requirements

### FR-001: Singleton Integrate-on-Close
**Type:** fr
**Priority:** P1

**Target Files:**
- `internal/core/bead_closer.go`

**Exports:**
- `(*BeadCloser) Close(worker *types.Worker, success bool, exitCode int) error`

In `(*BeadCloser).Close`, inside the existing commit-on-close block (the `success && reason == "completed" && c.worktreeManager != nil && c.chainKeyFn != nil && bead != nil` branch), after a successful `Commit`: compute `key := c.chainKeyFn(*bead)`; if `key == bead.ID.String()` the bead is a singleton chain (it belongs to no registered multi-bead chain — the chain-completion event that would normally trigger integration will never fire for it), so call `c.worktreeManager.IntegrateAndRemove(projectPath, key)` immediately. Log `[Worktree] integrated singleton <bead-id>` on success; on error log `[Worktree] integrate failed for <bead-id>: <err>` and continue (the close itself must not fail — the reconciler is the retry path). Multi-bead chains (`key != bead.ID.String()`) keep the existing event-driven integration and are NOT integrated here.

**Acceptance Criteria:**
- With a real git repo fixture: a singleton bead whose worktree has a commit, on the commit-on-close path, ends with its `hydra/wt-<bead-id>` branch merged into the working branch and the worktree removed
- A bead whose `chainKeyFn` returns a value different from its own ID does NOT trigger integration in `Close`
- An integration error does not cause `Close` to return an error (close proceeds; a log line matching `/integrate failed/` is emitted)
- `go build ./...` exits 0
- `go test ./internal/core/...` exits 0

**Depends On:** —

---

### FR-002: Orphaned-Branch Reconciler
**Type:** fr
**Priority:** P1

**Target Files:**
- `internal/core/worktree_reconciler.go`

**Exports:**
- `ReconcileOrphanedWorktrees(m *WorktreeManager, projectPath string, activeKeys map[string]bool) (integrated int, conflicts int)`

Create `internal/core/worktree_reconciler.go` with a package-level function `ReconcileOrphanedWorktrees(m *WorktreeManager, projectPath string, activeKeys map[string]bool) (integrated int, conflicts int)`. Behavior: list the project's worktree branches by running `git for-each-ref --format=%(refname:short) refs/heads/hydra/wt-*` in `projectPath`; for each branch, derive its key by stripping the `hydra/wt-` prefix; skip keys present in `activeKeys` (a worker is still using that worktree); for the rest call `m.IntegrateAndRemove(projectPath, key)` — count successes in `integrated`; on error count in `conflicts` and log `[WorktreeReconcile] <key>: <err>` (IntegrateAndRemove already aborts conflicted merges and leaves the worktree for inspection). If the working branch has no `hydra/wt-*` branches or `projectPath` is not a git repo, return `(0, 0)` without error. Emit one summary log line `[WorktreeReconcile] project=<path> integrated=<n> conflicts=<n>` only when `integrated+conflicts > 0`.

**Acceptance Criteria:**
- With a real git repo fixture containing two `hydra/wt-*` branches with commits (worktrees already removed or present): `ReconcileOrphanedWorktrees` merges both into the working branch and returns `integrated == 2, conflicts == 0`
- A branch whose key is in `activeKeys` is skipped (branch still exists after the call)
- A branch that conflicts with the working branch is counted in `conflicts`, the merge is aborted (working branch tree unchanged), and the branch still exists
- A non-git `projectPath` returns `(0, 0)`
- `go build ./...` exits 0
- `go test ./internal/core/...` exits 0

**Depends On:** —

---

### FR-003: Reconciler Scheduling
**Type:** fr
**Priority:** P1

**Target Files:**
- `internal/core/charon.go`

Wire the reconciler into Charon's periodic cleanup, immediately before the existing hourly stale-worktree GC block in `performCleanup` (the sweep must run first so committed-but-unintegrated work is merged before the GC removes old worktree directories). Reuse the existing hourly throttle (`j.lastWorktreeGC`): inside that same `if time.Since(j.lastWorktreeGC) > time.Hour` block, before launching `CleanupStaleWorktrees`, iterate `j.getProjects()` (skip if the provider is nil) and for each project run `ReconcileOrphanedWorktrees(NewWorktreeManager(), project.Path, active)` in the same goroutine that runs the GC, using the same `active` key set already built there. Order inside the goroutine: reconcile all projects first, then `CleanupStaleWorktrees`.

**Acceptance Criteria:**
- Within the hourly block, reconciliation runs before `CleanupStaleWorktrees` (verifiable by code order in the launched goroutine)
- When `j.getProjects` is nil the reconciler is skipped and the GC still runs
- `go build ./...` exits 0
- `go test ./internal/core/...` exits 0

**Depends On:**
- FR-002: Orphaned-Branch Reconciler

---

## 4. Non-Functional Requirements

### NFR-SYS: System Integration Verification
**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- Full project builds with `go build ./...` and exits 0
- `go test ./internal/core/...` exits 0
- No empty files or placeholder comments remain in `Target Files` of any closed bead
- `internal/core/bead_closer.go`, `internal/core/worktree_reconciler.go`, and `internal/core/charon.go` all compile as part of the package

**Verifies:**
- FR-001: Singleton Integrate-on-Close
- FR-002: Orphaned-Branch Reconciler
- FR-003: Reconciler Scheduling

---

## 5. Technical Considerations

### Architecture
- Two complementary paths: eager (singleton close — the common case, immediate) and reconciling (sweep — catches missed events, multi-bead chains whose completion event was lost, and work stranded by crashes/restarts). Both funnel through the existing `IntegrateAndRemove`, which already handles merge conflicts by aborting and leaving the worktree for inspection.
- The reconciler is a package-level function over `WorktreeManager` (no new state) so Charon can call it without new dependencies.

### Technology
- Go standard library + `git` CLI (already the pattern in `worktree_manager.go` via `gitOut`). Tests use real temp git repos (`t.TempDir()` + `git init`), the same approach as `worktree_manager_test.go`.

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `internal/core/bead_closer.go` | fr | — | — |
| FR-002 | `internal/core/worktree_reconciler.go` | fr | — | — |
| FR-003 | `internal/core/charon.go` | fr | FR-002 | — |
| US-001 | — | us-integration | FR-001..003 | — |
| NFR-SYS | — | nfr-verification | FR-001..003 | — |

### Project Structure

The deliverable adds the following source and test files under `internal/core/`:

```
internal/core/
├── bead_closer.go
├── bead_closer_singleton_integration_test.go
├── charon.go
├── worktree_reconciler.go
└── worktree_reconciler_test.go
```

### Edge Cases
- Branch exists but worktree directory already GC'd: `IntegrateAndRemove` handles it (`isGitWorktree` false → merge branch, delete branch, prune).
- Merge conflict: abort, leave branch + worktree, count as conflict, never block the sweep.
- Working branch checked out in the main checkout with uncommitted changes: `git merge` may fail — treated as a conflict (logged, skipped).
- Empty branch (commit-on-close committed nothing): merge is a no-op fast-forward; branch cleanup still runs.

### Constraints
- `Close` must never fail because integration failed.
- The reconciler must never force-merge: conflicts are surfaced, not resolved.

---

## 6. MoSCoW

**Must Have:** FR-001 eager singleton integration; FR-002 reconciler; FR-003 scheduling before GC.
**Should Have:** Conflict counting + summary log.
**Could Have:** Per-project reconcile metrics exposed via stats.
**Won't Have:** Automatic conflict resolution; changes to multi-bead chain event-driven integration.

---

## 7. Research Insights

### Codebase Findings
- Commit-on-close block: `internal/core/bead_closer.go` ~line 143 (`success && reason == "completed" && c.worktreeManager != nil ...`) — FR-001 slots directly after the successful `Commit` call; `projectPath` is in scope.
- `chainKeyForBead` (`internal/core/chain_lookup.go`) returns the chain root ID or, for beads in no registered chain, the bead's own ID — the singleton test is `key == bead.ID.String()`.
- Event-driven integration lives in `internal/core/chain_completion_initializer.go` (callback → `IntegrateAndRemove(projectPath, RootBeadID)`) and remains unchanged.
- `IntegrateAndRemove`, `gitOut`, `branchExists` in `internal/core/worktree_manager.go`; branch naming `hydra/wt-<sanitized-key>` via `branchName`. Note `sanitizeKey` — keys derived from branch names are already sanitized, and `IntegrateAndRemove` re-sanitizes idempotently.
- Charon hourly worktree block: `internal/core/charon.go` `performCleanup` (`j.lastWorktreeGC` throttle) — FR-003 extends that block.

### Historical Patterns
- Live run 2026-07-13: 40-bead PRD completed with correct code, but ~300 worktrees accumulated and none integrated; the deliverable was assembled by hand from the shared checkout. This PRD is the countermeasure.
