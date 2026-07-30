# PRD: State-Based Gate Truth (Shepherd A3)

## 1. Overview

**Problem:** Run D3 (2026-07-29) closed all three gates — COMPLIANCE, LINT, NFR — as clean on a run whose working branch contained **zero integrated code** (all work sat uncommitted in chain worktrees at timeout). The programmatic gate check exists precisely to prevent this: `internal/validation/compliance_gate.go` validates upstream target files and failed closures so "operators don't need to trust the agent's self-report." But it is wired only into `Overseer.CloseBead` → `ValidationService.CloseBead` → `CloseBeadWithValidation`, a path used by the API. The path that closes every gate bead in a real run — worker completes → `BeadCloser.Close` — calls `beadsClient.CloseBead` directly (bead_closer.go:192). The gate validator is dead code on the only path that matters, and gates close on the worker's self-report alone.

Compounding it: chains stranded mid-flight at daemon stop/timeout leave their worktrees and branches behind forever (run-H worktrees were still present during run-D3 forensics). Committed work in a stranded chain is neither integrated nor quarantined — it is invisible.

**Solution:** Route gate-bead closes through the existing validator on the worker-completion path, and reconcile stranded terminal chains at daemon startup using the A1 machinery (integrate clean, quarantine failed).

**Success Metrics:**
- A compliance-gate bead whose upstream dependency target files are absent from the working tree cannot close as completed; it closes failed with the gap list in the fix bead.
- Replaying the run-D3 end state (work in worktrees, nothing integrated) yields a failed compliance gate, not a clean one.
- After a daemon restart, a chain whose beads are all terminal and whose worktree still exists is integrated (clean) or quarantined (failed) within one scheduler cycle, with an event emitted.

---

## 0. Scope Profile
- **System Type:** library (internal Go, daemon-side)
- **Interfaces:** internal Go API only
- **Surfaces Explicitly Excluded:** the compliance *cycle* (analyzer → adjudicator → gap-to-prd — unchanged), LINT GATE and NFR gate semantics (their validators do not exist yet; separate work), task-breakdown gate bead creation, the worker binary, `internal/validation` checker internals
- **Deployment Model:** single-binary daemon

---

## 2. User Stories

### US-001: Gates cannot close on self-report — Integration Bead
**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Gate Validation On The Worker Close Path
- FR-002: Stranded Chain Reconciliation At Startup

**Acceptance Criteria:**
- `go build ./...` exits 0
- `go test ./internal/core/... ./internal/validation/...` exits 0
- An integration test drives a compliance-gate bead close through `BeadCloser.Close` with an upstream dependency whose target file is missing, and the bead closes `failed` with a fix bead carrying the gap list
- The same close with all upstream target files present and stub-free closes `completed`
- No mocking of internal dependencies: the test uses the real validator against a real temp project tree

---

## 3. Functional Requirements

### FR-001: Gate Validation On The Worker Close Path
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/bead_closer.go`, `internal/core/gate_close_test.go`

In `BeadCloser.Close`, before the success path closes a bead that `validation.IsComplianceGate(bead)` reports as a gate (requires the bead lookup already performed at the top of `Close`):

1. Run the compliance-gate checks via the existing `internal/validation` surface (`Validator.Validate` already runs `runComplianceGateChecks` first for gate beads and returns critical `compliance` errors; construct the validator the same way `ValidationService.CloseBead` does). Gate validation runs only when `success && reason == "completed"` and `c.config.Validation.Enabled`.
2. On gaps: log each gap, flip the close to `failed` — reuse the existing failed-close path so the `closed-failed` label, cause block, fix bead, dedup, and `bead_closed` event all apply unchanged. The fix bead's description must contain the formatted gap list (pass the gaps as the cause when the worker-log cause is empty).
3. On validator *error* (not gaps): log and close as today — fail-open on infrastructure, fail-closed only on evidence.
4. Emit event `gate_validated` with `Fields: {gaps: <count>, verdict: "clean"|"failed"}` on every gate validation.

**Acceptance Criteria:**
- A gate bead with an upstream dependency whose `target_files` include a missing file closes `failed`, carries `closed-failed`, and its fix bead description contains the missing-file gap
- A gate bead with clean upstream closes `completed` exactly as today
- With `validation.enabled: false`, behaviour is byte-identical to today (no validator constructed)
- A validator infrastructure error (e.g. unreadable project dir) does not block the close and logs the error
- `gate_validated` events appear with correct verdicts in both outcomes
- Non-gate beads see zero behaviour change (asserted by an existing-suite pass)

### FR-002: Stranded Chain Reconciliation At Startup
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/chain_reconciler.go`, `internal/core/chain_reconciler_test.go`, `internal/core/overseer.go`

