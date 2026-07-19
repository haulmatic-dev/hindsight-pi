# Discovery Report: FR-001 Phase-Model Config

**Task ID:** FR-001  
**Title:** Phase-Model Config  
**Phase:** discovery  
**Date:** 2026-07-17  
**Author:** Hydra dogfood run  
**Related PRD:** `docs/PRDs/per-phase-model-routing-20260717.md`

---

## 1. Executive Summary

FR-001 introduces a configuration-driven mechanism to route Pi worker invocations to a phase-specific provider/model. The implementation lives entirely in `internal/core/config.go` and requires no worker-side changes because the worker already consumes `HYDRA_PI_PROVIDER` and `HYDRA_PI_MODEL` (emitted by `internal/core/worker_env_builder.go` in FR-002).

The discovery phase has concluded that the requirement is unambiguous, the data model is small, and the implementation can proceed with the design documented below. Implementation is already in place and verified; this report captures the analysis that drove it.

---

## 2. Requirement Restatement

Add a per-phase model override map to the existing `agents.pi` configuration block:

```yaml
agents:
  pi:
    provider: primary-p
    model: primary-m
    phase_models:
      red:
        provider: p1
        model: m1
      green:
        model: m2
```

For a given phase string (e.g., `red`), resolve the effective provider/model by:
1. Starting with the primary `provider`/`model`.
2. Looking up the phase in `phase_models` using a case-insensitive key.
3. Overriding the provider and/or model only when the phase entry provides a non-empty value (partial overrides allowed).
4. Falling back to the primary values when the phase is unknown, the map is nil, or the entry is empty.

---

## 3. Acceptance Criteria Review

| # | Criterion | Status | Notes |
|---|-----------|--------|-------|
| 1 | `PiConfig{Provider:"pp",Model:"pm"}.ModelForPhase("red")` returns `("pp","pm")` | ✅ Implemented | nil/empty `PhaseModels` map falls back to primary. |
| 2 | `PhaseModels: {"red": {Provider:"p1", Model:"m1"}}` → `ModelForPhase("RED")` returns `("p1","m1")` | ✅ Implemented | Lookup uses `strings.ToLower(strings.TrimSpace(phase))`. |
| 3 | `PhaseModels: {"green": {Model:"m2"}}` with primary `pp/pm` → `ModelForPhase("green")` returns `("pp","m2")` | ✅ Implemented | Only non-empty fields override the primary. |
| 4 | YAML `agents.pi.phase_models` parses correctly | ✅ Implemented | Snake-case YAML tags on `PhaseModel` and `PhaseModels`. |
| 5 | `go build ./...` exits 0 | ✅ Verified | No compile errors. |
| 6 | `go vet ./internal/core/...` exits 0 | ✅ Verified | Clean. |

---

## 4. Ambiguities Identified and Resolutions

### 4.1 Task metadata `data_models` is empty

**Ambiguity:** The task metadata claims “Full data model definitions are available in the task metadata under `data_models` field,” but the array is empty. This makes it impossible to verify the data contract from the task metadata alone.

**Resolution:** The authoritative data model was derived from the PRD (`docs/PRDs/per-phase-model-routing-20260717.md`) and existing `PiConfig` structure. The derived model is:

```go
type PhaseModel struct {
    Provider string `yaml:"provider"`
    Model    string `yaml:"model"`
}

type PiConfig struct {
    // ... existing fields ...
    PhaseModels map[string]PhaseModel `yaml:"phase_models"`
}
```

No additional schema was needed beyond the two new types above.

### 4.2 Case sensitivity of phase keys

**Ambiguity:** The acceptance criteria mention `ModelForPhase("RED")` but the config keys are lowercase (`red`).

**Resolution:** Lookup is case-insensitive by normalizing the input phase with `strings.ToLower(strings.TrimSpace(phase))`. Config keys are expected to be lowercase, matching the convention used in existing phase titles (`RED:`, `GREEN:`, `REFACTOR:`).

### 4.3 Partial override semantics

**Ambiguity:** It is not stated whether a phase entry must provide both `provider` and `model`, or whether omitting one should keep the primary value.

**Resolution:** Partial overrides are supported. A non-empty `Provider` or `Model` overrides only that field; the other field falls back to the primary. This matches the explicit acceptance criterion for `green` with model-only override.

### 4.4 Empty `PhaseModel` entry behavior

**Ambiguity:** What happens if `phase_models.red` is present but has no fields (`red: {}`)?

**Resolution:** The entry is treated as a no-op. `ModelForPhase` only applies non-empty fields, so the primary provider/model is returned unchanged. This is covered by unit tests.

### 4.5 Config cache shallow-copy risk

