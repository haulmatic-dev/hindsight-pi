# TASK handoff — task-xk1d

**Bead:** hydra-xk1d (FR-002: Beads mirror audit tool — GREEN closure)
**Date:** 2026-10-22
**Predecessor:** task-s8f0 (discovery, all 6 ambiguities resolved; frozen chain context)

## What changed

- `tools/beads-mirror-audit/main.go` — GREEN implementation complete: `runAudit` parses `<storeDir>/issues.jsonl` (JSON objects or `id status` token lines) against a single `br list --json` call (via `brRunner` seam, prod = `execBr`); divergence lines `<bead-id> jsonl=<status> db=<status>`; missing-from-db mirror beads = drift with empty `db=`; db-only beads skipped; `mirror clean (N beads)` + exit 0 on agreement; exit 1 on drift; exit 2 with `audit unavailable: <err>` on br absence/failure/store unreadable (fail-open, never mutates either store); `--json` emits `{"divergences":[...],"checked":N}`. `auditImplemented = true`.
- Tests: subprocess RED suite (`main_test.go`) with skip gate now passing + in-process seam suite (`main_inproc_test.go`, 8 tests, 71.3% statement coverage).
- `CHANGELOG.md` — GREEN, RED, discovery, and refactor entries present under `## [Unreleased] > Added`.
- Prior commit `c96a3190` renamed the stray in-process test file to a `_test.go` name (`main_test_inproc.go` -> `main_inproc_test.go`) so the repo-wide build passes.
- Read-only contract verified: `execBr` only issues `br list --json`; no `os.WriteFile`/`Create`/`Remove`/`Rename` calls in `main.go`; pinned by `TestAudit_Readonly` byte-comparing `issues.jsonl` pre/post. Pattern saved to CASS (`cm-ctx-8201`, score 0.94).

## Verification

- `go test ./tools/beads-mirror-audit/ -count=1 -cover` — PASS, 71.3% statement coverage.
- `go build ./tools/beads-mirror-audit/` — BUILD_OK.
- All 6 ACs satisfied: exit 0 clean / exit 1 divergence lines / exit 2 `audit unavailable` / `--json` shape / fake-br double tests / read-only guarantee.
- Stray build artifact `./beads-mirror-audit` (3.3MB) removed from worktree root.
- TODO/FIXME/Placeholder grep: 1 hit, confirmed legitimate doc comment (`brRunner` seam description).

HYDRA_VERDICT: {"done": true, "ac_met": ["exit0-clean", "exit1-divergences", "exit2-unavailable", "json-flag-shape", "fake-br-tests", "read-only"], "ac_unmet": [], "files_changed": ["tools/beads-mirror-audit/main.go", "tools/beads-mirror-audit/main_test.go", "tools/beads-mirror-audit/main_inproc_test.go", "CHANGELOG.md", "docs/hydra/handoff/task-xk1d.md"], "verification": ["go test ./tools/beads-mirror-audit/ -count=1 -cover => PASS, 71.3% statements", "go build ./tools/beads-mirror-audit/ => BUILD_OK"]}

## Open edges

- Pan health-digest wiring for the audit tool is out of scope (per s8f0).
- `internal/core/resource_monitor.go` and `internal/core/pan_stale_filing.go` carry unrelated staged work from sibling strands — not committed here; do not fold into this bead's diff.
- Compiled binary artifacts in cwd are gitignored/untracked; removed before commit.
