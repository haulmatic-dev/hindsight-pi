# SYNTAX handoff (task-cryg)

## What changed (files + one line each)
- internal/core/tool_health.go: sweeper + hindsight/cass/graphify/llm-router contract probes (cherry-picked GREEN commit af5a1419 = cace0bfd from hydra/wt-hydra-jipg); all exported symbols documented (DefaultProbeTimeout, ToolHealthConfig, ToolHealthSweeper, NewToolHealthSweeper).
- internal/core/tool_health_test.go: 10 contract tests (RED commit bb41b853 = abc91de7) — now GREEN.
- internal/core/config.go / overseer.go / service_starter.go: shepherd.tool_health wiring (from GREEN commit).
- CHANGELOG.md, docs/architecture/shepherd.md, docs/hydra/handoff/red-jdvp.md: from GREEN/RED commits.
- One wrong-strand cherry-pick (9fed8aef, tool-repair closed-loop chain) was applied, found to clobber the sweeper file, and reverted at 754329af — the closed-loop chain (rhin/6t03/y4i4) is a DIFFERENT chain and must not be merged here.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS (rc=0).
- `go vet ./internal/core/` — PASS (rc=0, clean).
- `gofmt -l` on tool_health.go, tool_health_test.go, config.go, overseer.go, service_starter.go — clean (empty list).
- `go test ./internal/core/ -run 'TestToolHealthContract' -count=1` — PASS (ok 0.315s).
- Placeholder grep (TODO/FIXME/Placeholder) over tool_health.go + tool_health_test.go — no matches.
- Doc comments verified on all 4 exported symbols.

## Open edges for the next phase
- The tool-repair closed-loop chain (9fed8aef/4fc47a71/y4i4/9c51647b) edits the SAME file internal/core/tool_health.go — integration of both chains needs a manual merge combining sweeper + repair code; do not cherry-pick blind.
- Parent ACs beyond syntax (stub-sweep zero events, degradation latch, disabled-at-false) are pinned by the 10 contract tests, which pass.
