# PRD: Compliance Sticky Approval

## 1. Overview

**Problem:** After the compliance cycle declares a PRD APPROVED, the poll loop re-enters `checkAndRunCycle` on every tick and re-runs the analyzer, re-approving until `max_cycles` is hit and a false "manual intervention required" is logged.
**Solution:** Record a sticky, restart-durable approved state the moment approval finalizes, and short-circuit `checkAndRunCycle` while it is set.
**Success Metrics:**
- Once a PRD is approved, `checkAndRunCycle` performs zero further analyzer runs.
- The approved state survives a daemon restart (no re-run after restart).
- `[ComplianceCycle] MAX CYCLES ... Manual intervention required.` is no longer logged for an already-approved PRD.

---

## 2. User Stories

### US-001: Approved PRD stops the compliance loop
**As a** Hydra operator
**I want** the compliance cycle to stop working once a PRD is approved
**So that** I do not get false "manual intervention required" alerts and wasted analyzer runs after a clean pass.

**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Sticky Approval State
- FR-002: Finalize Marks Approved

**Acceptance Criteria:**
- `go test ./internal/core/...` exits 0
- After `markApproved()` is called on a `ComplianceCycleManager`, a subsequent `checkAndRunCycle()` returns `nil` and leaves `cm.cycles` unchanged
- No mocking of internal dependencies in the integration test

---

## 3. Functional Requirements

### FR-001: Sticky Approval State
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/compliance_cycle.go`

Add a restart-durable "approved" state to `ComplianceCycleManager` and guard the cycle entrypoint with it. Add an `approved bool` field to the `ComplianceCycleManager` struct. Add three methods on `*ComplianceCycleManager`: `approvalSentinelPath() string` returns `filepath.Join(cm.config.ProjectDir, ".hydra", "compliance-approved")`; `isApproved() bool` returns `true` when the in-memory `approved` flag is set OR the sentinel file exists on disk; `markApproved()` sets `cm.approved = true`, creates the `.hydra` directory (`os.MkdirAll(..., 0755)`), and writes the sentinel file (`os.WriteFile(path, []byte(...), 0644)`). Add a guard as the FIRST statement of `checkAndRunCycle()` (before any `cm.overseer` dereference): `if cm.isApproved() { return nil }`.

**Acceptance Criteria:**
- On a `ComplianceCycleManager` whose `config.ProjectDir` is a fresh temp dir with no sentinel file, `isApproved()` returns `false`
- After calling `markApproved()`, `isApproved()` returns `true`
- After calling `markApproved()`, a file exists at `filepath.Join(cm.config.ProjectDir, ".hydra", "compliance-approved")`
- A newly constructed `ComplianceCycleManager` whose `config.ProjectDir` already contains a file at `.hydra/compliance-approved` returns `true` from `isApproved()` even though its in-memory `approved` field is false (restart durability)
- With `approved` set true, calling `checkAndRunCycle()` returns `nil` without incrementing `cm.cycles` (guard runs before any overseer access)
- `go build ./...` exits 0
- `go vet ./internal/core/...` exits 0

**Depends On:** —

---

### FR-002: Finalize Marks Approved
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/ship_gate.go`

Wire the approval finalizer to record the sticky state. In `finalizeApproval()`, immediately after the `cm.notifyApproved()` call on the ship-gate-pass path, add a call to `cm.markApproved()`. Do NOT mark approved on the ship-gate-fail path (the branch that logs `SHIP-GATE FAILED` and creates a fix bead must remain unchanged), so a failing product is never marked approved.

**Acceptance Criteria:**
- `finalizeApproval()` calls `cm.markApproved()` on the ship-gate-pass branch (the branch containing `cm.notifyApproved()`)
- The ship-gate-fail branch (containing the `SHIP-GATE FAILED` log and `cm.createShipGateBead(failures)`) does NOT call `cm.markApproved()`
- After a `finalizeApproval()` call on a manager whose ship-gate passes (or is disabled), `cm.isApproved()` returns `true`
- `go build ./...` exits 0
- `go test ./internal/core/...` exits 0

**Depends On:**
- FR-001: Sticky Approval State

---

## 4. Non-Functional Requirements

### NFR-SYS: System Integration Verification
**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- Full project builds with `go build ./...` and exits 0
- Full test suite for the touched package runs with `go test ./internal/core/...` and exits 0
- No empty files or placeholder comments remain in `Target Files` of any closed bead
- `internal/core/compliance_cycle.go` and `internal/core/ship_gate.go` both compile as part of the package

**Verifies:**
- FR-001: Sticky Approval State
- FR-002: Finalize Marks Approved

---

## 5. Technical Considerations

### Architecture
- Sticky flag + on-disk sentinel. The in-memory `approved` flag covers the running process; the sentinel file covers daemon restarts. `isApproved()` OR-combines both so either source is authoritative.
- Guard placed as the first statement of `checkAndRunCycle()` so an approved PRD short-circuits before any overseer/beads work — cheap and side-effect-free.

### Technology
- Go. Standard library only (`os`, `path/filepath` already imported in the package). Test with `go test`.

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `internal/core/compliance_cycle.go` | fr | — | — |
| FR-002 | `internal/core/ship_gate.go` | fr | FR-001 | — |
| US-001 | — | us-integration | FR-001, FR-002 | — |
| NFR-SYS | — | nfr-verification | FR-001, FR-002 | — |

### Edge Cases
- Sentinel file unreadable/dir uncreatable: `markApproved()` still sets the in-memory flag so the running process stops looping; restart durability is best-effort.
- Manual re-run: deleting `.hydra/compliance-approved` and restarting re-enables the cycle (documented operator escape hatch).

### Constraints
- FR-002 must not alter the ship-gate-fail path — a failing product must never be marked approved.

---

## 6. MoSCoW

**Must Have:** Sticky approved flag, sentinel-file durability, cycle guard, finalize wiring.
**Should Have:** Restart-durability test coverage.
**Could Have:** Operator log line when the guard short-circuits an already-approved cycle.
**Won't Have:** Slack/email notification (separate concern, TODO already noted in code).

---

## 7. Research Insights

### Codebase Findings
- `checkAndRunCycle()` (`internal/core/compliance_cycle.go`) is the poll entrypoint; it currently has no terminal state — approval paths call `finalizeApproval()` and return `nil`, so the next tick re-enters and re-runs the analyzer.
- `finalizeApproval()` lives in `internal/core/ship_gate.go`; it calls `cm.notifyApproved()` on ship-gate pass (or when the gate is disabled) and files a fix bead on failure.
- `ComplianceCycleManager` struct is defined in `internal/core/compliance_cycle.go` with fields `config, overseer, cycles, running, stopCh, prevSurviving`.
- `cm.config.ProjectDir` is the per-run project root; `.hydra/` is already used there (e.g. `gap-adjudications.json`).
