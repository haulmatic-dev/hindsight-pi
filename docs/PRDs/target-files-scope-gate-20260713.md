# PRD: Target-Files Scope Gate

## 1. Overview

**Problem:** Nothing enforces a bead's declared Target Files — in a live run, workers on a 2-file PRD also modified 40+ unrelated files (-1753 lines) and created spurious directory trees, which no gate rejected.
**Solution:** At verified-close time, diff the worker's tree and reject (or warn on) closes whose changed files fall outside the bead's declared Target Files plus a small conventional allowlist.
**Success Metrics:**
- A close whose diff touches files outside Target Files ∪ allowlist is rejected with a reason listing the off-scope paths (in `block` mode) or logged (in `warn` mode).
- On-scope closes are unaffected.
- Beads with no extractable Target Files, and non-implementation bead types, are never blocked by this gate.

---

## 2. User Stories

### US-001: Off-scope worker changes cannot verified-close silently
**As a** Hydra operator
**I want** closes rejected when a worker changed files outside the bead's declared targets
**So that** workers cannot sprawl beyond their contract without it being caught at the gate.

**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Scope Violation Detector
- FR-002: Scope Gate Config
- FR-003: Close-Path Wiring

**Acceptance Criteria:**
- `go test ./internal/core/...` exits 0
- With config mode `block`, a `BeadCloser.verifyBeforeClose` call for a bead whose repo contains a modified file NOT in its Target Files returns a non-nil rejection whose message contains the off-scope file path
- With config mode `warn`, the same scenario does not reject the close
- No mocking of internal dependencies in the integration test

---

## 3. Functional Requirements

### FR-001: Scope Violation Detector
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/scope_gate.go`

Create the pure detection logic in a new file `internal/core/scope_gate.go` with two package-level functions. `changedFiles(repoPath string) ([]string, error)` returns the repo-relative paths of all uncommitted changes: it runs `git status --porcelain` in `repoPath` and parses each line's path (including untracked files; for renames `R old -> new` it takes the new path). `scopeViolations(changed []string, targets []string) []string` returns, in input order, every path in `changed` that is NOT allowed. A path is allowed when ANY of: (a) it equals a target path (after stripping any `:symbol` suffix a target may carry, e.g. `internal/core/foo.go:Bar()` → `internal/core/foo.go`); (b) it is a `_test.go` file in the same directory as any target; (c) its base name is `go.mod` or `go.sum`. Comparison is by cleaned repo-relative path (`filepath.Clean`).

**Acceptance Criteria:**
- `scopeViolations([]string{"internal/core/a.go"}, []string{"internal/core/a.go"})` returns an empty slice
- `scopeViolations([]string{"internal/core/a_test.go"}, []string{"internal/core/a.go"})` returns an empty slice (same-dir test file allowed)
- `scopeViolations([]string{"go.mod", "go.sum"}, []string{"internal/core/a.go"})` returns an empty slice
- `scopeViolations([]string{"internal/other/b.go"}, []string{"internal/core/a.go"})` returns `["internal/other/b.go"]`
- `scopeViolations([]string{"internal/core/a.go"}, []string{"internal/core/a.go:DoThing()"})` returns an empty slice (symbol suffix stripped)
- `changedFiles` on a temp git repo containing one modified tracked file and one untracked file returns both paths, and returns an empty slice on a clean repo
- `go build ./...` exits 0
- `go vet ./internal/core/...` exits 0

**Depends On:** —

---

### FR-002: Scope Gate Config
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/config.go`

Add a config knob to the existing `ValidationConfig` struct in `internal/core/config.go`: field `EnforceTargetScope string` with yaml tag `enforce_target_scope`. Valid values: `"off"`, `"warn"`, `"block"`. In `DefaultConfig()` set it to `"warn"` (safe rollout: observe violations without blocking). Add a method `func (v ValidationConfig) TargetScopeMode() string` that returns the normalized lowercase value, mapping empty string to `"warn"` and any unrecognized value to `"warn"`.

**Acceptance Criteria:**
- `DefaultConfig().Validation.TargetScopeMode()` returns `"warn"`
- A `ValidationConfig{EnforceTargetScope: "BLOCK"}.TargetScopeMode()` returns `"block"`
- A `ValidationConfig{EnforceTargetScope: "bogus"}.TargetScopeMode()` returns `"warn"`
- A `ValidationConfig{EnforceTargetScope: "off"}.TargetScopeMode()` returns `"off"`
- A yaml config containing `validation:\n  enforce_target_scope: block` parses with `EnforceTargetScope == "block"`
- `go build ./...` exits 0

**Depends On:** —

---

