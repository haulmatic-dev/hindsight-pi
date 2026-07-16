# PRD: Scope Gate for Target-less Implementation Beads

## 1. Overview

**Problem:** The Target-Files scope gate only checks beads that declared Target Files (`if len(targets) > 0`). Implementation beads that declared none slip through unchecked — in a live run all residual sprawl (dozens of unrelated files modified) came from exactly these target-less beads.
**Solution:** When an implementation-type bead closes having declared no Target Files, surface the off-scope risk: log a visibility warning listing the code files it changed. Non-implementation beads (gates, epics, discovery, integration/verification) legitimately have no targets and stay exempt.
**Success Metrics:**
- A target-less implementation bead (e.g. type `fr`/`fix`/`implement`) that changed code files produces a `[ScopeGate]` warning naming those files.
- A target-less non-implementation bead (e.g. `epic`, `approval`, `us-integration`, `nfr-verification`) produces no such warning.
- Beads that declared Target Files keep their existing scope-check behavior unchanged.

---

## 2. User Stories

### US-001: Target-less implementation sprawl is visible
**As a** Hydra operator
**I want** a warning when an implementation bead changes files without having declared any Target Files
**So that** worker sprawl through the "no declared targets" loophole is no longer silent.

**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Implementation-Bead Classifier
- FR-002: Target-less Warning in Close Gate

**Acceptance Criteria:**
- `go test ./internal/core/...` exits 0
- A `fr`-type bead with no Target Files that changed a `.go` file yields a `[ScopeGate]` log naming that file
- An `epic`-type bead with no Target Files that changed a file yields no scope warning
- No mocking of internal dependencies in the integration test

---

## 3. Functional Requirements

### FR-001: Implementation-Bead Classifier
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/scope_gate.go`

Add a package-level function `func beadTypeShouldDeclareTargets(beadType string) bool` to `internal/core/scope_gate.go`. It returns true for bead types that write code and are therefore expected to declare Target Files, and false for structural/aggregate types that legitimately touch many or no files. Compare case-insensitively on the trimmed type. Return true for: `fr`, `fix`, `implement`, `green`, `refactor`, `task`, `us-heavy`. Return false for everything else (explicitly including `epic`, `approval`, `discovery`, `us-light`, `us-integration`, `nfr-config`, `nfr-verification`, and the empty string).

**Acceptance Criteria:**
- `beadTypeShouldDeclareTargets("fr")` returns `true`
- `beadTypeShouldDeclareTargets("implement")` returns `true`
- `beadTypeShouldDeclareTargets("REFACTOR")` returns `true` (case-insensitive)
- `beadTypeShouldDeclareTargets("epic")` returns `false`
- `beadTypeShouldDeclareTargets("us-integration")` returns `false`
- `beadTypeShouldDeclareTargets("nfr-verification")` returns `false`
- `beadTypeShouldDeclareTargets("")` returns `false`
- `go build ./...` exits 0
- `go vet ./internal/core/...` exits 0

**Depends On:** —

---

### FR-002: Target-less Warning in Close Gate
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/bead_closer.go`

In `(*BeadCloser).verifyBeforeClose` (`internal/core/bead_closer.go`), extend the existing scope-gate block. The block currently runs only `if len(targets) > 0`. Add an `else` branch (still inside `if mode := c.config.Validation.TargetScopeMode(); mode != "off"`): when `targets` is empty AND `beadTypeShouldDeclareTargets(string(bead.Type))` is true, call `changedFiles(repoPath)`; filter the result to code files that are not in the standard allowlist (exclude `go.mod`, `go.sum`, files under `docs/`, and `CHANGELOG.md` — reuse the existing allowlist notion by calling `scopeViolations(changed, changed)` is NOT valid, so instead filter with a small inline predicate: keep a path only if it does not have base `go.mod`/`go.sum`, is not `CHANGELOG.md`, and is not under `docs/`); if any remain, log `[ScopeGate] bead <id> (type <type>) declared no Target Files but changed <n> file(s): <paths>`. This never rejects the close (scope cannot be enforced without declared targets) — it only warns, in both `warn` and `block` modes. A `changedFiles` error fails open (log, continue), matching the existing gate.

