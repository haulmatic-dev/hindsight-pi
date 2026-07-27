# PRD: Orphan Bead Recovery Correctness

## 1. Overview

**Problem:** Charon's orphan-bead recovery is unreachable in the exact situation it was written for, its documented 5-minute grace is never enforced, and when it does run it force-*closes* incomplete work instead of reopening it — so a worker dying mid-bead either stalls the swarm indefinitely (observed: a 65-minute dead stop during dogfood run D) or silently unblocks dependents on work that was never done.
**Solution:** Run orphan recovery before the tracked-worker early return, actually compare the bead's age against the grace period, and recover by reopening the bead (the mechanism `rejectClose` already uses) instead of force-closing it. Add Observer signatures so recovery bursts and quota exhaustion surface within one polling interval.
**Success Metrics:**
- An in_progress bead with no live worker is recovered even when zero workers are tracked (the run-D stall no longer possible).
- Recovery reopens the bead (`StatusOpen`) — dependents stay blocked until the work is actually done.
- A bead younger than the grace period is left alone; a bead with no usable timestamp is left alone.

---

## 0. Scope Profile
- **System Type:** library (internal Go packages inside the Hydra daemon) + CLI tool (observer)
- **Interfaces:** internal Go API (Charon cleanup cycle), observer log-signature table
- **Surfaces Explicitly Excluded:** worker-side changes, the stuck-*process* killer (separate concern, already works), bead schema changes, retry-policy changes
- **Deployment Model:** single-binary daemon + standalone observer binary

---

## 2. User Stories

### US-001: A dead worker never stalls or falsely unblocks the swarm — Integration Bead
**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Correct Orphan Recovery
- FR-002: Observer Signatures for Recovery and Quota

**Acceptance Criteria:**
- `go build ./...` exits 0 and `go test ./internal/core/...` exits 0
- `go test ./...` in `tools/hydra-observer` exits 0
- With zero tracked workers and one in_progress bead older than the grace period, a Charon cleanup cycle reopens that bead (status becomes `open`, not `closed`)
- No mocking of internal dependencies in the integration test

---

## 3. Functional Requirements

