# Shared integration-exhaustion wedge — triage (hydra-fhhy)

Bead: hydra-fhhy (discovery/triage). Named wedges: hydra-isv5, hydra-5ob8.
Date: 2026-09-30.

## Question

Two beads (hydra-isv5, hydra-5ob8) hard-parked with `integration-exhausted` at nearly
the same time — systemic integration/concurrency wedge vs per-bead failures?
Root cause needed; hygiene repairs (clear-hard-park) would re-hit the wall.

## Findings — wedge already resolved

### hydra-isv5 — CLOSED, doc-only rebase conflict
- `br show hydra-isv5` (hydra store): CLOSED 2026-09-29, labels
  `closed-failed, integration-exhausted, integration-failed, pan-filed`.
- Cause: local merged work unpublished — PR rebase onto origin/development failed
  with add/add conflicts in `docs/discovery/integration-wedge-triage-rsyw.md` and
  `docs/hydra/handoff/task-rsyw.md` (doc files only, no code conflict).
- Resolution: doc deliverables restored on development via commit `882b0b7`
  ("docs: restore hydra-rsyw deliverables from quarantined branch"), merged through
  PR #422 (`b421992 Merge pull request #422`). The wedge cause is gone; bead closed.
- Dependency hydra-9yhq (fix bead) unblocked by the closure.

### hydra-5ob8 — IN_PROGRESS, pan-filed meta bead, proposals landed upstream
- `br show hydra-5ob8`: IN_PROGRESS, pan-filed; lists fleet-wide hard-parks across
  hydra + srv-telematics and proposes (a) re-verify the "PR already exists"
  fallback landed, (b) possible hydra code change.
- Verified: the PR-reuse fallback IS landed on development —
  `resolveOrCreatePR` / `isPRAlreadyExists` (PR #418, merge 8ad10d8; regression
  tests 37807da, hydra-uqxe). Additionally the post-merge PR state read retry
  (4 attempts, 2s×n backoff) landed via hydra-8odx for GitHub eventual consistency.
- Named trigger PR #389 (gh pr create "already exists") is MERGED
  (`gh pr view 389 -R haulmatic-dev/hydra` → `mergedAt 2026-09-29T16:39:01Z`).
- Also landed since the wedge formed: per-repo integration gate
  (`internal/core/integration_gate.go` — keyed mutex + 30s sibling cooldown),
  `publishWithRebaseRetry` (conflict → rebase → retry, cap 2), and the
  `integration_retest` stage (max_attempts=5, retry_interval=1h in
  /etc/hydra/hydra.yaml).

### Live fleet state — no remaining wedge
- Hydra bead store (`/opt/dev-tools/components/hydra/.beads`):
  **0 beads** carry `integration-exhausted` (open or closed scan = 0 remaining open).
- srv-telematics store (`/home/ajantha/projects/srv-telematics`):
  **0 open** with `integration-exhausted` (prior 145-label accumulation drained by
  earlier triages: s402, 10ka, 2vuc, uqxe).
- Events `/opt/dev-tools/var/hydra/events/2026-09-30.jsonl`: **0** exhaust /
  integration-failure storm events. Only 3 worktree_stranded, 2
  publish_rebase_attempted, 2 integration-failed (normal single-bead churn).

## Disposition

- **No code change required.** The proposed code change (PR-already-exists
  fallback) already exists on development and is deployed (PR #422 merge of
  the rsyw restoration carries it; the daemon re-verifies).
- **No park clears.** All named hard-parks already drained via prior triages or
  bead closure; nothing left to clear.
- hydra-5ob8 (meta bead) can be closed as cause-fixed once its own chain's
  verdict lands — closing it here would race its own gate; left to the chain
  owner.

## Pipeline capacity check

Recent development history shows healthy PR landings (PR #422, earlier #418,
#401, #389 merged) — no concurrency or merge-capacity starvation.

## Lessons (for `cm add`)

- Fleet-wide integration-exhausted wedges in this dogfood all traced to one of:
  PR-already-exists loop (fixed: resolveOrCreatePR), stale local integration
  base (fixed: ResolveIntegrationBase), provider/PR-state races (fixed: state
  read retry), doc-only rebase conflicts (human/ours-theirs resolution). None
  recur in 2026-09-30 events.
