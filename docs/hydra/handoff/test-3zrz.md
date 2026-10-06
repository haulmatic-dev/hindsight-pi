# test handoff (test-3zrz)

## What changed (files + one line each)
- docs/hydra/handoff/test-3zrz.md: this handoff (validation round 1, verify-first close; no code change — the failure-signature classification and closed-loop repair test files already exist and pass).
- No source change: the claimed gap "RED bead has no test files (target_files metadata missing)" is stale.

## Verification (commands run + pass/fail)
- Test files present on this branch (tracked at HEAD 343b7377): `cmd/worker/{transient_overload_test.go,forced_kill_classification_test.go,quota_classification_test.go,concurrency_classification_test.go}` (failure classification), `internal/core/fix_cause.go` + `internal/core/bead_closer_fix_cluster_test.go` (failure-signature classification + closed-loop fix-bead clustering), `internal/core/repair_handlers_test.go` (repair handlers).
- `go test ./internal/core/ -run 'TestFailureSignature|FixCluster|Signature' -count=1` — PASS; 5 tests: TestAdversaryBlockerSignatureOrderStable, TestFailureSignature_ScrubsBeadIDs, TestSignatureLabel_DurableAndStable, TestCreateFixBead_SameSignatureParksAgainstMeta, TestCreateFixBead_DifferentSignatureCreatesSeparateMeta.
- `go test ./cmd/worker/ -run 'Classification|TransientOverload' -count=1` — PASS; incl. TestParkReasonClassification, TestClassifyFailure_TransientOverloadIsNotARateLimit, TestIsTransientOverload.
- `git status --short` — clean tree (no uncommitted source changes).
- graphify-out/graph.json absent in cwd → rg fallback per protocol.
- hindsight_search at bead start — not connected (skip rule applied).

## Open edges for the next phase
- Same disposition as sibling handoffs test-6lmz / test-izp2 / test-4mn6 / test-udlg: if the validation gate still flags missing target_files metadata for the RED bead, the fix is bead metadata (br update target_files), not the test suite.
- No benchmarks exist for this functionality.
