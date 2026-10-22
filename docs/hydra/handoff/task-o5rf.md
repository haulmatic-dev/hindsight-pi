# red handoff

## What changed (files + one line each)

- internal/mirroraudit/single_source_test.go: (pre-existing, committed at RED 63dd8a87) three enforce-gated source-level tests pinning the single-source-of-truth contract: `TestSingleSource_ToolImportsPackage` (CLI must import internal/mirroraudit), `TestSingleSource_ToolHasNoDuplicateClassifier` (no duplicate parseJSONLLine/parseDBList/readJSONLMirror/divergenceLine in the tool), `TestSingleSource_DAOMConsumerPinsPackage` (daemon sweep references mirroraudit.*). RED gate via `MIRROR_AUDIT_SOT_ENFORCE=1`.
- internal/mirroraudit/audit.go: gofmt-only fix — struct tag alignment drift in `Report` (StorePath/Checked/Counts tag column). No behavior change.
- No other files changed on this bead; GREEN (efa8f7ed) and the mirror-health healing fix (d4351286) already landed before this run, so RED-phase "tests fail initially" is recorded in prior handoffs and no longer reproducible on this HEAD.

## Verification (commands run + pass/fail)

- `go test ./internal/mirroraudit/ -count=1` — PASS (ok). Daemon-gate form (no enforcement), 0 failures.
- `MIRROR_AUDIT_SOT_ENFORCE=1 go test ./internal/mirroraudit/ -count=1 -v` — PASS; all three SOT contract tests (ToolImportsPackage, ToolHasNoDuplicateClassifier, DAOMConsumerPinsPackage) PASS, confirming the tool is a thin wrapper (3 `mirroraudit.` refs, zero duplicate classifiers) and pan_mirror_health.go references mirroraudit.*.
- `go test ./internal/mirroraudit/ -count=1 -cover` — PASS, coverage 85.5% of statements (fixture cases: clean/divergence/missing all exercised).
- `go test ./tools/beads-mirror-audit/ -count=1 -cover` — PASS, coverage 47.7% of statements.
- `go run ./tools/beads-mirror-audit --store /home/ajantha/projects/srv-jobfile/.beads` — PASS, output `mirror clean (49 beads)`, exit 0 (read-only: shells out to `br list --json` only).
- `go run ./tools/beads-mirror-audit --store /home/ajantha/projects/srv-jobfile/.beads --json` — PASS, `{"divergences":[],"checked":49}`.
- `go build ./...` — PASS, exit 0.
- `gofmt -l internal/mirroraudit/ tools/beads-mirror-audit/` — PASS after the audit.go fix (listed audit.go before, clean after).
- `go vet ./internal/mirroraudit/ ./tools/beads-mirror-audit/` — PASS, exit 0.

## Open edges for the next phase

- `internal/mirroraudit/runner.go` BrListJSON seam remains at 0% coverage in the package test (exec seam; exercised only via the tool's own test at 47.7%). If a future bead hardens the runner, add a stubbed exec seam test.
- Parent AC2 hardcodes the live `srv-jobfile` store path; if that path is ever absent on a fresh checkout the command will fail — the AC is host-specific, not portable.
- Tool coverage (47.7%) is bounded by main()/flag parsing; this is the CLI wrapper, not the shared classifier, and is acceptable for the AC.
