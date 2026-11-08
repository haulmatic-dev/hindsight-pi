# discovery handoff (task-kzlu)

## What changed (files + one line each)
- CHANGELOG.md: Documentation entry under Unreleased recording this verify-first discovery (no code change).
- docs/hydra/handoff/task-kzlu.md: this handoff.

## Verified state (superseded — FR-001 already implemented on this branch)
- `internal/core/bead_claims.go`: `matchClaimLine` / `parseBeadClaims` (claim lines `claim: <assertion> (verify: <cmd>)`, optional `hard:` prefix), `claimVerifyTimeout = 2 * time.Second`, `classifyClaimStatus` + `evaluateClaim` (refactored per landed refactor bead), events `claim_hard_failed` / `claim_soft_failed` / `claim_unknown`.
- Wired at spawn preflight: `internal/core/overseer.go:1614` calls `o.spawnPreflightBeadClaims(bead, projectPath)`; implementation in `internal/core/spawn_preflight.go:249` returns `SpawnSuppressedError{Reason:"hard claim failed: payload drift-check refused spawn"}`.
- Zero claims → no-op, no events (backward compatible) — pinned by TestParseBeadClaims/zero_claims_noop + TestCheckBeadClaims/zero_claims_spawns_with_no_events.
- Follow-on discovery (FR-002 claim re-proof at respawn) already recorded in docs/discovery/claim-respawn-cache-discovery-6osm.md.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'BeadClaims|DriftCheck' -count=1` — PASS (ok, 0.628s).
- Verbose: 3 test funcs, 22 subtests, all `--- PASS`: TestParseBeadClaims (7), TestCheckBeadClaims (7), TestBeadClaimsDefaultPlanVerifyRunner (6 incl. timeout/exit-code/stdout/stderr evidence). 0 FAIL.
- `hindsight_search` at bead start — unavailable (context-mode hook NODE_TOO_OLD; skip rule applied). `cm context`/`graphify` via bash also failed → rg + frozen chain context fallback per protocol.

## AC mapping
1. Claim-line parsing — MET (matchClaimLine/parseBeadClaims).
2. 2s timeout verify at spawn preflight, pass/fail/unknown split — MET.
3. hard: fail → spawn refused + `claim_hard_failed`; soft fail → warn + `claim_soft_failed` — MET.
4. Zero claims no-op — MET.
5. Probe failures fail-open except `hard:` — MET (classifyClaimStatus / claim_unknown).
6. Narrow gate test exits 0 — VERIFIED.

## Open edges for the next phase
- None for FR-001 installation. Concurrency-strand note (from refactor handoff gcip): a discarded test-file delta on bead_claims_test.go was re-checked; current suite green.
- Untracked sibling-strand artifacts in the shared worktree (cmd/worker/turn_telemetry*, internal/registry/types.go, docs/discovery/turn-report-*) belong to other chains — untouched.

HYDRA_VERDICT: {"done": true, "summary": "superseded: discovery for deterministic payload drift-check at spawn preflight — FR-001 already implemented and green (bead_claims.go + spawnPreflightBeadClaims wiring verified at HEAD); verify-first close, docs-only diff", "ac_met": ["claim parsing", "2s timeout pass/fail/unknown", "hard/soft event split", "zero-claims no-op", "fail-open except hard:", "narrow gate test exit 0"], "ac_unmet": [], "files_changed": ["CHANGELOG.md", "docs/hydra/handoff/task-kzlu.md"], "verification": [{"cmd": "go test ./internal/core/ -run 'BeadClaims|DriftCheck' -count=1", "result": "PASS (ok 0.628s, 22 subtests)"}]}
