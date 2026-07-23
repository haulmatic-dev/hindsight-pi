# PRD: Security Scan Gate

## 1. Overview

**Problem:** Hydra writes and (soon) pushes code autonomously, but nothing checks its output for planted secrets, vulnerable dependencies, or other security defects — the compliance cycle verifies PRD faithfulness only.
**Solution:** A deterministic `tools/security-scan` verifier that wraps gitleaks and govulncheck and emits analyzer-schema gaps, wired into `hydra-compliance-analyzer` as an always-on check so findings flow through the existing adjudicator → gap-to-prd → task-breakdown pipeline and block PRD approval until fixed.
**Success Metrics:**
- A repo containing a planted secret produces a `category: "security"` gap and the compliance cycle refuses approval.
- A clean repo adds one passing `security-scan` check and zero gaps — cycle behavior otherwise unchanged.
- A box without gitleaks/govulncheck installed is fail-open: the check passes with an explanatory detail, nothing crashes.

---

## 2. User Stories

### US-001: Autonomous output is security-gated
**As a** Hydra operator
**I want** every compliance cycle to run deterministic security scanners over the project
**So that** secrets and known-vulnerable dependencies are caught and turned into gap beads before a PRD is approved.

**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: security-scan Tool
- FR-002: Analyzer Wiring

**Acceptance Criteria:**
- `go test ./...` in `tools/security-scan` exits 0 and `go build ./tools/hydra-compliance-analyzer` exits 0
- With a stub `gitleaks` on PATH emitting one canned finding, `hydra-compliance-analyzer --prd <fixture> --project <fixture-dir> --output out.json` exits 1 and `out.json` contains a gap with `"category": "security"` and a check named `security-scan` with `passed: false`
- With no `security-scan` binary resolvable, the same analyzer invocation completes without error and `out.json` contains no `security` gaps
- No mocking of internal dependencies in the integration test

---

## 3. Functional Requirements

### FR-001: security-scan Tool
**Type:** fr
**Priority:** P1

**Target Files:** `tools/security-scan/main.go`, `tools/security-scan/main_test.go`, `tools/security-scan/go.mod`

New standalone tool following the conventions of the sibling tools (`tools/gap-to-prd`, `tools/hydra-compliance-analyzer`): own `go.mod` (module `github.com/haulmatic-dev/hydra/tools/security-scan`, standard library only), CLI `security-scan --project <dir> --output <json-path>`. The tool runs two scanners and maps their findings into the analyzer's `Gap` schema (the exact struct in `tools/hydra-compliance-analyzer/main.go`: `id`, `category`, `severity`, `description`, `target_files`, `suggested_bead`):

1. **gitleaks** — invoke `gitleaks detect --source <project> --no-banner --report-format json --report-path <tmpfile> --no-git` (fall back to a `--no-git`-less retry if the flag is unsupported; treat exit code 1 as "leaks found", not an error). Each leak maps to a gap: `category: "security"`, `severity: "critical"`, description containing the rule ID and redacted match (never include the full secret value — truncate the `Secret` field to its first 4 characters + `…`), `target_files: [<File>]`, `suggested_bead: "Remove secret from <File>:<StartLine> and rotate the credential"`.
2. **govulncheck** — only when `<project>/go.mod` exists: invoke `govulncheck -json ./...` with working directory `<project>`, parse the JSON stream for `finding` messages that have a non-empty trace (i.e. reachable vulns). Each distinct OSV ID maps to one gap: `category: "security"`, `severity: "warning"`, description containing the OSV ID and module path, `target_files: ["go.mod"]`, `suggested_bead: "Upgrade <module> to a fixed version for <OSV-ID>"`.

**Missing-scanner fail-open:** when a scanner binary is not on PATH (`exec.LookPath` fails), skip it and record `{"name": "<scanner>", "available": false}` in the output's `scanners` array; this is not an error. Output JSON shape: `{"passed": bool, "scanners": [{"name": string, "available": bool}], "gaps": [Gap...]}` where `passed` is `len(gaps) == 0`. Exit codes mirror the analyzer: 0 = no gaps, 1 = gaps found, 2 = configuration error (missing `--project`, unwritable `--output`). Progress/log lines go to stderr only — stdout carries nothing when `--output` is set (the analyzer's `runTool` captures stdout and must stay parseable).