### FR-003: Close-Path Wiring
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/bead_closer.go`

Wire the gate into `(*BeadCloser).verifyBeforeClose` in `internal/core/bead_closer.go`, immediately after the existing require-diff check. Behavior: if `TargetScopeMode()` is `"off"`, do nothing. Otherwise compute `targets := beadTargetFiles(bead)` (existing helper); if `targets` is empty, do nothing (beads without declared targets — gates, epics, discovery — are never scope-checked). Otherwise call `changedFiles(repoPath)` then `scopeViolations(changed, targets)`. On violations: in `"warn"` mode log a single line prefixed `[ScopeGate]` listing the bead ID and the off-scope paths, and continue; in `"block"` mode reject the close the same way the require-diff gate rejects (same error/return path), with a reason string that contains `off-scope changes` and the joined off-scope paths. A `changedFiles` error must fail open (log and continue), matching the diff-gate's fail-open convention.

**Acceptance Criteria:**
- In `block` mode, `verifyBeforeClose` for a bead with Target Files `["x/a.go"]` in a repo where `x/a.go` and `y/b.go` are both modified rejects the close with an error/reason matching `/off-scope changes/` and containing the literal string `y/b.go`
- In `block` mode, the same bead in a repo where only `x/a.go` (and `x/a_test.go`) changed closes successfully
- In `warn` mode, the off-scope scenario does NOT reject the close
- A bead whose `beadTargetFiles` returns an empty slice is never rejected by the scope gate in any mode
- When `changedFiles` returns an error, the close proceeds (fail-open) and a log line is emitted
- `go test ./internal/core/...` exits 0
- `go build ./...` exits 0

**Depends On:**
- FR-001: Scope Violation Detector
- FR-002: Scope Gate Config

---

## 4. Non-Functional Requirements

### NFR-SYS: System Integration Verification
**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- Full project builds with `go build ./...` and exits 0
- `go test ./internal/core/...` exits 0
- No empty files or placeholder comments remain in `Target Files` of any closed bead
- `internal/core/scope_gate.go`, `internal/core/config.go`, and `internal/core/bead_closer.go` all compile as part of the package

**Verifies:**
- FR-001: Scope Violation Detector
- FR-002: Scope Gate Config
- FR-003: Close-Path Wiring

---

## 5. Technical Considerations

### Architecture
- Detection (FR-001) is pure and file-system/git-only — no daemon state — so it is unit-testable in isolation and reusable by a future patch-contract gate.
- Enforcement lives in the existing verified-close gate (`verifyBeforeClose`), the single choke point through which successful closes already pass (require-diff precedent). No new close path.
- Default `warn` mode decouples rollout from enforcement: run it, read `[ScopeGate]` lines, flip to `block` in config when clean.

### Technology
- Go standard library only (`os/exec` for git, `path/filepath`, `strings`). Test with `go test`, temp git repos via `t.TempDir()` + `git init`.

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `internal/core/scope_gate.go` | fr | — | — |
| FR-002 | `internal/core/config.go` | fr | — | — |
| FR-003 | `internal/core/bead_closer.go` | fr | FR-001, FR-002 | — |
| US-001 | — | us-integration | FR-001..003 | — |
| NFR-SYS | — | nfr-verification | FR-001..003 | — |

### Edge Cases
- Renamed files (`git status` `R` lines): treat the NEW path as the changed file.
- Targets carrying symbol suffixes (`file.go:Func()`): compare on the path part only.
- Worktree vs shared checkout: `repoPath` is whatever tree the worker ran in; both are git work trees, `git status --porcelain` works in either.
- Non-git repoPath or git failure: fail open (never block a close on inability to check), same convention as the require-diff gate.

### Constraints
- Must not change the behavior of closes for beads without Target Files.
- Must not introduce any new external dependency.

---

## 6. MoSCoW

**Must Have:** FR-001 detection, FR-002 knob with warn default, FR-003 wiring with block/warn/off.
**Should Have:** Rename handling; symbol-suffix stripping.
**Could Have:** Per-bead-type mode overrides (e.g. always-warn for us-integration).
**Won't Have:** Patch extraction/application (future patch-contract work order); allowlist configurability.

---

## 7. Research Insights

### Codebase Findings
- `internal/core/bead_closer.go` already gates closes: `verifyBeforeClose` (~line 252) runs when `verifiedCloseEnabled()`, and the require-diff check (`config.Validation.RequireDiffOnClose`, ~line 266) calls `repoHasChanges(repoPath, beadTargetFiles(bead))` — the scope gate composes directly after it.
- `beadTargetFiles(bead)` (~line 339) already extracts declared target files from a bead; reuse it, do not re-parse.
- `internal/core/bead_analyzer.go` has `ExtractTargetFiles` — a second extractor; FR-003 must use `beadTargetFiles` (the close-path one) for consistency.
- Fail-open on git errors is the established convention in this gate (see require-diff comments).

### Historical Patterns
- Live run 2026-07-10: workers on PRD `compliance-sticky-approval` (2 target files) also changed 40+ unrelated files; nothing rejected it. This PRD is the direct countermeasure.
