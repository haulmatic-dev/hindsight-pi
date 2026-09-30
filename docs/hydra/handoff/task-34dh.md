# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-exhausted-spike-triage-34dh.md: triage of the 2026-09-30 fleet-wide integration-exhausted spike — root cause (operator-API park/unpark toggle loop re-triggering deterministic publisher.go rebase conflicts), entropy-injection audit, 4 proposed remediation beads.
- CHANGELOG.md: Unreleased/Documentation entry pointing at the triage.
- No code change (discovery/triage bead).

## Verification (commands run + pass/fail)
- hindsight_search on integration-exhausted spike terms — 31 records; durable-park-by-design + PR-already-exists root cause confirmed as prior/landed, today's mode is different (rebase conflict). PASS.
- `graphify query ...` — graph.json absent in cwd → rg/handoff fallback per protocol. PASS.
- Event analysis `/opt/dev-tools/var/hydra/events/2026-09-30.jsonl` + `2026-09-29.jsonl` (sandbox script, both days parsed):
  - 09-30 failures all stage `integrate`, all `pr conflict: rebase attempt 1 failed — human review needed`, conflict path `internal/core/publisher.go` (4/6) + triage docs. PASS.
  - `project_parked {"reason":"operator hold"}` / `project_unparked {"reason":"resolved"}` pairs within 1ms (00:05:16.972/.973, 00:06:43.393, 00:07:50.166), period ~60–90s → unpark immediately re-triggers integration → re-exhaustion. PASS.
  - Counter uncapped: hydra-xxq5 exhausted events 20→21→22 in 3 min while parked; 09-29 beads up to 38 exhaustion events each. PASS.
- integration_gate cooldown (30s) < loop period → gate provides no back-pressure (code: internal/core/integration_gate.go). VERIFIED by code inspection.

## Open edges for the next phase
- Implementation beads (recommended order): (1) unpark cooldown/require-changed-reason on operator API; (2) stop re-emitting integration-exhausted past cap; (3) widen/exponentialize gate cooldown on repeat conflicts; (4) add caller identity to park/unpark audit events to identify the toggling automation.
- Human disposition for conflicted cohort hydra-xxq5/mhk2/5ob8: resolve publisher.go conflict or supersede — do NOT clear parks while conflict persists (durable-park-by-design).
- Tier B agentic conflict resolution still unimplemented.
