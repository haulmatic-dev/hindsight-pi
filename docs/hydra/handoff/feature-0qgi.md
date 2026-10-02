# feature handoff

## What changed (files + one line each)
- cmd/worker/main.go: fix/bug `phaseGuidance` leads with PRE-IMPLEMENTATION CROSS-VERIFICATION (A1 reproduce on clean origin/<base> checkout, A2 not-reproducible → verify-first close without code changes, A3 grep for existing mechanism) and every fix/bug/feature prompt ends with PRE-CLOSE CROSS-VERIFICATION (B1 re-run original reproduction, B2 diff-verify claims, B3 AC→executed-check mapping); new FEATURE/fr discipline case carries the pre-close half + duplicate-mechanism grep.
- internal/core/empty_close_gate.go: `emitEmptyDiffCompleted` — typed `empty_diff_completed` event when a done verdict is rejected over an empty or target-missing diff (thok-class false-closure backstop, fail-open, nil-sink-safe).
- internal/core/bead_closer.go: backstop wired at both rejection sites (empty-diff gate and target-scope block).
- cmd/worker/phase_guidance_test.go, internal/core/bead_closer_empty_gate_test.go: tests pinning the new doctrine routing and event emission matrix.
- CHANGELOG.md, docs/architecture/shepherd.md (fix-lifecycle guard 1c): operator docs.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./cmd/worker/ ./internal/core/` — PASS.
- `go test ./cmd/worker/ ./internal/core/ -count=1` — PASS (both packages).
- LIVE-FIRE VERIFIED: seeded already-fixed bug bead proj-778 (scratch repo /tmp/livefire-0qgi/proj, Sum() actually correct, description claimed off-by-one) was processed by the real worker binary following the new doctrine — closed with reason `verify-first: defect not reproducible`, `git diff` on calc.go/calc_test.go empty (zero code changes), reproduction (go test) passing quoted. The spawned worker's claim "Successfully closed bead hydra-0qgi" was worker-side only; the live store shows hydra-0qgi still IN_PROGRESS — daemon re-verification governs, no false closure occurred.
- Pre-close diff check: `git diff --stat` shows exactly the 5 code/doc files above; every verdict claim (PRE-IMPLEMENTATION/PRE-CLOSE/FEATURE strings, empty_diff_completed event) greps in the diff.

## Open edges for the next phase
- Empty-diff backstop reads the worker log via the existing empty-gate verdict extractor; beads closed through paths without worker logs get no event (fail-open by design).
- Scratch live-fire repo /tmp/livefire-0qgi contains a vendored hydra-src gitlink from the spawned worker — scratch only, safe to delete.
- Worker-side "closed" log line before daemon re-verification is confusingly worded (observed during live-fire); separate observability nit, not fixed here.
