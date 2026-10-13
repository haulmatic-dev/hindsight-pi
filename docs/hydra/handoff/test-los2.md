# test handoff (test-los2)

## What changed (files + one line each)
- internal/core/postclose_sweep.go: GREEN implementation for the RED scaffold's pinned contract — `scan()` sweep (per project: closed beads with metadata `test_command`, UpdatedAt ≤24h), flake guard (failing run re-run once; pass → `postclose-flake` label, no reopen/comment), reopen path (comment carrying BOTH failing-run outputs, `UpdateBeadStatus` → open, `postclose-reopened` label), bounded auto-reopen (already-`postclose-reopened` bead failing twice → `needs-human`, no second reopen); `failingOutput` helper renders the error string when the command produced no output (`false` case); real hourly `Start()` loop (sweep at start + ticker) honoring `stopChan`; all mutations fail-open, commenter nil-safe.
- Original validation-round-3 strand cherry-picked from `hydra/quarantine/hydra-kmpq` / `hydra/quarantine/hydra-qbnp` (not on development):
  - 96bb5f2a (RED: internal/core/tool_health_evidence_test.go + evidence_probes_test.go, inert stubs)
  - c53beae4 (GREEN: internal/core/tool_health_evidence.go — `ToolHealthEvidenceScanner` emitting `tool_degraded` probe:"evidence"; internal/core/evidence_probes.go — `IntelUsageEvent` worker event; overseer.go/service_starter.go wiring; CHANGELOG)
  - d319db86 (REFACTOR: ready() guard, emitDegraded, named consts; intel_usage split out of worker intel path)

## Verification (commands run + pass/fail)
- Gate failure diagnosis: /opt/dev-tools/var/hydra/gates/hydra/hydra-los2/test.log — 4 FAIL in internal/core: TestPostCloseSweepReopensAndAttachesBothOutputs / FlakeLabels / SecondReopenAddsNeedsHuman / IntegrationRealGitRepoReopen (postclose_sweep.go was an inert RED scaffold; scan() empty).
- After fix: `go test ./internal/core/ -run 'TestPostCloseSweep|TestEvidenceProbes|TestToolHealthEvidence' -count=1` — PASS (ok).
- `go vet ./internal/core/` — clean; `gofmt -l internal/core/postclose_sweep.go` — clean; `go build ./internal/core/` — PASS.
- `go test ./cmd/worker/ -count=1` — PASS (ok; prior run, strand unchanged since).
- The validation gap "RED bead has no test files (target_files metadata missing)" is stale: test files exist and pass; per prior test-6lmz/82sj/izp2/udlg disposition, remaining flag is bead metadata (br update target_files), not the suite.
- hindsight_search at bead start — not connected (skip rule applied).

## Open edges for the next phase
- If the gate still flags missing target_files metadata, fix is bead metadata, not code.
- Strand not yet landed on origin/development; integration of this worktree branch carries the full chain.
- PostCloseSweeper is not yet constructed/wired in overseer/service_starter — Start loop exists but nothing instantiates the sweeper in the daemon; follow-up feature bead if live post-close re-verification is desired.