**Acceptance Criteria:**
- `security-scan` with no `--project` exits 2 and stderr matches `/--project/`
- With stub `gitleaks` and `govulncheck` scripts on PATH (test creates a temp dir of fake executables and prepends it to PATH) where the gitleaks stub writes a report containing one finding `{"RuleID":"aws-access-key","File":"cfg/prod.env","StartLine":3,"Secret":"AKIAABCDEFGHIJKLMNOP"}` and exits 1, the output JSON has `passed: false` and exactly one gap with `category` equal to `"security"`, `severity` equal to `"critical"`, `target_files` equal to `["cfg/prod.env"]`
- The gap description for that finding does not contain the substring `AKIAABCDEFGHIJKLMNOP` (secret redaction)
- With both stub scanners emitting zero findings and exiting 0, the tool exits 0 and output JSON has `passed: true`, `gaps: []`
- With an empty PATH (no scanners resolvable), the tool exits 0, output JSON has `passed: true` and `scanners` contains entries with `available: false` for both `gitleaks` and `govulncheck`
- With a govulncheck stub emitting a finding for OSV `GO-2024-0001` in module `example.com/dep` (and a `go.mod` present in the fixture project), the output contains one gap with `severity` equal to `"warning"` and `target_files` equal to `["go.mod"]`
- `go build ./...` (in `tools/security-scan`) exits 0
- `go test ./...` (in `tools/security-scan`) exits 0

**Depends On:** —

---

### FR-002: Analyzer Wiring
**Type:** fr
**Priority:** P1

**Target Files:** `tools/hydra-compliance-analyzer/main.go`

**Reads From:**
- FR-001: security-scan Tool

In `tools/hydra-compliance-analyzer/main.go`: (1) add `func runSecurityScan(cfg Config) []Gap` modeled on the existing `runVerifySourceVsGo`: call the existing `runTool("security-scan", []string{"--project", cfg.ProjectDir}, cfg)` helper (no `--output` — read stdout; the tool must print the JSON report to stdout when `--output` is omitted, which FR-001's flag handling already provides), tolerate exit code 1 (gaps found) exactly as the other verifiers do, parse the `{"passed", "scanners", "gaps"}` JSON and return the `gaps` slice. (2) When `runTool` returns its "tool not found" error, return `nil` (fail-open — a dogfood box without the tool must not fail the cycle) and log the reason to stderr. (3) When output parsing fails, return a single gap with `category: "security"` and `severity: "warning"` describing the parse failure (mirrors the other verifiers' parse-failure behavior). (4) In `main()`, add `runCheck(report, "security-scan", runSecurityScan(cfg))` immediately after the existing `runCheck(report, "verify-source-vs-go", ...)` line — always-on, NOT gated behind `--enable-experimental` (deterministic scanners are low-noise, unlike the experimental verifiers).

