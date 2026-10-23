# PRD: PRD Enhancement Pipeline — Drift Gates, Claim Re-Proof, Enrichment, Turn Telemetry

**Version:** 1.0 · **Date:** 2026-10-22 · **System Type:** library · **Repo:** components/hydra

## Problem

PRDs are verified at authoring time and trusted forever after. The measured failure
class is stale facts at consumption time (doctrine record: a PRD cited a jest.config.ts
that never existed and a 52-spec baseline that was actually 73). Workers then spend
turns re-deriving what a precomputed payload should have carried (ajantha-srv-data-harbour-8nz
re-derived 47 FRs of chain history across 7 runs, 40+ turns each), or trust wrong facts.

Design (from adversarial review 2026-10-22): deterministic gates on both sides of any
LLM enrichment — enrichment may not launder hallucinations or rot unnoticed; it reuses
existing mechanisms (frozen-context injection `cmd/worker/main.go:2562`, ContextPack
anchor re-validation `pack_miss`, `task-breakdown --validate`) rather than adding a
fourth pipeline stage.

## Verified Facts (measured 2026-10-21/22)

- Pack-guided vs unguided: 68-turn p50 vs 20 (verify: docs/PRDs/ sizing notes + srv-smart-engine campaign record).
- ≤12-file beads completed first attempt; 97/56-file beads demanded 539/811 turns and hit the timeout wall (same record).
- Token-burn shape: turns × ~100K context, quadratic; kgkc7 = 20.5M tokens, 200+ turns, non-converging (CASS bullet, 0.80 score).
- No queryable turn telemetry exists: `grep -c '"type":"turn_end"'` across ALL worker logs incl. rotated `.gz` = 0 hits; session files also carry none (verified 2026-10-22).
- Frozen-context injection exists: `cmd/worker/main.go:2562` "FROZEN CHAIN CONTEXT … Do NOT re-derive what they hold".
- Anchor re-validation pattern exists: ContextPack `pack_miss` events drop broken anchors on refresh (`internal/core/worker_spawner.go` ensureContextPack).
- `task-breakdown --validate` analyze-only gate exists and is mandated at PRD authoring (since 2026-10-18).
- Hard caps without calibration produce zombies: hydra-mt8 exhausted a 40-turn budget mid-task, session dead 3 days (institutional memory).

## Assumptions

- Enrichment payloads ride the existing `--agent-context` store path; cross-uid write failures are handled by the current campaign's FR-005 precision fix.
- Turn-budget VALUES are deliberately out of scope until FR-005 telemetry yields a measured distribution (no guesswork settings — Oct-5 doctrine).

## Out of Scope

- Setting `turn_budgets` values (await telemetry).
- task-breakdown skill edits (live in untracked shared Pi config, not this repo) — skill-side drift-check invocation is an operator config edit at unpark time.

---

### FR-001: Deterministic payload drift-check at spawn preflight

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `internal/core/worker_spawner.go`, `internal/core/bead_claims.go`, `internal/core/bead_claims_test.go`
**Test Command:** `go test ./internal/core/ -run 'BeadClaims|DriftCheck' -count=1`
**Build Command:** `go build ./...`

**Acceptance Criteria:**
- New module parses bead description/payload for claim lines of the form `claim: <assertion> (verify: <shell cmd>)` (same convention as PRD machine-checkable state claims).
- At spawn preflight, each claim's verify command runs with a 2s timeout; results split: `pass`, `fail`, `unknown` (timeout/parse error).
- `fail` on a claim marked `hard:` prefix → spawn refused, typed event `claim_hard_failed` with `{bead, assertion, output-head}`; without `hard:` prefix → warning only, typed event `claim_soft_failed`.
- Zero claims in payload → no-op, no events, spawn proceeds (backward compatible).
- All probe failures fail open except `hard:` (product gate fail-closed, tooling fail-open doctrine).
- `go test ./internal/core/ -run 'BeadClaims|DriftCheck' -count=1` exits 0.

### FR-002: Claim re-proof at respawn with payload persistence

