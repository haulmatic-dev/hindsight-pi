# Integration Wedge Triage — hydra-28yq (mhk2 / xxq5 / 5ob8 simultaneous hard-parks)

Date: 2026-09-30. Bead: hydra-28yq (discovery/triage).

## Claim investigated

Three beads (hydra-mhk2, hydra-xxq5, hydra-5ob8) hard-parked with
`integration-exhausted` minutes apart (02:03:48, 02:05:14, 02:15:34 UTC on
2026-09-30 per events/2026-09-30.jsonl) — suspected fleet-wide integration
wedge rather than independent causes.

## Root cause (VERIFIED)

Not a systemic pipeline wedge. All three events are per-branch rebase
conflicts, and all three branches were attempting to land the SAME already-
landed change — the `isPRAlreadyExists` dedupe / existing-PR reuse fix:

- hydra-mhk2 — conflict in docs rsyw add/add (precheck commit eaf6917 already
  landed via PR #443, merge e0a3bce; labels cleared 2026-09-30 01:49 UTC by
  hydra-fetm triage — this bead is already resolved).
- hydra-xxq5 — conflict in internal/core/publisher.go applying d1d48b1
  "fix(publisher): dedupe isPRAlreadyExists; fail-closed resolver contract".
- hydra-5ob8 — conflict in internal/core/publisher.go applying b6d3e49
  "fix: dedupe isPRAlreadyExists (lint gate)".

The fix each branch carries is already on origin/development:

- `e0c70a6 fix(publisher): reuse existing PR via URL embedded in gh error;
  dedupe isPRAlreadyExists (hydra-6zwl)` (PR #418)
- `ecc6cb5 fix(publisher): restore fail-closed existing-PR resolver`
- `29ad719 PR-first close must be idempotent for existing PRs`
- `e0a3bce feat(publisher): pre-rebase conflict precheck — fail closed before
  touching the checkout (#443)`

`resolveOrCreatePR` / `isPRAlreadyExists` present on origin/development
publisher.go (7 hits). The timing cluster is explained by the Pan/filed
duplication loop filing near-identical fix beads that then raced each other
through the integration gate — the adaptive per-repo cooldown
(7d464c3, PR #444) and fast-path absorption (f2dca86) now landed address the
contention, and the rebase precheck (e0a3bce) fail-closes before these
stale branches can clobber the checkout.

## Disposition per bead

- **hydra-mhk2**: RESOLVED. Labels cleared; unique precheck landed (PR #443).
  Nothing to do.
- **hydra-xxq5**: SUPERSEDED pan-filed meta bead. Content landed via
  e0c70a6/ecc6cb5. Closed `not_planned: superseded`.
- **hydra-5ob8**: SUPERSEDED pan-filed meta bead. Content landed. Closed
  `not_planned: superseded`.

## Re-running all three together?

CONTRAINDICATED. Playbook rule: verify per-bead merge-tree before clearing
parks. All three branches conflict with newer landed code (publisher.go
content conflicts; rebase would revert the landed resolver + precheck).
Abandon-with-summary is the correct exit, not a grouped retry.

## Remediation already landed (no code change required from this bead)

- PR-reuse / PR-already-exists fallback: e0c70a6, ecc6cb5, 29ad719.
- Pre-rebase conflict precheck (fail closed): e0a3bce (PR #443).
- Adaptive per-repo integration gate cooldown: 7d464c3 (PR #444).
- Retest fast-path absorption: f2dca86.
- Pan pan-filed dedupe (open-title check): landed (green handoff 440 chain).

## Residual open edge

- Tier B agentic conflict resolution remains unimplemented (separate bead).
- Stale `gate-failed-lint` labels on xxq5/5ob8 referenced the historic
  isPRAlreadyExists redeclaration — moot once beads closed.
