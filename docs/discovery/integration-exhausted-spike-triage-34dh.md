# Integration-exhausted spike triage — 2026-09-30 fleet-wide churn loop (hydra-34dh)

## Scope of evidence
- `/opt/dev-tools/var/hydra/events/2026-09-30.jsonl` (67 events, first hour 00:00–00:15Z) and `2026-09-29.jsonl` (9,063 events, 597 integration-exhausted mentions).
- Live event stream at bead start (00:12 UTC): hydra-mhk2, hydra-xxq5, hydra-5ob8 entered/exhausted integration within the same minute, matching the reported symptom.

## Root cause (verified, not hypothesis)

Not a state-machine race (hypothesis a) and not sweep-thread concurrency collapse (hypothesis b). The near-zero age offsets are explained by a **deterministic rebase conflict + an operator-API park/unpark toggle loop**:

1. **Deterministic conflict.** Every failure in the window is `integrate` stage: `pr-first close <bead> via dogfood/<id> → development: pr conflict: rebase attempt 1 failed — human review needed` — rebase of the worktree branch onto `origin/development` conflicts in `internal/core/publisher.go` (churn file: 57/597 events on 09-29, 4/6 on 09-30; plus triage/handoff docs from parallel chains). All conflict paths land in files multiple parallel worktree chains simultaneously touch. `publishWithRebaseRetry` fail-closes after 2 attempts — correct behavior, the parks themselves are sound.

2. **The loop.** Immediately after each `integration-exhausted` event, the project `campaign` is parked and unparked via the operator API within the same second:
   - `00:05:16.972 project_parked {"reason":"operator hold"}` / `00:05:16.973 project_unparked {"reason":"resolved"}`
   - `00:06:43.393` parked/unparked pair; `00:07:50.166` pair; pattern repeats every ~60–90s.
   Each unpark re-enables integration evaluation for the parked cohort → next sweep retries `pr-first close` → same rebase conflict → `integration-failed` → counter increments → `integration-exhausted` re-fires minutes later. Observed counters climbing while parked: hydra-xxq5 20→21→22 in 3 minutes (17 exhaustion events total on 09-29); hydra-mhk2 12→13.

3. **Gate bypassed by timing.** `integration_gate.go` serializes per-repo and applies a 30s cooldown after a gated failure. The park/unpark cycle period (~60–90s) exceeds the cooldown, so the gate never suppresses the re-attempt. Per-repo gating cannot fix this: the re-trigger is an operator (or automation impersonating one) toggling park state, not sibling contention.

4. **Counter uncapped post-park.** `consecutive_failures_capped` events keep firing past the cap (22, 21, 20…): every retry that occurs after a park-clear increments the same counter and re-emits the exhausted event, so each unpark manufactures a fresh "spike".

## Entropy injection points (task's requested audit)
- **Operator park/unpark API** (`POST /api/v1/projects/:name/park|unpark`, FR-033): currently no rate limit, no cooldown after unpark, no requirement that the named cause changed between park and unpark ("resolved" unpark 1ms after "operator hold" park is contradictory and passes). Primary injection point.
- **Rebase retry cap + fail-close** (`publishWithRebaseRetry`, cap 2): correct, produces the durable park; not a defect.
- **`integration_gate` cooldown (30s)**: shorter than any human/automation park cycle; provides no back-pressure against unpark-triggered retries.
- **Consecutive-failure counter**: never reset on park, and re-fires the exhausted event on each capped retry — turns one root cause into N visible spikes.

## Tier B / agentic resolution note
Tier B agentic conflict resolution remains unimplemented; the publisher.go conflict on these branches needs human merge strategy. The 09-29 storm triage (hydra-uqxe) already established the PR-already-exists mode is fixed; today's mode is pure rebase-conflict churn.

## Recommendations (in priority order)
1. **Unpark cooldown/back-pressure**: after `project_unparked`, suppress integration retries on that project for a configurable window (suggest ≥10m), and require the unpark reason to differ from the last park reason. Kills the loop.
2. **Counter sanity**: stop incrementing/re-emitting `integration-exhausted` once the label is present — the first event is the signal, later ones are noise.
3. **Widen integration_gate cooldown** or make it exponential on repeat conflicts of the same paths.
4. **Identify the toggle source**: the operator-API park/unpark pairs originate off-box (audit events carry no caller identity — add caller field to the audit event; FR-033 emits typed events but omits the client identity).
5. Human disposition for the conflicted cohort (hydra-xxq5, hydra-mhk2, hydra-5ob8): resolve the publisher.go conflict manually or supersede — clearing parks without resolving the conflict reproduces the park (durable-park-by-design rule).

## Disposition
This is a triage/discovery bead: no code change made. The parks are individually correct; the systemic defect is the unpark→retry→conflict→park loop plus uncapped counter noise. Recommendations 1–4 are small, separate implementation beads.
