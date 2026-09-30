# discovery handoff

## What changed (files + one line each)
- docs/discovery/integration-wedge-triage-28yq.md: triage of the mhk2/xxq5/5ob8 simultaneous integration-exhausted hard-parks (02:03–02:15 UTC 2026-09-30) — root cause: three near-duplicate branches all re-landing the already-landed isPRAlreadyExists dedupe/existing-PR reuse fix, each hitting rebase conflicts vs newer landed code; not a systemic pipeline wedge. mhk2 already resolved (PR #443); xxq5/5ob8 closed as superseded (live-store). No code change.
- CHANGELOG.md: Unreleased/Documentation entry pointing at the triage.

## Verification (commands run + pass/fail)
- Events /opt/dev-tools/var/hydra/events/2026-09-30.jsonl — all three exhaustion events are rebase conflicts: mhk2 (rsyw docs add/add), xxq5 (publisher.go applying d1d48b1 isPRAlreadyExists dedupe), 5ob8 (publisher.go applying b6d3e49 same fix). PASS.
- git grep resolveOrCreatePR/isPRAlreadyExists origin/development — publisher.go hits; landed commits e0c70a6 (PR #418), ecc6cb5, 29ad719, e0a3bce (PR #443), 7d464c3 (PR #444 adaptive gate), f2dca86 (fast path). PASS.
- br show hydra-mhk2 — labels cleared 2026-09-30 01:49 UTC (hydra-fetm), resolved. PASS.
- br comments add + br close hydra-xxq5, hydra-5ob8 — both ok, closed "not_planned: superseded". PASS.
- hindsight_search on shared-wedge terms — durable-park-by-design + per-bead merge-tree-before-clear playbook confirmed. PASS.

## Open edges for the next phase
- Tier B agentic conflict resolution unimplemented (separate bead).
- Root churn: pan-filed duplicate fix beads racing each other — pan dedupe landed, monitor for recurrence.
