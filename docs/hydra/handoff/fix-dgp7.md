# fix handoff (dgp7)

## What changed (files + one line each)
- internal/validation/bead_evidence.go (new): RED-phase inert stub — `Evidence{Files, BlobIDs, Fingerprint}`, `ChainEvidenceInput{Red/GreenBead, Red/GreenLocated, Red/GreenEvidence, RepoRoot}`, `TDDResult = TDDValidationResult` alias, `beadEvidenceImplemented = false`, `BeadEvidence(repoRoot, beadID, includeDirty)` returns empty/nil, `ValidateTDDChainEvidence` returns pass-all. Package compiles, vet clean, zero behavior.
- internal/validation/bead_evidence_test.go (new): 8 `TestBeadEvidence_*` contract tests — commit subject `(hydra-ab12)` matches, body `Bead: hydra-ab12` matches, unrelated commit not evidence, no prefix bleed (hydra-ab123 ≠ hydra-ab12), dirty set (modified tracked + untracked in new dir) only with includeDirty, non-repo dir → empty evidence + nil error, 64-hex deterministic fingerprint that changes with content, whole-word regex shape (`QuoteMeta(id)+([^[:alnum:]]|$)`).
- internal/validation/tdd_validator_test.go (new, file did not exist): 9 `TestTDDValidator_*` evidence-based chain-check tests — RED evidence test file with `t.Run(` passes (zero gaps), RED declared-file + no evidence → exactly one gap naming bead id + `no test file in bead evidence`, no-evidence + no-declared-files → zero gaps, unlocated side → zero gaps, GREEN docs-only evidence (CHANGELOG.md + docs/x.md) → one green gap, GREEN real impl passes, GREEN declared-file + no evidence → `no implementation file in bead evidence`, `import "testing"` only is not meaningful, case-sensitive assertion pattern matrix (`t.Run(`, `func Test`, `t.Error`, `t.Fatal`, `assert.`, `require.`, `expect(`, `describe(`, `def test_` positive; uppercase variants negative).
- CHANGELOG.md: Added entry under Unreleased.
- Gate helper `beadEvidenceRequireImpl(t)`: skips loudly pre-GREEN (`beadEvidenceImplemented == false`), fails under `BEAD_EVIDENCE_ENFORCE=1`.

## Verification (commands run + pass/fail)
- `go build ./internal/validation/ ./internal/core/ ./cmd/worker/` — PASS (rc=0).
- `go vet ./internal/validation/` — PASS (clean).
- `gofmt -l` on the three new/edited files — clean. (Pre-existing gofmt drift in other package files — compliance_gate.go, stub_detector.go, tdd_validator.go, validator.go, prd_validator.go etc. — NOT touched; smallest-change rule.)
- `go test ./internal/validation/ -run 'TestBeadEvidence_|TestTDDValidator_' -count=1 -v` — PASS, exit 0; 17 tests, all SKIP loudly ("RED: per-bead evidence contract not yet implemented").
- `BEAD_EVIDENCE_ENFORCE=1 go test ./internal/validation/ -run 'TestBeadEvidence_|TestTDDValidator_' -count=1` — FAIL with exactly `RED: per-bead evidence contract not yet implemented (BeadEvidence/ValidateTDDChainEvidence are inert stubs)` — proves RED detects the missing contract.
- `go test ./internal/validation/ -count=1` — PASS (`ok ... 0.708s`, whole package incl. pre-existing suites).

## Open edges for the next phase (GREEN must implement)
- `BeadEvidence`: `git log --all -E --grep '<QuoteMeta(beadID)>([^[:alnum:]]|$)' --name-only` over all commits (subject + body both match), union of touched files; dirty set from `git status --porcelain=v1 -z --untracked-files=all` (handle `R old -> new`, exclude `.beads/`) only when `includeDirty`; fingerprint = sha256 over sorted `path:blob-id` (git log --format with %H blob ids or `git ls-tree`); missing git binary / non-repo / git error → empty evidence, nil error.
- `ValidateTDDChainEvidence`: RED passes when evidence has ≥1 `beadfiles.IsTestFile` file with meaningful assertion read from RepoRoot; GREEN passes when evidence has ≥1 non-test source file (not IsTestFile, not `*.md`, not `docs/`, not `CHANGELOG*`) that is not a stub (`DetectStubs`); declared files (`beadfiles.Resolve`) scope the stub check + sibling tests only; gap texts pinned exactly in this suite.
- Replace `hasMeaningfulAssertions` with the case-sensitive pattern list (pinned in TestTDDValidator_AssertionPatternsCaseSensitive); keep `ValidateTDDChain` exported.
- Update `docs/operations/bead-validation-system.md` + `docs/operations/validation-stub-detection.md` (they describe metadata-only target_files) + CHANGELOG (Fixed).
- Note: original hydra-2ev1 failure was a model-call context-length infra error (1.75M tokens), not a code defect — this worktree had NO prior bead_evidence files; the RED work was redone from the PRD contract (`docs/PRDs/validator-truth-prd-20261022.md` FR-003).
