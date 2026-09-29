# Shared wedge triage: fleet-wide integration-exhausted hard-parks (hydra-4axh)

Task proposal: detect existing PRs before create + sequential rebase-integrate
with a project-level integration concurrency circuit breaker.

## Disposition: NO NEW CODE CHANGE — both proposed remedies are already landed and verified effective post-deploy

### Remedy 1: detect existing PRs before create — LANDED
- `internal/core/publisher.go`: `isPRAlreadyExists` (line ~300) classifies the
  benign duplicate-PR race; `resolveOrCreatePR` (line ~659) falls back to
  resolving the pre-existing PR when `gh pr create` reports "already exists",
  and both PR publish paths use it (lines ~330, ~419).
- `internal/core/bead_closer.go`: PR-first close treats PR-already-exists as
  landed, not an integration failure (bwyn handoff, landed).
- `internal/core/publish_path.go`: `maybeOpenAutoPR` close path is idempotent
  (lists open PRs by head before create).

### Remedy 2: sequential rebase-integrate with project-level circuit breaker — LANDED
- `internal/core/integration_gate.go`: per-repo keyed mutex serializes all
  integrate/PR attempts per repo; after a gated failure sibling beads wait a
  bounded repo cooldown (fail-closed but bounded; success clears it). Motivation
  comment cites this exact telematics wedge (parallel CHANGELOG.md conflicts).
- Wired at: singleton close-path integration (`bead_closer.go` ~316) and
  reconciler `IntegrateBranchAndRemove`.
- `internal/core/publisher.go`: `publishWithRebaseRetry` (FR-011, cap 2) covers
  redundant rebase conflicts.

### Deployment verification
- Daemon `/opt/dev-tools/var/hydra/bin/hydra -config /etc/hydra/hydra.yaml`
  restarted 2026-09-29T16:57 (binary mtime 16:56:58) — fixes deployed.
- Events `/opt/dev-tools/var/hydra/events/2026-09-29.jsonl`: 434
  integration-exhausted mentions on 09-29 but **zero** `integration_exhausted`
  events after 17:00 (post-restart). Failures up to 16:50 (8dvj, oive, 5l11,
  publish-hydra PR #394 collision) were the pre-restart binary, which predated
  the PR-reuse/gate fixes.

### Residual parks
Existing integration-exhausted hard-parks predate the deploy and remain durable
by design. Disposition per prior triages (10ka, 501r, iojl): per-bead
supersession/abandon-with-summary or batch unpark via operator API
(`POST /api/v1/projects/:name/unpark`) after cause verification — not a code
change.

## Open edges
- Tier B agentic conflict resolution unimplemented (separate bead).
- If srv-telematics parks recur post-deploy, re-triage with fresh events.