**Acceptance Criteria:**
- In a repo where a target-less `fr` bead changed `internal/core/x.go`, `verifyBeforeClose` emits a log line matching `/declared no Target Files/` containing `internal/core/x.go`, and returns nil (close not rejected)
- In the same scenario with a target-less `epic` bead, no `declared no Target Files` line is emitted
- A target-less implementation bead that changed only `CHANGELOG.md` and a file under `docs/` emits no warning (allowlisted paths filtered out)
- A bead that declared Target Files is unaffected (existing behavior)
- `go test ./internal/core/...` exits 0
- `go build ./...` exits 0

**Depends On:**
- FR-001: Implementation-Bead Classifier

---

## 4. Non-Functional Requirements

### NFR-SYS: System Integration Verification
**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- Full project builds with `go build ./...` and exits 0
- `go test ./internal/core/...` exits 0
- No empty files or placeholder comments remain in `Target Files` of any closed bead
- `internal/core/scope_gate.go` and `internal/core/bead_closer.go` both compile as part of the package

**Verifies:**
- FR-001: Implementation-Bead Classifier
- FR-002: Target-less Warning in Close Gate

---

## 5. Technical Considerations

### Architecture
- Warn-only by design: a bead that declared no targets gives nothing to enforce against, so the gate cannot reject without guessing. Surfacing the change set is the honest maximum — it makes the loophole auditable and feeds a future "require targets for implementation beads" policy.
- Type-based exemption keeps legitimately-broad beads (integration/verification/epics) quiet while catching the beads that should have been scoped.

### Technology
- Go standard library only. `changedFiles`, `scopeViolations`, and the allowlist already live in `internal/core/scope_gate.go`. Tests use real temp git repos, as in `scope_gate_test.go`.

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `internal/core/scope_gate.go` | fr | — | — |
| FR-002 | `internal/core/bead_closer.go` | fr | FR-001 | — |
| US-001 | — | us-integration | FR-001, FR-002 | — |
| NFR-SYS | — | nfr-verification | FR-001, FR-002 | — |

### Edge Cases
- Empty change set (nothing modified): no warning.
- Only allowlisted paths changed (docs/CHANGELOG/go.mod): no warning — those are always in-scope.
- Non-implementation target-less bead: skipped by the classifier.
- `changedFiles` error / non-git repo: fail open.

### Constraints
- Must never reject a close (warn-only).
- Must not change behavior for beads that declared Target Files, nor for `mode == "off"`.

---

## 6. MoSCoW

**Must Have:** FR-001 classifier, FR-002 warn-on-target-less-implementation.
**Should Have:** Allowlist reuse (docs/CHANGELOG/go.mod excluded from the warning).
**Could Have:** A config flag to escalate the warning to a block for implementation beads.
**Won't Have:** Inferring targets from the diff; changing non-implementation bead handling.

---

## 7. Research Insights

### Codebase Findings
- `verifyBeforeClose` scope block: `internal/core/bead_closer.go` ~line 296-314. The `if len(targets) > 0` guard is exactly the loophole; FR-002 adds the `else`.
- `changedFiles`, `scopeViolations`, allowlist (`go.mod`/`go.sum`/`CHANGELOG.md`/`docs/`) all in `internal/core/scope_gate.go`.
- `beadTargetFiles(bead)` extracts declared targets; unchanged here.
- Bead type is `bead.Type` (a `types.BeadType`/string); task-breakdown emits `fr`, `fix`, `green`, `refactor`, `implement`, `task`, `epic`, `approval`, plus `us-*` / `nfr-*` from the PRD compiler.

### Historical Patterns
- Live run 2026-07-14 (dogfood-3): the scope gate worked for beads with targets, but all residual sprawl (skill-doc trees, unrelated files) came from beads that declared no Target Files and were skipped by the `len(targets) > 0` guard. This PRD closes that gap.