**Acceptance Criteria:**
- `runSecurityScan` on a project where the tool resolves and reports one gap returns a slice of length 1 with `Category` equal to `"security"`
- `runSecurityScan` when no `security-scan` binary resolves (empty `HYDRA_TOOLS_DIR`, no project-vendored binary, no system binary) returns `nil` and the analyzer run exits 0 on an otherwise gap-free project
- `runSecurityScan` on unparseable tool stdout returns exactly one gap whose description matches `/Failed to parse security-scan output/` with the exact literal string `Failed to parse security-scan output`
- After the change, `hydra-compliance-analyzer` run without `--enable-experimental` executes the `security-scan` check (the report's `checks` array contains an entry named `security-scan`)
- `go build ./tools/hydra-compliance-analyzer` exits 0
- `go test ./...` run from `tools/hydra-compliance-analyzer` exits 0

**Depends On:** —

---

## 4. Non-Functional Requirements

### NFR-SYS: System Integration Verification
**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- `go build ./...` at the repo root exits 0
- `go build ./...` in `tools/security-scan` exits 0 and `go test ./...` in `tools/security-scan` exits 0
- `go build .` in `tools/hydra-compliance-analyzer` exits 0
- No empty files or placeholder comments remain in `Target Files` of any closed bead

**Verifies:**
- FR-001: security-scan Tool
- FR-002: Analyzer Wiring

---

## 5. Technical Considerations

### Architecture
- **Verifier, not a new gate.** The analyzer already aggregates deterministic verifiers via `runCheck` and the compliance cycle already withholds approval while gaps exist (`internal/core/compliance_cycle.go:188-210`), adjudicates false positives (`internal/core/gap_adjudicator.go`), and decomposes survivors into beads. Adding a verifier inherits all of that; adding a separate gate step in `compliance_cycle.go` would duplicate it. Rejected alternative: a ship-gate step (`internal/core/ship_gate.go`) — that runs only at approval time, whereas a verifier runs every cycle and produces structured gaps instead of one opaque fix bead.
- **Deterministic-first.** Scanners catch what pattern-matching can (secrets, known CVEs); the companion `security-review` skill (shipped separately in `.agents/skills/security-review/`) reasons about what they can't (authz logic, injection, unsafe exec).
- **Fail-open everywhere a binary may be missing.** Dogfood clones and developer laptops must not be blocked by ops state. Visibility is preserved via the `scanners[].available` field and the analyzer's stderr log.

### Technology
- Go standard library only (`os/exec`, `encoding/json`, `flag`). Scanner stubbing in tests via fake executables on a temp PATH — no network, no real scanner install required for CI.
- Real scanners are ops-installed on the VPS: `gitleaks` (binary release) and `govulncheck` (`go install golang.org/x/vuln/cmd/govulncheck@latest`).

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `tools/security-scan/main.go`, `main_test.go`, `go.mod` | fr | — | — |
| FR-002 | `tools/hydra-compliance-analyzer/main.go` | fr | — (Reads From FR-001) | — |
| US-001 | — | us-integration | FR-001, FR-002 | — |
| NFR-SYS | — | nfr-verification | FR-001, FR-002 | — |

### Edge Cases
- gitleaks exit 1 = findings (expected), exit ≥2 = scanner error → record `available: true` plus a stderr log, contribute zero gaps (fail-open on scanner malfunction, never fabricate gaps from a crash).
- Project without `go.mod`: govulncheck skipped entirely (not marked unavailable — marked `{"name":"govulncheck","available":true}` only when it actually ran; use a third state via omitting the entry when skipped-by-project-shape).
- Duplicate leaks of the same secret across files: one gap per (rule, file, line) — no dedupe beyond that; the adjudicator and `gapFingerprint` handle cross-cycle dedupe.
- Very large scanner reports: cap gaps at 50 per scanner, append a final gap noting the truncation count (`description` matching `/truncated/`).

### Constraints
- stdout of `security-scan` must be exactly the JSON report (or empty when `--output` is used) — the analyzer's `runTool` captures stdout for parsing.
- Never print secret values anywhere (stdout, stderr, gap descriptions).
- No behavior change to the five experimental verifiers or their flag.

---

## 6. MoSCoW

**Must Have:** FR-001, FR-002, secret redaction, fail-open on missing binaries.
**Should Have:** govulncheck reachability filtering (only findings with traces), truncation cap.
**Could Have:** semgrep as a third scanner behind a `--enable-semgrep` flag — deferred, heavier install.
**Won't Have:** LLM-based review (that is the `security-review` skill, separate deliverable); scanning of worktree diffs mid-run (cycle-level scanning is sufficient for the MVP); auto-rotation of found credentials.

---

## 7. Research Insights

### Codebase Findings
- `runTool` (`tools/hydra-compliance-analyzer/main.go:170`) resolves binaries project-vendored → `$HYDRA_TOOLS_DIR` → `/opt/dev-tools/components/hydra/tools/…` and go-builds only the project-vendored copy; stdout is captured for JSON parsing, stderr passes through.
- `runCheck` (`main.go:146`) appends a `Check` and folds gaps into the report; `Passed = len(gaps)==0` drives the analyzer's exit code and the cycle's approval decision.
- The adjudicator fingerprints gaps by category+description+prd_ref+target_files (`internal/core/gap_adjudicator.go:48-57`), so stable descriptions matter: keep them deterministic (no timestamps).
- Sibling tools use exit codes 0/1/2 with 1 = findings (`tools/gap-to-prd/main.go`, `tools/hydra-compliance-analyzer/main.go`).

### Historical Patterns
- The experimental verifiers were demoted behind a flag for false-positive noise (`main.go:73-84`); gitleaks/govulncheck are precise enough for always-on, and the adjudicator is the second line of defense.
