# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-triage-pmed.md: triage of the hydra-pmed integration-exhausted hard-park — cause, evidence, disposition.
- CHANGELOG.md: Documentation entry under Unreleased.
- No code change (triage-only bead).

## Verification (commands run + pass/fail)
- `br show hydra-pmed` (/opt/dev-tools/components/hydra) — IN_PROGRESS, labels integration-exhausted/integration-failed/pan-filed, push-pending body citing publisher.go rebase conflict at d6c5752. PASS.
- Events `/opt/dev-tools/var/hydra/events/2026-09-29.jsonl` — `integration-exhausted` on hydra-pmed 17:54:49Z, 5 consecutive failures, last "rebase attempt 1 failed", conflict_paths=[internal/core/publisher.go]. PASS.
- `git merge-tree --write-tree origin/development hydra/wt-hydra-pmed` — exit 0, CLEAN. Branch now merges; only unique commit 59a95c5 (docs). PASS.
- `git merge-base --is-ancestor 69a6cb8 origin/development` — ancestor; upstream resolution ecc6cb5 landed. origin/development tip dcaa38c (PR #397), reflog fast-forward-only, no rewind. PASS.
- Shared-wedge framing (pmed/242b/srv-telematics-burr): causes are per-bead (superseded feature / push-path blockage / transient publisher collision), not one common change. VERIFIED via prior triages (task-242b, task-501r handoffs).

## Open edges for the next phase
- Recommended repair in live store: `repair:clear-hard-park` + `repair:reset-retries` on hydra-pmed (cause resolved, clean merge-tree, docs-only branch). NOT applied by this bead — human/Pan decision.
- Stale cherry-pick state in local main checkout (CHERRY_PICK_HEAD 9a905d3, "integrate hydra/wt-hydra-uewg") — separate repo-state issue, out of scope here.
