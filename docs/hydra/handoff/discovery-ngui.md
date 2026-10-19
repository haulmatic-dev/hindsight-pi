# discovery handoff (hydra-ngui, verify-first re-run)

## What changed (files + one line each)
- docs/hydra/handoff/discovery-ngui.md: this handoff (verify-first re-run; no code change — the FR-004 deep-tier implementation landed on sibling strand hydra/wt-hydra-cbww, verified green here).
- docs/discovery/evidence-of-use-probes-discovery-ngui.md: unchanged from first pass (requirements + AC mapping remain valid).

## Verified outcome (FR-004 implementation state)
- Implementation commits exist on branch `hydra/wt-hydra-cbww`: 9da19047 `feat(intel): evidence-of-use probes (deep tier) — intel_usage worker event + tool_degraded evidence sweep` (+448: cmd/worker/intel_usage.go + test, internal/core/evidence_probes.go + test, tool_health_evidence.go, overseer.go + service_starter.go wiring) and 4af35a30 `refactor(intel): evidence-of-use sweep — ready() guard, emitDegraded, named consts; split worker intel_usage emitter`.
- `git merge-base --is-ancestor 9da19047 origin/development` — NO → not yet landed on origin/development; integration belongs to the cbww strand, not this discovery bead.
- Verify-first AC checks ran in-cwd against the cbww tip (files checked out temporarily, then removed; no commit):
  - `go test ./internal/core/ -run 'TestToolHealthEvidence' -count=1` — ok (PASS).
  - `go build -buildvcs=false ./...` — PASS.
  - `go test ./cmd/worker/ -run 'IntelUsage' -count=1 -v` — PASS 3/3 (WritesJSONLBeforeFields, FailOpen, GraphifyFallbackFor).

## Open edges for the next phase
- CBWW strand integration: land hydra/wt-hydra-cbww (GREEN + refactor commits) via the normal dogfood PR path — this discovery bead must NOT duplicate it.
- AC wording "sweep emits" implies the sweeper is wired into daemon Start (service_starter.go) — confirmed present in 9da19047 diff.
- Shallow-tier tool_degraded/tool_recovered (US-001) still absent on origin/development; deep-tier `probe:"evidence"` discriminator avoids collision when it lands.