**Type:** fr
**Priority:** P0
**System Type:** library
**Depends On:** FR-001
**Target Files:** `internal/core/worker_spawner.go`, `internal/core/bead_claims_test.go`
**Test Command:** `go test ./internal/core/ -run 'BeadClaims' -count=1`
**Build Command:** `go build ./...`

**Acceptance Criteria:**
- Verified-claim results are cached on the bead payload (label `claims-ok-<first8>` + timestamp); respawn reuses cached passes without re-running probes unless the worktree base_sha changed.
- Changed base_sha → full re-proof; broken claims dropped from the injected payload with typed event `claim_dropped` `{bead, assertion}` (mirror of `pack_miss`).
- Cached-pass reuse adds <5ms to spawn path (test asserts wall-time bound).
- `go test ./internal/core/ -run 'BeadClaims' -count=1` exits 0.

### FR-003: Brownfield subagent enrichment emitting claims + summaries

**Type:** fr
**Priority:** P1
**System Type:** library
**Target Files:** `cmd/worker/enrich.go`, `cmd/worker/enrich_test.go`
**Reads From:** `cmd/worker/main.go`
**Test Command:** `go test ./cmd/worker/ -run 'Enrich' -count=1`
**Build Command:** `go build ./...`

**Acceptance Criteria:**
- `enrich(bead) (claims []string, summary string, err error)` runs a subagent (pi `--mode rpc` child or detached invocation) scoped to the bead's Target Files ONLY; brownfield trigger = Target Files all exist on the base branch.
- Output contract: every factual assertion in `summary` must have a matching `claim: … (verify: …)` line; assertions without claims are dropped by the emitter (anti-laundering gate).
- Summary is token-budgeted (reuse `context_pack.max_pack_tokens`, default 40000); overflow → oldest-first elision with footer `elided: n`.
- Injection rides the existing frozen-context path: enrichment output is committed as a chain handoff doc (`docs/hydra/handoff/enrich-<bead>.md`) so `main.go:2562` picks it up — no new injection channel.
- Greenfield beads (Target Files don't exist) skip enrichment entirely, log one line, cost zero tokens.
- `go test ./cmd/worker/ -run 'Enrich' -count=1` exits 0 with a fake-agent test double.

### FR-004: Turn telemetry in worker close events

**Type:** fr
**Priority:** P0
**System Type:** library
**Target Files:** `cmd/worker/reporter.go`, `internal/core/bead_closer.go`, `cmd/worker/token_reporter_test.go`
**Test Command:** `go test ./cmd/worker/ ./internal/core/ -run 'Turn' -count=1`
**Build Command:** `go build ./...`

**Acceptance Criteria:**
- Worker counts agent turns (rpc `turn_end` events observed during the session) and reports `TurnsSoFar` in the heartbeat report alongside `TokensSoFar`.
- The close/complete path emits typed event `worker_turns` with `{bead, turns, phase, outcome}` on every worker exit (success and failure).
- RED test first: a fake rpc session streaming 3 `turn_end` events must produce `TurnsSoFar=3` — must fail on current main (today no turn counter exists; verified: zero `turn_end` observability).
- `go test ./cmd/worker/ ./internal/core/ -run 'Turn' -count=1` exits 0.

### FR-005: Turn distribution extractor + budget calibration report

**Type:** fr
**Priority:** P1
**System Type:** cli
**Depends On:** FR-004
**Target Files:** `tools/turn-report/main.go`, `tools/turn-report/main_test.go`
**Test Command:** `go test ./tools/turn-report/ -count=1`
**Build Command:** `go build ./tools/turn-report/`

**Acceptance Criteria:**
- `tools/turn-report --events-dir <dir> --days N` reads `worker_turns` events and prints per-phase percentiles (p25/p50/p75/p90/max) and a suggested `turn_budgets:` yaml block = round-up-to-10 of p90 per phase, emitted as a COMMENT for operator review — never written to config automatically.
- Zero events → prints `insufficient data: N events` and exits 0 (fail-open reporting).
- Includes drift warning when a phase's sample size < 20.
- `go test ./tools/turn-report/ -count=1` exits 0.
