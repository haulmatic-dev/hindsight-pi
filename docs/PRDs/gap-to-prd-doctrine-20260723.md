# PRD: gap-to-prd Compiler-Contract Compliance

## 1. Overview

**Problem:** `tools/gap-to-prd` generates mini-PRDs that bypass the create-prd compiler contract: it emits `**Target Files:**` as a bullet list that task-breakdown's parser cannot read (so gap beads silently lose their target files and hit the targetless-bead scope gate), omits `**Type:**`/`**Priority:**`/`**Test Command:**`/`**Build Command:**`, and numbers FRs from a Go map iteration so ordering — and the US `Aggregates:` references — are nondeterministic.
**Solution:** Make the generated mini-PRD parser-exact: inline Target Files, full compiler-contract fields, deterministic FR ordering with correct Aggregates references, and a human-triage section for targetless gaps, verified by a golden-file test against the parser's actual regexes.
**Success Metrics:**
- Every FR in a generated mini-PRD round-trips through `pi-config/skills/task-breakdown/analyzer.mjs` field regexes (target files, priority, test/build command all populated).
- Two runs over the same gap report produce byte-identical output (minus the timestamp line).
- Gaps without target files no longer become beads that the scope gate rejects.

---

## 2. User Stories

### US-001: Gap beads carry the full compiler contract
**As a** Hydra compliance cycle
**I want** gap mini-PRDs to be as doctrine-compliant as human-authored PRDs
**So that** gap beads have target files, priorities, and verify commands instead of silently degrading into targetless work.

**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Doctrine-Compliant Mini-PRD Template

**Acceptance Criteria:**
- `go test ./...` in `tools/gap-to-prd` exits 0
- Running the built binary over the golden fixture `testdata/gap-report.json` produces output where every `### FR-` block contains a same-line `**Target Files:**` value, a `**Priority:**` value, a `**Test Command:**` value, and a `**Build Command:**` value
- No mocking of internal dependencies in the integration test

---

## 3. Functional Requirements

### FR-001: Doctrine-Compliant Mini-PRD Template
**Type:** fr
**Priority:** P1

**Target Files:** `tools/gap-to-prd/main.go`, `tools/gap-to-prd/main_test.go`, `tools/gap-to-prd/testdata/gap-report.json`, `tools/gap-to-prd/testdata/golden-prd.md`

Rework `generateMiniPRD` in `tools/gap-to-prd/main.go` (behavior-preserving otherwise — flags, exit codes, evidence-based ACs, and the compliance-gate section stay):

1. **Inline Target Files.** Replace the bullet-list emission (current `main.go:113-117`) with a single line `**Target Files:** ` + backtick-quoted comma-separated paths (`` **Target Files:** `a.go`, `b.go` ``). Rationale: `analyzer.mjs:121-139` only parses same-line content — the regex is `` /^\*{0,2}target files:?\*{0,2}\s+(.+)/i `` — so the current bullet list silently yields zero files.
2. **Compiler-contract fields.** Each FR block gains, directly under the heading: `**Type:** fr`, `**Priority:** P0` when the gap severity is `critical` else `**Priority:** P1`, and after the description: `**Test Command:** \`go test ./...\`` and `**Build Command:** \`go build ./...\`` (same-line values — parser regexes at `analyzer.mjs:161-184`).
3. **Deterministic ordering.** Sort the category keys of `groupGapsByCategory`'s map before iterating (and sort gaps within a category by `ID`), so FR numbering is stable across runs. Fix the US-001 `Aggregates:` list to reference the FR numbers actually assigned to route/ac gaps (track a `map[gapID]frNum` during emission) instead of the current positional `i+1` (`main.go:149-151`), which is wrong whenever route/ac gaps are not the first FRs emitted.
4. **Targetless-gap triage section.** Gaps with empty `target_files` do not become FRs (the scope gate rejects targetless beads anyway). Instead they are listed under a `## NEEDS HUMAN TRIAGE: Gaps Without Target Files` section at the end, one bullet per gap with its ID, category, and description. When all gaps are targetless, the tool still writes the PRD (triage section only, no FRs) and exits 0.
5. **Simplify the struct plumbing.** Lift the anonymous gap struct into a named `type Gap struct {...}` so `groupGapsByCategory`/`filterGapsByCategory` lose their unreadable repeated literals (no schema change — JSON tags identical).