A daemon stop/timeout mid-run leaves chain worktrees under `$HYDRA_HOME/.hydra-worktrees/<project-slug>/<key>` whose beads may since be terminal. Their committed work is currently invisible forever.

1. Add `type ChainReconciler struct` with `func (r *ChainReconciler) Reconcile(projectPath string) (integrated, quarantined, skipped int)`. For each existing worktree of the project (enumerate via `git worktree list` against the project repo, filtering `hydra/wt-` branches):
   - Resolve the chain's bead IDs from the worktree key using the same chain registry the completion detector uses; when the registry has no entry (post-restart), fall back to treating the worktree's key as a bead ID prefix and look the beads up via the bead file reader.
   - **All beads terminal + any `closed-failed`** → `QuarantineAndRemove`, emit `chain_quarantined` with `Reason: "reconciled_after_restart"`.
   - **All beads terminal + none failed + worktree branch has commits ahead of the working branch** → `IntegrateAndRemove`, emit `chain_integrated` with `Reason: "reconciled_after_restart"`.
   - **Any bead non-terminal** → skip (the chain is still live).
   - **No commits ahead and all terminal** → remove the worktree and branch (nothing to preserve), count as skipped.
2. Call `Reconcile` once per project during overseer startup, after the worktree manager and bead file reader are initialized, before the first scheduling cycle. Log a one-line summary per project only when it did something.
3. Failure isolation: one chain's reconciliation error must not abort the others; errors log and count as skipped.

**Acceptance Criteria:**
- A fixture repo with a terminal-clean chain worktree (one commit ahead) reconciles to integrated: commit present on the working branch, worktree and `hydra/wt-` branch gone, `chain_integrated` event emitted
- The same fixture with one chain bead labelled `closed-failed` reconciles to quarantined: `hydra/quarantine/<key>` exists, working branch untouched
- A chain with a non-terminal bead is skipped and its worktree remains
- A terminal chain with zero commits ahead is cleaned up without an integrate or quarantine event
- An error on the first chain does not prevent reconciliation of the second (asserted with a deliberately corrupted first worktree)
- `go vet ./internal/core/...` exits 0

---

## 4. Non-Functional Requirements

### NFR-001: Gate truth is evidence-based and fail-open on infrastructure
**Type:** nfr-verification
**Priority:** P1

**Target Files:** `internal/core/bead_closer.go`

A gate must fail only on evidence (gaps found), never on infrastructure trouble (validator error, missing config), and reconciliation must never delete work that is not provably integrated or quarantined.

**Acceptance Criteria:**
- `go build ./...` exits 0
- `go test ./internal/core/... -run 'Gate|Reconcil' -count=1` exits 0
- A test asserts: validator returning an error (not gaps) → bead closes completed; validator returning gaps → bead closes failed
- A test asserts the reconciler never runs `git worktree remove` on a chain with a non-terminal bead

**Verifies:**
- FR-001: Gate Validation On The Worker Close Path
- FR-002: Stranded Chain Reconciliation At Startup

---

## 5. Out of Scope
- LINT GATE / NFR gate programmatic validators (no checker exists for them yet; follow-up once compliance-gate wiring proves out)
- Changing the compliance cycle, analyzer, or adjudicator
- Continuous (non-startup) reconciliation — the D1 shepherd cron will own periodic sweeps
- Any change to how gates are seeded by task-breakdown

---

## 6. Adversarial Review

**Reviewed by:** Claude Fable 5 (independent pass after drafting)

- **"Wiring the validator into the hot close path adds latency to every close."** It runs only for gate-labelled beads (a handful per run), only on the success path, and the checks are local file reads — no model, no network. Non-gate closes are untouched.
- **"Flipping a 'completed' close to 'failed' could loop: fix bead → gate re-close → fail again."** Bounded by the existing machinery this PRD deliberately reuses: fix-cap 1 refuses fix-of-fix, dedup refuses a second fix for the same gate, and the gap list in the fix bead gives the retry the exact evidence. That is churn-bounded self-healing, which is the design goal.
- **"Startup reconciliation could integrate work the operator meant to abandon."** It integrates only chains whose every bead is terminal with zero failures — the exact condition under which the live daemon would have integrated it moments later. Abandoned-by-intent work has failed or open beads and routes to quarantine or skip.
- **"The registry is empty after restart; key-prefix fallback could mis-map beads."** Worktree keys are chain-root bead IDs by construction (`hydra/wt-<key>`), and the fallback only reads beads that exist with that prefix; a miss means skip, never a destructive action (NFR-001 pins this).
- **"Why not also gate LINT/NFR beads?"** No programmatic checker exists for them; wiring self-report beads to a nonexistent validator would either no-op (false confidence) or hard-fail every run. Compliance-gate first, measure, then extend.
