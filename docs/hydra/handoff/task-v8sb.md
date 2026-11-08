# refactor handoff (refactor-ank6, postclose sweep — re-apply after integration)

## What changed (files + one line each)
- internal/core/postclose_sweep.go: behavior-preserving refactor re-applied on the post-merge variant — eligibility extracted to `sweepableCommand(bead)` (closed + UpdatedAt within 24h + non-empty `metadata["test_command"]`, nil-Metadata safe), reopen comment body extracted to `reopenCommentBody(output1, err1, output2, err2)`, event types named (`evPostCloseNeedsHuman`, `evPostCloseReopened`). Behavior byte-identical (flake path unchanged: label only, no event — matches this variant).
- internal/core/postclose_sweep_test.go: `BenchmarkPostCloseSweepVerifyBead` re-added (integration merge had dropped it with the earlier refactor) — per-bead steady-state sweep cost.
- CHANGELOG.md: Unreleased/Changed entry.

## Verification (commands run + pass/fail)
- BEFORE (this pass): `go test ./internal/core/ -run 'TestPostCloseSweep' -count=1` — PASS (ok, HEAD 24e52402 pre-refactor variant).
- AFTER: same command — PASS (ok 0.149s, 6 tests, 0 failures).
- `gofmt -l` on both files — clean. `go build` / `go vet ./internal/core/` — PASS.
- Benchmark: `go test ./internal/core/ -run XXX -bench BenchmarkPostCloseSweepVerifyBead -benchtime 1000x` — 83.86 ns/op, 0 B/op, 0 allocs/op, PASS (prior runs 59.8–170.5 ns/op; variance = host load, no regression).
- Provenance: the first-pass refactor commits (9146037 + 3db6ae2) fell out of HEAD ancestry when integration merged the n9nq/rxi6 GREEN restore; this pass re-lands the equivalent refactor on the current variant.

## Open edges for the next phase
- Integration-merge provenance: postclose files churn across strand merges; a future merge conflict here must keep `sweepableCommand`/`reopenCommentBody` + the benchmark, not the inline form.
- Flake path in this variant emits no event; if the operator contract wants a `postclose_flake` event, it is a behavior change → separate feature bead.