**Acceptance Criteria:**
- For the fixture gap `{"id":"G1","category":"behavior","severity":"critical","target_files":["internal/core/foo.go"]}` the output contains the exact literal lines `**Type:** fr`, `**Priority:** P0`, `` **Target Files:** `internal/core/foo.go` ``, `` **Test Command:** `go test ./...` ``, and `` **Build Command:** `go build ./...` ``
- A `severity: "warning"` fixture gap produces `**Priority:** P1`
- The emitted `**Target Files:**` line for a two-file gap is `` **Target Files:** `a.go`, `b.go` `` (single line; test asserts it matches the JS parser regex `^\*{0,2}target files:?\*{0,2}\s+(.+)` translated to Go: `(?i)^\*{0,2}target files:?\*{0,2}\s+(.+)`)
- Generating twice from `testdata/gap-report.json` yields identical output after stripping the `**Generated:**` line, and matches `testdata/golden-prd.md` (golden updated by a `-update` test flag)
- With a fixture containing a route-category gap emitted as FR-002 (not FR-001), the US `Aggregates:` section references `FR-002`
- A gap with `"target_files": []` appears under the exact literal heading `## NEEDS HUMAN TRIAGE: Gaps Without Target Files` and no FR block is emitted for it
- Existing behavior preserved: critical gaps keep the `REQUIRED: change the Go file` AC line; the `## COMPLIANCE GATE` section is still emitted
- `go build ./...` exits 0
- `go test ./...` exits 0

**Depends On:** —

---

## 4. Non-Functional Requirements

### NFR-SYS: System Integration Verification
**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- `go build ./...` in `tools/gap-to-prd` exits 0
- `go test ./...` in `tools/gap-to-prd` exits 0
- Root `go build ./...` exits 0
- No empty files or placeholder comments remain in `Target Files` of any closed bead

**Verifies:**
- FR-001: Doctrine-Compliant Mini-PRD Template

---

## 5. Technical Considerations

### Architecture
- One FR: all changes land in one file plus its new test/fixtures — splitting would create same-file conflicts the doctrine forbids.
- The golden-file test is the contract with `analyzer.mjs`: the Go test embeds the parser's field regexes (translated), so a future template regression fails here rather than as silent targetless beads three stages downstream.

### Technology
- Go standard library only. Golden test with `-update` flag convention.

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `tools/gap-to-prd/main.go`, `main_test.go`, `testdata/*` | fr | — | — |
| US-001 | — | us-integration | FR-001 | — |
| NFR-SYS | — | nfr-verification | FR-001 | — |

### Edge Cases
- Gap with one target file: no trailing comma.
- Empty gap report (`passed: true`): unchanged existing behavior — message + exit 0, no file written.
- All gaps targetless: PRD written with triage section only; task-breakdown then creates no FR beads (acceptable — these need a human).
- Duplicate target files within one gap: deduplicate, preserve first-seen order.

### Constraints
- Output schema consumed by `node pi-config/skills/task-breakdown/cli.mjs <prd>` — the parser regexes in `analyzer.mjs` are the source of truth; do not "improve" the field syntax beyond what it parses.
- Exit codes unchanged (0 converted/zero-gaps, 1 input invalid, 2 config error).

---

## 6. MoSCoW

**Must Have:** Inline Target Files, Type/Priority/Test/Build fields, deterministic ordering, correct Aggregates.
**Should Have:** Triage section for targetless gaps; named Gap struct cleanup.
**Could Have:** Per-category Test Command overrides (e.g. scoped `go test ./internal/core/...`).
**Won't Have:** LLM involvement (this stays a deterministic template); changes to the analyzer or adjudicator schemas.

---

## 7. Research Insights

### Codebase Findings
- Parser field regexes: priority `analyzer.mjs:109-119`, target files `:121-139` (same-line only), reads-from `:141-159`, test/build command `:161-184`. Bullet lists under a bare `**Target Files:**` line are dropped on the floor.
- Nondeterminism source: `groupGapsByCategory` returns a `map[string][]…` iterated directly (`main.go:101`); Go randomizes map order per run.
- `Aggregates` bug: `main.go:149-151` emits `FR-%03d` from the route/ac slice index, valid only if those gaps happened to be emitted first.
- Scope-gate context: `docs/PRDs/scope-gate-targetless-beads-20260715.md` — beads without target files are rejected at scope-gate time, which is exactly what the bullet-list bug produces.

### Historical Patterns
- The evidence-forcing AC text (`REQUIRED: change the Go file…`) was added because agents closed gap beads as "verified intentional" without changes (comment at `main.go:120-124`); it must survive this rework.
