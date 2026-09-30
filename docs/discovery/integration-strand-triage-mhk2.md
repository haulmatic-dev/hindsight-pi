# Integration strand triage — hydra-mhk2 (2026-09-30)

Task: bead hard-parked with `integration-exhausted` (park-since <1 min, no live
worker). Proposal: hold the durable park, let a human inspect the strand, and
decide between `repair:integrate-stranded` (conflict-free path exists) and a
manual maintainer rebase. This bead documents the strand; the park stays.

## Branch inventory (VERIFIED)

`hydra/wt-hydra-mhk2` = 3 unique commits over origin/development:

- `a15231c` docs: restore hydra-rsyw deliverables from quarantined branch
- `2e7fafb` hydra: integrate hydra/wt-hydra-rsyw
- `eaf6917` feat(publisher): pre-rebase conflict precheck — fail closed before touching the checkout

Unique content (diff `origin/development...`): 8 files, +344/−1 —
`internal/core/publisher.go` (+51 `mergeTreeConflicts` + `precheckRebaseConflicts`),
`internal/core/publisher_rebase_test.go` (+84), `scripts/cache-cap-guard.sh` (+54),
4 docs, CHANGELOG.

## Merge resistance (VERIFIED)

`git merge-tree --write-tree origin/development hydra/wt-hydra-mhk2`:
exactly TWO conflicts, both add/add, both docs-only; everything else
(including `internal/core/publisher.go`) auto-merges:

1. `docs/discovery/integration-wedge-triage-rsyw.md`
   - origin side: full 57-line landed report (last touched 29ad719, Wed Sep 30 04:16 +0530)
   - branch side: full report restored from `hydra/quarantine/hydra-rsyw` (a15231c, Sep 29 18:21 UTC), pre-lint-gate-fix content
2. `docs/hydra/handoff/task-rsyw.md`
   - origin side: terse triage-discovery handoff (252a68f, Sep 29 18:38 UTC — NEWER)
   - branch side: expanded rsyw worker handoff (a15231c, Sep 29 18:21 UTC — older but richer: gate-fix detail, verification log, cache-purge record)

## Supersession check (VERIFIED)

- `mergeTreeConflicts` / `precheckRebaseConflicts` grep on origin/development:
  NOT present (only PR-first `resolveOrCreatePR` precheck tests exist) →
  eaf6917 is a real, unlanded feature, not a superseded iteration.
- `scripts/cache-cap-guard.sh`: not on origin/development → also unlanded.
- Conclusion: NOT a superseded strand. Base has NOT diverged badly — a
  conflict-free path exists after resolving the two doc files.

## Disposition

- **HOLD the durable park.** Do NOT clear `integration-exhausted` /
  `integration-failed`; do NOT run `repair:integrate-stranded` mechanically —
  the two add/add docs still conflict, so a clean-merge gate would refuse.
- **Human resolution path** (one rebase-and-push, maintainer-only):
  1. Take the ORIGIN side of `docs/discovery/integration-wedge-triage-rsyw.md`
     (canonical landed report).
  2. Take the BRANCH side of `docs/hydra/handoff/task-rsyw.md` (superset of
     the origin terse version; add the rsyw batch-clear + gate-fix record) —
     or a union merge of both.
  3. Finish the rebase, push, land via the normal dogfood PR path. The
     precheck feature + cache-cap guard then integrate cleanly.
- Human-only per the pan-filed bead body itself: "This is a human-only
  repo-content operation; do not automate conflict resolution."

## Verification (commands run)

- `git fetch origin development` + `git merge-tree --write-tree origin/development hydra/wt-hydra-mhk2` — 2 add/add doc conflicts, rest auto-merges. PASS.
- `git diff --stat origin/development...hydra/wt-hydra-mhk2` — 8 files +344/−1. PASS.
- `git grep mergeTreeConflicts origin/development -- internal/core/` — no hits (feature unlanded). PASS.
- `br show hydra-mhk2` (/opt/dev-tools/components/hydra) — OPEN, P2, labels integration-exhausted + integration-failed + pan-filed. PASS.
- `git log`/diff of both doc conflict sides — provenance + versions recorded above. PASS.
- graphify-out/graph.json absent in cwd → rg/handoff fallback per protocol. No code change made (discovery/triage bead).
