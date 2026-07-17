# PRD: Per-Phase Model Routing

## 1. Overview

**Problem:** Every worker uses the single configured model regardless of task phase, so cheap mechanical phases (RED test-writing, discovery) burn the same tokens as hard phases (GREEN implementation), and there is no way to route retries or specific phases to a stronger model.
**Solution:** An optional `agents.pi.phase_models` config map that routes beads to a provider/model by TDD phase (derived from the bead's title prefix and type), falling back to the primary model when unset.
**Success Metrics:**
- With `phase_models.red` configured, a `RED:` bead's worker env carries that provider/model; all other beads keep the primary.
- With no `phase_models` configured, behavior is byte-for-byte unchanged.
- Circuit-breaker failover still overrides everything when open.

---

## 2. User Stories

### US-001: Cheap phases run on cheap models
**As a** Hydra operator
**I want** to route TDD phases to different models via config
**So that** token spend matches task difficulty without touching code.

**Type:** us-integration
**Priority:** P1

**Aggregates:**
- FR-001: Phase-Model Config
- FR-002: Phase Detection and Env Routing

**Acceptance Criteria:**
- `go test ./internal/core/...` exits 0
- A yaml config with `agents.pi.phase_models.red: {provider: p1, model: m1}` parses, and `Build` for a bead titled `RED: write tests` emits `HYDRA_PI_PROVIDER=p1` and `HYDRA_PI_MODEL=m1`
- No mocking of internal dependencies in the integration test

---

## 3. Functional Requirements

### FR-001: Phase-Model Config
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/config.go`

In `internal/core/config.go`: (1) add `type PhaseModel struct { Provider string `yaml:"provider"`; Model string `yaml:"model"` }`. (2) Add field `PhaseModels map[string]PhaseModel `yaml:"phase_models"`` to the existing `PiConfig` struct (~line 34, the `agents.pi` block). (3) Add method `func (p PiConfig) ModelForPhase(phase string) (string, string)` returning the provider and model for a phase: look up `strings.ToLower(strings.TrimSpace(phase))` in `PhaseModels`; when the entry exists, a non-empty `Provider`/`Model` each override the primary (an entry may set only `model`); when absent or the map is nil, return the primary `p.Provider, p.Model` unchanged.

**Acceptance Criteria:**
- `PiConfig{Provider:"pp",Model:"pm"}.ModelForPhase("red")` returns `("pp","pm")` (nil map fallback)
- With `PhaseModels: {"red": {Provider:"p1", Model:"m1"}}`, `ModelForPhase("RED")` returns `("p1","m1")` (case-insensitive)
- With `PhaseModels: {"green": {Model:"m2"}}` and primary `pp/pm`, `ModelForPhase("green")` returns `("pp","m2")` (partial override)
- A yaml config containing `agents:\n  pi:\n    phase_models:\n      red: {provider: p1, model: m1}` parses with `PhaseModels["red"].Provider == "p1"`
- `go build ./...` exits 0
- `go vet ./internal/core/...` exits 0

**Depends On:** —

---

### FR-002: Phase Detection and Env Routing
**Type:** fr
**Priority:** P1

**Target Files:** `internal/core/worker_env_builder.go`

In `internal/core/worker_env_builder.go`: (1) add `func beadPhase(bead types.Bead) string` returning one of `"red"`, `"green"`, `"refactor"`, `"syntax"`, `"discovery"`, `"fix"`, or `""`: if the upper-cased trimmed title starts with `RED:`→red, `GREEN:`→green, `REFACTOR:`→refactor, `SYNTAX:`→syntax, `DISCOVERY:`→discovery, `FIX:`→fix; else if the bead type equals `fix` (case-insensitive) →fix; else empty. (2) In `buildPiEnv`, the function currently resolves the primary provider/model then applies the circuit-breaker failover; it must now be phase-aware: change `buildPiEnv()` to `buildPiEnv(bead types.Bead)` (both callers, `Build` and `BuildForContextEnrichment`, already have the bead in scope — pass it), and BETWEEN primary resolution and the breaker check apply `p.Provider, p.Model = cfg.ModelForPhase(beadPhase(bead))` — but only when `beadPhase` is non-empty and the phase has an entry (i.e. simply assign the returned pair; `ModelForPhase` already falls back to primary so unconditional assignment after resolving defaults is correct ONLY if called with the resolved primary — instead call it on the config value first, then apply the existing standard-agent fallbacks for any still-empty field). Keep the order: phase routing first, circuit-breaker failover LAST (an open breaker overrides phase routing). Log one line `[Worker] Phase routing: <phase> -> <provider>/<model>` only when the phase changed the selection.

**Acceptance Criteria:**
- `beadPhase(types.Bead{Title:"RED: x"})` returns `"red"`; `{Title:"GREEN: x"}` `"green"`; `{Title:"anything", Type:"fix"}` `"fix"`; `{Title:"EPIC: x", Type:"epic"}` `""`
- With `phase_models.red = {provider p1, model m1}`, `Build` for a `RED:` bead emits env containing `HYDRA_PI_PROVIDER=p1` and `HYDRA_PI_MODEL=m1`
- The same config for a `GREEN:` bead emits the primary provider/model
- With no `phase_models`, emitted env is unchanged for all beads
- With the circuit breaker forced open (existing test hooks), the fallback provider wins over the phase-routed one
- `go build ./...` exits 0
- `go test ./internal/core/...` exits 0

**Depends On:**
- FR-001: Phase-Model Config

---

## 4. Non-Functional Requirements

### NFR-SYS: System Integration Verification
**Type:** nfr-verification
**Priority:** P2

**Acceptance Criteria:**
- Full project builds with `go build ./...` and exits 0
- `go test ./internal/core/...` exits 0
- No empty files or placeholder comments remain in `Target Files` of any closed bead
- `internal/core/config.go` and `internal/core/worker_env_builder.go` both compile as part of the package

**Verifies:**
- FR-001: Phase-Model Config
- FR-002: Phase Detection and Env Routing

---

## 5. Technical Considerations

### Architecture
- Pure config + env plumbing: the worker already honors `HYDRA_PI_PROVIDER`/`HYDRA_PI_MODEL` per spawn, so no worker-side change is needed.
- Precedence (weakest→strongest): primary → phase route → circuit-breaker failover. The breaker exists to survive provider outages; it must never be shadowed by routing.

### Technology
- Go standard library only. Table-driven tests; yaml parse test via the existing config test helpers.

### Implementation Map

| Requirement | Target Files | Type | Depends On | Chain |
|-------------|-------------|------|------------|-------|
| FR-001 | `internal/core/config.go` | fr | — | — |
| FR-002 | `internal/core/worker_env_builder.go` | fr | FR-001 | — |
| US-001 | — | us-integration | FR-001, FR-002 | — |
| NFR-SYS | — | nfr-verification | FR-001, FR-002 | — |

### Edge Cases
- Unknown phase keys in config (e.g. `phase_models.banana`): ignored (lookup never hits them).
- Empty PhaseModel entry (`red: {}`): both fields empty → primary unchanged.
- Beads with no recognizable phase (epics, gates): empty phase → primary.

### Constraints
- No behavior change when `phase_models` is absent.
- Breaker precedence unchanged.

---

## 6. MoSCoW

**Must Have:** FR-001, FR-002, breaker precedence.
**Should Have:** Partial (model-only) overrides; routing log line.
**Could Have:** Retry-escalation routing (attempt N≥2 → stronger model) — deferred until the attempt count is available in the spawn path.
**Won't Have:** Per-project phase maps; worker-side changes.

---

## 7. Research Insights

### Codebase Findings
- `PiConfig` (the `agents.pi` block) lives in `internal/core/config.go` ~line 34 with primary `Provider`/`Model` and `FallbackProvider`/`FallbackModel` for the circuit breaker.
- `buildPiEnv` in `internal/core/worker_env_builder.go` resolves primary → standard-agent fallbacks → hard defaults → breaker override, then emits `HYDRA_PI_PROVIDER`/`HYDRA_PI_MODEL`; both callers (`Build`, `BuildForContextEnrichment`) have the bead in scope.
- Title prefixes emitted by task-breakdown: `RED:`, `GREEN:`, `REFACTOR:`, `SYNTAX:`, `Discovery:`, `Fix:`; fix beads also carry type `fix`. The worker-side `phaseGuidance` (cmd/worker) uses the same convention.

### Historical Patterns
- WO-I item 4 in `docs/hydra-analysis-20260712iv.md`. The provider/model env plumbing was live-validated across dogfood runs 3-6; this PRD only adds selection logic in front of it.
