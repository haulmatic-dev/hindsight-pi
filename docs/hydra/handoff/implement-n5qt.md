# implement handoff

## What changed (files + one line each)
- internal/core/semantic_cluster.go: new — `SemanticClusterConfig`, OpenAI-compatible `/embeddings` client, `cosineSimilarity`, `semanticClusterer` (in-process embedding cache keyed by signature hash, bounded 1024), `findSemanticDuplicate`, helpers `openMetaFixBeads` / `firstFixSigLabel` / `causeFromFixDescription`.
- internal/core/bead_closer.go: `semanticClusterer` + `semanticOnce` fields; lazy `getSemanticClusterer()`; in `createFixBeadForFailedCloseWithExitCode`, after an exact FR-020 signature miss, semantic pass embeds the candidate cause and compares against open meta fix beads' recorded causes (parsed from their "### What failed last time" description block); cosine ≥ threshold → park against the meta bead (labels `fix-clustered` + own sig label, event `fix_semantic_clustered`) instead of filing a new fix bead. All embedding/list errors fail open.
- internal/core/config.go: `ValidationConfig.SemanticClustering` (`validation.semantic_clustering`: enabled/threshold/embed_base_url/embed_model/embed_api_key_env/embed_timeout), defaults threshold 0.82, model text-embedding-3-small, key env OPENAI_API_KEY, timeout 10s.
- internal/core/semantic_cluster_test.go: 9 tests — cosine math, description parsing, telematics replay (4 same-root-cause failures → 1 meta bead + 3 parked), fail-open on embeddings 500, disabled-by-default inert, missing-key inert, embed cache hits once.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS.
- `go test ./internal/core/ -run 'TestSemantic|TestCosine|TestCauseFromFix' -count=1` — PASS (9 tests; replay asserts ≥2 parked originals, so the semantic merge provably fired — string signatures of the 4 causes differ).
- `go test ./internal/core/ -run 'TestCreateFixBead|TestFailureSignature|TestSignatureLabel|TestReleaseParked|TestExtractFailureCause' -count=1` — PASS (27 pre-existing clustering/cause tests).

## Open edges for the next phase
- Deterministic signature label remains the grouping key; semantic pass only fills exact-match gaps — verify no double-parking when both paths match (exact path returns first, semantic only runs on miss, so no overlap by construction).
- Embeddings cache is in-process only; daemon restart re-embeds once per signature (bounded).
- Live replay against the real telematics store (pmgh/qj74/5gq/kher) not run — unit replay uses synthetic logs at unit level; production needs an embeddings endpoint with a real key.
