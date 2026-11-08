# red handoff

## What changed (files + one line each)
- internal/core/tool_health_test.go (new): 10 RED tests pinning the shepherd tool-health probe framework — all-healthy→zero events; hindsight fail → exactly one `tool_degraded` (fields["tool"]="hindsight") across 3 sweeps + one `tool_recovered` on recovery; per-probe timeout bounds sweep (DefaultProbeTimeout == 10s); `ToolHealthConfig{Enabled:false}` → Start no-op, zero events; hindsight contract probe (GET /health 200 + "healthy", POST /v1/default/banks/hydra/memories/recall 200 + JSON, recall endpoint actually exercised); cass probe (runCommand `cm context --json`, exit 0 + success:true; failure → tool_degraded tool=cass); constructor builds all four default probes (ProbeNames); graphify probe (per project: graphify-out/graph.json exists + mtime fresh vs 2× refresh interval; missing → tool_degraded tool=graphify); llm-router probe (health URL 200; unreachable → tool_degraded tool=llm-router).
- internal/core/tool_health.go (new): inert RED stub — DefaultProbeTimeout const, ToolHealthConfig, ToolHealthSweeper with no-op constructor/SetProbe/SetProbeTimeout/ProbeNames/sweep/Start/Stop so the package compiles for the lint gate; zero behavior.
- CHANGELOG.md: Added entry under Unreleased.

## Contract GREEN must implement
- `ShepherdConfig.ToolHealth ToolHealthConfig yaml:"tool_health"` with `ToolHealthConfig{Enabled bool; IntervalSeconds int; HindsightBaseURL string; LLMRouterURL string; GraphifyRefreshInterval time.Duration}`.
- `NewToolHealthSweeper(cfg ToolHealthConfig, getProjects func() []types.Project, events *EventSink, httpClient *http.Client, runCommand func(ctx context.Context, name string, args ...string) (string, error)) *ToolHealthSweeper` builds the four default probes: hindsight (config.HindsightBaseURL), cass (`cm context "tool-health probe" --json` via runCommand; exit 0 + parsed success:true), graphify (per getProjects project: <path>/graphify-out/graph.json exists and mtime < 2× GraphifyRefreshInterval), llm-router (GET config.LLMRouterURL → 200).
- `SetProbe(name string, check func(context.Context) error)` — override/add; check == nil removes the probe (test seam).
- `SetProbeTimeout(d)`, `ProbeNames() []string`, `sweep()`, `Start(ctx)`, `Stop()`.
- Sweep semantics: each probe under DefaultProbeTimeout (10s) context; state-latched per tool — first healthy→unhealthy emits exactly one `tool_degraded` Event{Fields:{"tool":name}}, unhealthy→healthy emits exactly one `tool_recovered`; all-healthy sweeps emit nothing; repeated failing sweeps emit nothing further.
- Enabled=false → Start is a no-op; the sweep goroutine is never launched.

## Verification (commands run + pass/fail)
- `go build -buildvcs=false ./...` — PASS.
- `gofmt -l internal/core/tool_health*.go` — clean.
- RED gate: `go test -c ./internal/core/` + `./core.test -test.run TestToolHealthContract -test.v` — 3 trivially pass (inert stub = zero events / instant return), 6 FAIL for the RIGHT reason, verbatim:
  - `tool_health_test.go:125: want exactly 1 tool_degraded across 3 failing sweeps, got 0: []`
  - `tool_health_test.go:207: hindsight probe must exercise the recall endpoint (contract probe)`
  - `tool_health_test.go:265: failing cm context must degrade exactly once with tool=cass, got []`
  - `tool_health_test.go:318: missing graph.json must degrade exactly once with tool=graphify, got []`
  - `tool_health_test.go:346: unreachable llm-router must degrade exactly once with tool=llm-router, got []`
- Note: the package's go test output is rewritten by a wrapper ("Test Results: PASS/FAIL" block) that hides per-test lines — run the compiled binary directly for verbose output.

## Open edges for the next phase
- Daemon wiring (NewOverseer + service_starter Start/Stop + real hindsight base URL / llm-router URL from existing config) is GREEN's job; config currently has no ToolHealth field.
- runCommand seam passes argv-style (name + args); GREEN should exec without a shell (verification_runner precedent).
- Coverage for new code must be measured and reported at GREEN per phase criteria.
