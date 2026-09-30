# Integration-exhaustion storm duplicate — hydra-ut4z (2026-09-30)

Task: pan-filed bead "Duplicate of integration-exhaustion storm filed for
hydra-mhk2/dedupe-skipped-safe — if unsampled this should carry identical
proposal (fleet concurrency wedge; scheduled-code change suggested)".

## Verdict

DUPLICATE of hydra-mhk2's publisher-wedge finding. Closed as `not_planned:
duplicate`, `pan-filed` label removed. No code change.

## Evidence

- `br show hydra-ut4z` — pan-filed task bead filed 2026-09-30, empty body
  beyond the dedupe-skip title (Pan's filer created it while flagging the
  mhk2 finding as a duplicate). Same class as the earlier `hydra-jcj6`
  "(dedupe skip)" twin of mhk2, which was closed as duplicate in f53112e;
  persistent title dedupe in `filePanBead` (1e3bfed) should prevent recurrences.
- The fleet-concurrency proposal the duplicate would carry is already
  adjudicated:
  - `integration_gate` (per-repo keyed mutex + 30s cooldown) landed on
    origin/development — integration attempts per repo are serialized, which
    is the per-project-concurrency-1 behavior the proposal asked for.
  - `docs/discovery/integration-wedge-triage-xg6s.md` (PR #439) verified the
    mhk2/xxq5/5ob8 cluster is NOT a capacity problem: mhk2 is blocked solely
    by two docs add/add conflicts (human-only rebase per
    `integration-strand-triage-mhk2.md`); xxq5's constituent parks were all
    cause-fixed; 5ob8's triggers all resolved.
- Durable-park-by-design playbook (hindsight): mass-clearing parks without a
  cause fix re-parks; no park-clearing action taken.

## Actions taken

- Live store: `br label remove hydra-ut4z pan-filed` (✓), `br close hydra-ut4z
  -r "not_planned: duplicate of ... hydra-mhk2 ..."` (✓). Bead now CLOSED.
- No code change; this report + CHANGELOG entry are the deliverable.

## Verification (commands run)

- `br show hydra-ut4z` before/after close — IN_PROGRESS/pan-filed → CLOSED,
  label gone. PASS.
- `hindsight_search` on fleet-concurrency-wedge terms — per-project
  concurrency proposal records and durable-park judgments confirmed. PASS.
- `git fetch origin development` + landed triage xg6s read — proposal already
  adjudicated upstream. PASS.
