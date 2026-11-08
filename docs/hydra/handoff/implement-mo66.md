# implement handoff

## What changed (files + one line each)
- docs/operations/host-setup.md: added `validation.agent_verifier` row to the config-knob table (next to `output.pr_auto_merge_repos`) documenting enabled (default false), min_score (0.8), classes (implement/feature/refactor), max_diff_bytes (32KB), timeout_seconds (300), provider/model/pi_path; points at shepherd.md §1a (bounded veto, fail-open, churn tripwire).

## Verification (commands run + pass/fail)
- `grep -c agent_verifier docs/operations/host-setup.md` → 1. PASS.
- Commit verified via `git show --stat` (1 file, +1).

## Open edges for the next phase
- None; doc-only change. Defaults cross-checked against `AgentVerifierConfig` in internal/core/config.go.
