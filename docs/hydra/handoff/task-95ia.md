# green handoff

## What changed (files + one line each)
- internal/core/bead_closer.go: durable verified-close escalation ladder — `rejectClose` writes a `verified-close-reject-attempt-N` label each rejection round (replacing the previous round's label), and `validationRetryCount` now returns max(in-memory retry count, `durableRejectAttempts(bead.Labels)`), so a daemon restart or Charon orphan-reopening cannot reset the ladder to zero and let gate failures cycle forever without reaching max rounds.
- internal/core/pan_health.go: fleet health digest alerts on silent gate deaths — `GateFailedOpen` (non-terminal beads carrying `gate-failed` without `escalated`/`needs-human`) and `ClosedFailedRecent` (beads labeled `closed-failed` with UpdatedAt within 4h) both fire P0 campaign-anomaly lines so a Pan judgment wakes on the death itself.
- internal/pan/health.go: `ProjectHealth.GateFailedOpen` + `ClosedFailedRecent` fields; `Text()` renders `gatefailed=N closedfail=N` in the stable per-project line.
- internal/core/bead_closer_gate_escalation_test.go (new): 2 tests — restart-resilient ladder (3 rejections across a simulated retry-manager reset escalate with the `escalated` label + attempt-label floor) and `durableRejectAttempts` parser table.
- internal/core/pan_health_gate_deaths_test.go (new): digest test — open gate-failed counted, escalated gate-failed skipped, recent closed-failed counted, old (24h) closed-failed outside window, anomaly lines + `gatefailed=`/`closedfail=` digest-text fields asserted.
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS; `go build ./internal/core/ ./internal/pan/` — PASS.
- `go vet ./internal/core/ ./internal/pan/` — PASS (clean).
- `gofmt -l` on all touched files — clean.
- `go test ./internal/core/ -run 'TestRejectClose_DurableEscalationLadderAcrossRestart|TestDurableRejectAttempts' -count=1` — PASS (4).
- `go test ./internal/core/ -run 'TestRejectClose|TestGate|TestCloseBead|TestVerifiedClose|TestCollectHealthDigest_GateDeathsAlert|TestDurableRejectAttempts' -count=1` — PASS (close-path regression set incl. existing rejectClose escalation/below-max tests and gate_close tests).
- `go test ./internal/core/ -run 'TestCollectHealthDigest' -count=1` — PASS (all digest tests; 345s run).
- `go test ./internal/pan/ -count=1` — PASS.
- Ground truth: `br show hydra-ee5r / hydra-lkbo / hydra-ejsk` — all carried `gate-failed` + `gate-failed-test` labels with no `escalated`/`needs-human` after their 2026-10-04 15:45–16:17 gate deaths; events show repeated gate_run failures with reopen cycles (lkbo later in_progress with `verified-close-reject-gate-test-failed`). Exactly the silent-death class this bead fixes.

## Open edges for the next phase
- The 4h closed-failed window is a constant (`closedFailedDigestWindow` in pan_health.go), not a config knob — promote if operators want tuning.
- Real br label add dedupes; the test mock appends labels without dedupe — the attempt-label replace (remove-then-add) keeps both honest, but any future label-counting must not assume mock/real parity.
- Beads ee5r/ejsk/lkbo remain open/in_progress with stale `gate-failed` labels; on next digest tick they will fire the new P0 anomaly — expected (they are genuinely unescalated gate deaths), Pan will alert.

## Gate timeout addendum (hydra-95ia retry, 2026-10-06)
- Daemon test gate `go test ./...` (15m) hit exit -1: log truncated at internal/bv, internal/core still running. Measured in-worktree: internal/core = 387.7s WITHOUT this bead's tests vs 400.7s with — this bead adds ~12s (~1s net after fixing); the gate timeout is marginal PRE-EXISTING.
- Fix applied (21aa6d9): TestCollectHealthDigest_GateDeathsAlert now builds a minimal Overseer (no NewOverseer IntelligenceHub/MCP init). Test binary runs in 21s.
- cm add recorded the package-init/gate-budget pattern (category testing).
- Open edge (operator): the 15m gate budget + 6.7m internal/core is a pre-existing squeeze; consider splitting the gate or raising its timeout.