### FR-001: Correct Orphan Recovery
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/charon.go`, `internal/core/charon_orphan_test.go`

Three defects in one code path, fixed together because they are one behaviour and one file (the doctrine forbids splitting FRs that collide on a file):

1. **Reachability.** `performCleanup` (`charon.go:121`) reads `trackedWorkers` and returns early when the list is empty (`charon.go:~151-154`, `log "No tracked workers to check"`). Orphan recovery lives *after* that return (`charon.go:~258`), so it is skipped precisely when no workers are tracked — which is exactly when orphaned beads exist. Move the `recoverOrphanInProgressBeads()` call to run BEFORE that early return, so it executes on every cleanup cycle regardless of tracked-worker count. Keep the stuck-process killer exactly where it is.
2. **Grace enforcement.** `recoverOrphanInProgressBeads` (`charon.go:400`) reads `grace := j.orphanBeadGrace` (`:409`) and then only interpolates it into a log string (`:442`) — it is never compared against anything, so the documented 5-minute grace does nothing and a bead can be recovered the instant it is untracked (racing a worker that has spawned but not yet registered). Compare the bead's `UpdatedAt` against the grace: skip the bead when `time.Since(bead.UpdatedAt) < grace`. When `UpdatedAt` is the zero value, SKIP the bead and log once — acting without knowing the age is the race this fixes (**fail-closed on the destructive action**).
3. **Reopen, do not force-close.** The recovery currently calls `CloseBead(...)` (`:446`), which force-closes past the open-children guard. An orphaned bead means the work is *incomplete*, so closing it marks undone work as done and unblocks dependents — the precise failure class the verified-close gate exists to prevent. Replace it with `j.beadsClient.UpdateBeadStatus(project.Path, bead.ID, types.StatusOpen)` (the same mechanism `rejectClose` uses at `bead_closer.go:411`) so the scheduler re-attempts the bead. Keep the existing stats counters (`OrphanInProgressBeads`, `OrphanBeadsRecovered`, `LastOrphanBeadTime`) and log a line containing the exact literal `reopened orphan bead`.

**Acceptance Criteria:**
- With a fake beads client reporting one `in_progress` bead whose `UpdatedAt` is 10 minutes old, zero tracked workers, and `orphanBeadGrace` of 5 minutes: `recoverOrphanInProgressBeads()` returns `1` and the client recorded an `UpdateBeadStatus` call with status `open` for that bead
- In the same scenario, the client recorded ZERO `CloseBead` calls (recovery must never close)
- With the bead's `UpdatedAt` 1 minute old and a 5-minute grace: returns `0` and no `UpdateBeadStatus` call is recorded (grace respected)
- With the bead's `UpdatedAt` at its zero value: returns `0` and no `UpdateBeadStatus` call is recorded (no age, no action)
- With one live tracked worker for that bead ID: returns `0` regardless of age
- With `orphanBeadGrace` set to `0`: returns `0` (feature disabled, unchanged)
- A `performCleanup()` cycle with ZERO tracked workers and one 10-minute-old in_progress bead still reopens it (proves reachability past the early return)
- The recovery log line contains the exact literal string `reopened orphan bead`
- `go build ./...` exits 0 and `go vet ./internal/core/...` exits 0
- `go test ./internal/core/...` exits 0

**Depends On:** —

---

### FR-002: Observer Signatures for Recovery and Quota
**Type:** fr
**Priority:** P1

**Target Files:** `tools/hydra-observer/signatures.go`, `tools/hydra-observer/signatures_test.go`

Add two entries to `DefaultSignatures()` so both classes surface within one polling interval instead of being found by hand:

| Key | Marker | Severity | Threshold |
|-----|--------|----------|-----------|
| `orphan-recovery` | `reopened orphan bead` | warning | 3 |
| `quota-exhausted` | `usage limit` | critical | 1 |

Rationale for the thresholds: a single orphan recovery is normal operational hygiene, but a burst of three means workers are dying repeatedly and the underlying cause needs attention; quota exhaustion is always worth surfacing immediately because every subsequent bead will fail until it resets. Do not change the existing signature entries or `ScanLog`'s behaviour.

**Acceptance Criteria:**
- `DefaultSignatures()` contains an entry with `Key` equal to `"orphan-recovery"`, `Marker` equal to `"reopened orphan bead"`, `Severity` equal to `"warning"`, and `Threshold` equal to `3`
- `DefaultSignatures()` contains an entry with `Key` equal to `"quota-exhausted"`, `Marker` equal to `"usage limit"`, `Severity` equal to `"critical"`, and `Threshold` equal to `1`
- `ScanLog` over a fixture containing 3 lines with `reopened orphan bead` returns a Hit with `Key` equal to `"orphan-recovery"` and `Count` equal to `3`
- `ScanLog` over a fixture containing 2 such lines returns no `orphan-recovery` hit (below threshold)
- `ScanLog` over a fixture containing the single line `2026/07/27 09:50:16 403 {"error":{"type":"permission_error","message":"You've reached your usage limit for this billing cycle."}}` returns a Hit with `Key` equal to `"quota-exhausted"` and `Count` equal to `1`
- All pre-existing signature keys are still present in `DefaultSignatures()`
- `go build ./...` and `go test ./...` in `tools/hydra-observer` exit 0

**Depends On:** —

---

## 4. Non-Functional Requirements

### NFR-SYS: System Integration Verification
**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- Root `go build ./...` exits 0
- `go test ./internal/core/...` exits 0
- `go build ./...` and `go test ./...` in `tools/hydra-observer` exit 0
- No empty files or placeholder comments remain in `Target Files` of any closed bead

**Verifies:**
- FR-001: Correct Orphan Recovery
- FR-002: Observer Signatures for Recovery and Quota

---

## 5. Technical Considerations

### Architecture
- Recovery **reopens**, never closes: the only safe recovery for incomplete work is to let it be re-attempted. Closing is reserved for work that passed the verified-close gates.
- Fail-open / fail-closed, declared: listing beads or reopening may fail — that is logged and skipped (fail-open, recovery retries next cycle). The *decision to recover* is fail-closed: no usable timestamp or age under the grace means no action, because acting blindly is what races a just-spawned worker.
- The stuck-*process* killer keeps its existing position and behaviour; only the bead-state recovery moves.

### Technology
- Go standard library; a fake `BeadsClient` in the test records `UpdateBeadStatus`/`CloseBead` calls so the "never closes" property is asserted directly rather than inferred.

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `internal/core/charon.go`, `charon_orphan_test.go` | fr | — | — |
| FR-002 | `tools/hydra-observer/signatures.go`, `signatures_test.go` | fr | — | — |
| US-001 | — | us-integration | FR-001, FR-002 | — |
| NFR-SYS | — | nfr-verification | FR-001, FR-002 | — |

### Edge Cases
- Bead reopened but its worktree still holds partial work: acceptable — the worktree-aware verified-close gate evaluates whatever the next attempt produces.
- A worker that registers *after* the recovery reopened its bead: the bead is open, the worker's close will be gated normally; no corruption.
- Many orphans at once (mass worker death): each is reopened independently; the observer's threshold-3 signature surfaces the burst.
- `UpdatedAt` in the future (clock skew): `time.Since` is negative, therefore under the grace, therefore skipped — safe by construction.

### Constraints
- Recovery must never close a bead.
- No change to the existing stuck-process killer, retry policy, or bead schema.
- Observer stays observation-only.

---

## 6. MoSCoW

**Must Have:** Reachability, grace enforcement, reopen-not-close, the never-closes assertion.
**Should Have:** Both observer signatures.
**Could Have:** A metric for recoveries per run in `hydra-eval`.
**Won't Have:** Worker-side changes; auto-diagnosing *why* workers died (Observer Phase 1b); changes to the stuck-process killer.

---

## 7. Research Insights

### Codebase Findings
- `performCleanup` (`internal/core/charon.go:121`) returns early at `~:151-154` when `len(trackedWorkers) == 0`; the orphan-recovery call sits at `~:258`, after that return — verified at HEAD. This is why dogfood run D stalled for 65 minutes on 2026-07-27 with three in_progress beads and no live workers, and why they had to be reopened by hand (`br update --status open`).
- `recoverOrphanInProgressBeads` (`:400`): `grace` is read at `:409` and referenced only inside the `fmt.Sprintf` at `:442` — there is no timestamp comparison anywhere in the function, so the documented 5-minute grace (`:57`) is inert. Verified at HEAD.
- The recovery closes via `CloseBead` at `:446`, whose comment notes it "retries with --force when the bead has open children" — i.e. it deliberately bypasses the guard that would otherwise protect incomplete work.
- `UpdateBeadStatus(projectPath, beadID, types.StatusOpen)` is on the `BeadsClient` interface (`internal/core/overseer.go:64`) and is exactly what `rejectClose` uses to reopen a bead after a failed verification (`internal/core/bead_closer.go:411`) — the fix reuses the proven path rather than adding plumbing.
- `types.Bead` carries `UpdatedAt time.Time` (`pkg/types`), the timestamp the grace check needs.
- Observer signature table: `DefaultSignatures()` in `tools/hydra-observer/signatures.go`, `{Key, Marker, Severity, Threshold}` with substring matching in `ScanLog` — shipped by dogfood run C.

### Historical Patterns
- The quota incident (2026-07-27) produced the `usage limit` marker now worth watching; its misclassification was fixed in the same session (`isQuotaError`, PR #73), but the Observer had no signature for it.
- Two separate incidents in this project were caused by daemon restarts orphaning in-flight beads, both requiring manual `br update`. That is the operator-error class this FR removes.

---
*Adversarial review: 1 round, 4 lenses, approved 2026-07-27. Findings folded in: the test-adversary lens noted that an implementation which merely moved the call would pass a naive "recovered == 1" AC while still force-closing, so an explicit "ZERO CloseBead calls" AC was added with a recording fake; the implementability lens forced the zero-`UpdatedAt` behaviour to be specified rather than left to inference (chosen: skip, fail-closed); the scope adversary confined all three defects to one FR because they share `charon.go`, and fenced the stuck-process killer and retry policy in `Won't Have`; the grounding auditor verified every cited line and the `UpdateBeadStatus` interface method against HEAD.*