**Ambiguity:** Adding a map field to `PiConfig` risks sharing mutable state between the cached config and clones returned by `Config.Clone()`.

**Resolution:** `Config.Clone()` deep-copies `Agents.Pi.PhaseModels` so that mutations of the returned config do not affect the cached config. This is explicitly tested.

### 4.6 Interaction with circuit breaker

**Ambiguity:** Should phase routing be applied before or after the circuit-breaker failover check?

**Resolution:** This is explicitly out of scope for FR-001 and belongs to FR-002. The documented precedence is: primary → phase route → circuit-breaker failover. The breaker is always checked last so it can override phase routing when a provider circuit is open. `internal/core/worker_env_builder.go` implements this ordering.

### 4.7 Unknown phase keys in config

**Ambiguity:** What happens if an operator adds a phase key like `banana` under `phase_models`?

**Resolution:** Unknown keys are harmless. They are never matched by `ModelForPhase` because the caller only passes recognized phase strings (`red`, `green`, `refactor`, `syntax`, `discovery`, `fix`, or `""`). The PRD treats this as an edge case and explicitly states such keys are ignored.

---

## 5. Data Model (Derived)

Because the task metadata shipped an empty `data_models` array, the following data model was derived from the PRD and the existing `PiConfig` struct.

### 5.1 `PhaseModel`

| Field | Type | YAML Key | Required | Description |
|-------|------|----------|----------|-------------|
| `Provider` | `string` | `provider` | No | Per-phase provider override; empty means fall back to primary. |
| `Model` | `string` | `model` | No | Per-phase model override; empty means fall back to primary. |

### 5.2 `PiConfig` additions

| Field | Type | YAML Key | Required | Description |
|-------|------|----------|----------|-------------|
| `PhaseModels` | `map[string]PhaseModel` | `phase_models` | No | Maps phase keys to optional provider/model overrides. |

### 5.3 `PiConfig.ModelForPhase`

```go
func (p PiConfig) ModelForPhase(phase string) (string, string)
```

Returns the effective `(provider, model)` for the given phase, applying case-insensitive lookup and partial overrides.

---

## 6. Integration Points

| Component | Role | Impact |
|-----------|------|--------|
| `internal/core/config.go` | Adds `PhaseModel`, `PhaseModels`, `ModelForPhase`, and `Config.Clone` deep-copy. | Direct implementation target for FR-001. |
| `internal/core/worker_env_builder.go` | Detects bead phase and calls `ModelForPhase` before applying circuit breaker. | FR-002 implementation target; consumes FR-001. |
| `cmd/worker/main.go` | Reads `HYDRA_PI_PROVIDER`/`HYDRA_PI_MODEL` from the environment. | No changes required; env plumbing already exists. |
| `internal/core/config_test.go` | Unit tests for `ModelForPhase`, YAML parsing, and deep-copy. | Adds coverage for FR-001. |
| `internal/core/worker_env_builder_test.go` | Integration tests for end-to-end phase routing and breaker precedence. | Adds coverage for FR-002. |

---

## 7. Verification Commands

All verification commands were run and passed in the worktree where this report was generated:

```bash
go build ./...
# (no output)

go vet ./internal/core/...
# (no output)

go test ./internal/core/...
# ok  	github.com/haulmatic-dev/hydra/internal/core	3.226s
```

Additional targeted tests:

```bash
go test ./internal/core/... -run "PhaseModel|ModelForPhase|Clone_PhaseModels"
# PASS
```

---

## 8. Open Questions / Risks

None blocking. The following are recorded for future reference:

1. **Retry escalation:** The PRD notes “Retry-escalation routing (attempt N≥2 → stronger model)” as a deferred feature. It is not part of FR-001/FR-002 and would require the attempt count to be available in the spawn path.
2. **Per-project phase maps:** Explicitly deferred per PRD MoSCoW “Won’t Have” section.
3. **Performance of deep-copy:** `Config.Clone` deep-copies the `PhaseModels` map, which adds a small allocation. Benchmark regression is acceptable because correctness (preventing shared mutable state) outweighs the marginal cost.

---

## 9. Conclusion

The discovery phase for FR-001 is complete. The requirements are clear, the data model is minimal, and the implementation is already in place and verified. The next phase (FR-002: Phase Detection and Env Routing) consumes the `ModelForPhase` method introduced here and is also implemented. No further design work is required for FR-001.

---

## 10. References

- PRD: `docs/PRDs/per-phase-model-routing-20260717.md`
- Implementation: `internal/core/config.go`
- Integration: `internal/core/worker_env_builder.go`
- Tests: `internal/core/config_test.go`, `internal/core/worker_env_builder_test.go`
