# fix handoff (fix-oend, original hydra-mlal / REFACTOR per-bead evidence)

## What changed (files + one line each)
- internal/validation/bead_evidence.go: GREEN replaced the inert RED stub — `BeadEvidence` (single `git log --all --format=%H%x00%B%x1e` sweep, whole-word bead-id regex `QuoteMeta(id)+([^[:alnum:]]|$)`, one batched `git diff-tree --stdin --no-commit-id --name-only -r --root -z` over ALL matching commits, `ls-tree -r -z HEAD` blob map, `status --porcelain=v1 -z -uall` dirty set excluding `.beads/` with rename old-path skip, deterministic sha256 over sorted `path:blob-id` pairs, fail-open: non-repo/empty/git-fault → empty Evidence + nil error) and `ValidateTDDChainEvidence` (RED: ≥1 `beadfiles.IsTestFile` evidence file with meaningful assertion; GREEN: ≥1 non-test non-docs evidence file that is not a stub — Go via `DetectStubs`, others non-empty; gap rule: `no test file in bead evidence` / `no implementation file in bead evidence` naming the bead id when evidence non-empty-or-declared; empty evidence + no declared files or unlocated side → no gaps) reusing `beadfiles.Resolve`/`IsTestFile` — no second parser.
- internal/validation/tdd_validator.go: `hasMeaningfulAssertions` upgraded from lowercase-substring to case-sensitive `assertionPatterns` (`func Test`, `t.Run(`, `t.Error(`, `t.Fatal(`, `assert.`, `require.`, `expect(`, `describe(`, `def test_`) via package-level `meaningfulAssertions`; method delegates. Bare `import "testing"`, `// T.RUN(`, `ASSERT.Equal`, `EXPECT(` no longer read as tests (validator-truth FR-003). Struct field alignment in `TDDValidationResult` normalized (gofmt; file was in this diff).
- CHANGELOG.md: Added entry under Unreleased (GREEN + note the original strand died on provider-credit 402 before landing code).

## Verification (commands run + pass/fail)
- Verify-first: original REFACTOR verification = `BEAD_EVIDENCE_ENFORCE=1 go test ./internal/validation/ -count=1 -run 'TestBeadEvidence|TestTDDValidator'` — PASS 17/17 (was: 15 skips + 2 … i.e. RED gate); without the gate the same run is `ok` with zero skips (checked `-v`: no SKIP lines among evidence tests).
- `go build ./...` — PASS ("[OK] Build successful").
- `go vet ./internal/validation/ ./internal/core/` — PASS (rc 0).
- `go test ./internal/validation/ -count=1` — PASS (`ok ... 0.998s`).
- `go test ./internal/core/ -run 'TDDValidation|ValidationService' -count=1` — PASS (consumers unaffected; the case-sensitive tightening changes no core test — the fix-bead test there relies on missing files, not assertion casing).
- `gofmt -l` on touched files — clean (8 other pre-existing unformatted files in the package are upstream-dirty, not in this diff).

## Open edges for the next phase
- No close-path/spawn wiring yet: `ValidateTDDChainEvidence` has no production caller (`rg` shows only the validation package). Wiring into `TDDValidator.ValidateTDDChain` / validation service is a separate bead — smallest-change discipline kept this bead package-local.
- `gitBlobMap` reads the whole HEAD tree per `BeadEvidence` call; fine for repo-scale use, but a caller sweeping thousands of beads should batch or cache — not optimized here (no caller yet).
- Fingerprint covers committed paths only for blob ids; dirty paths contribute `path:` with empty id (no test pins dirty-content sensitivity).
