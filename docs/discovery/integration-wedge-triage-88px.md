# Shared wedge triage — 6 beads hard-parked integration-exhausted within minutes (task 88px)

Date: 2026-09-29. Beads: hydra-xxq5, hydra-rsyw, hydra-6zwl, hydra-uqxe, hydra-242b (hydra store),
ajantha-srv-telematics-burr, ajantha-srv-telematics-otae (srv-telematics store).

## Root cause — two stacked causes, both identified and repaired

### 1. br WAL-index poison (primary for the burst timing)
The srv-telematics beads database was poisoned at stage `private-recovery`
("engine admission recovery changed protected database component \"-wal\""), with
recovery pre-states timestamped 2026-09-29T18:07 — inside the same burst window as the
mass `integration-failed` / `integration-exhausted` events (17:59–18:05). Store errors
blocked bead-state updates during integration, amplifying per-bead failures into a
simultaneous storm. This matches the documented playbook pattern (see
docs/discovery/integration-wedge-triage-501r.md).

**Repair applied (VERIFIED):** `br doctor migrate-schema recover` run 6 times on
/home/ajantha/projects/srv-telematics until recovery completed. Store now responds
(`br show` returns both beads). This matches the playbook note that recovery is often
re-poisoned by a contending writer on the first pass(es).

The hydra store (/opt/dev-tools/components/hydra, /opt/dev-tools/var/hydra) reads fine.

### 2. Concurrent duplicate implementations of the same fix (primary for the conflicts)
Every integration failure in the burst is a rebase/merge conflict on the SAME files:
- `internal/core/publisher.go` + `publisher_test.go` — four beads (xxq5, uqxe, 6zwl,
  15zh) each independently implemented "reuse existing PR instead of failing"
  (commits d1d48b1, da22ca0, 6e04a2a, e0c70a6 — the isPRAlreadyExists dedupe). Each
  branch conflicts with the others; each rebase attempt hits the newest landed
  sibling, exhausts `MaxPRRebaseAttempts = 2`, hard-parks.
- `scripts/cache-cap-guard.sh` add/add — rsyw's disk-watermark guard duplicated by a
  landed sibling.
- `docs/hydra/handoff/task-e3hn.md` add/add — otae (fix bead for e3hn) vs landed e3hn.

This is duplicate-concurrent-work, not integration capacity: origin/development on
both repos shows fresh PR merges (#375 srv-telematics; hydra pipeline landings
throughout the window). The integration path itself is healthy.

## Dispositions

| Bead | Status | Disposition |
|---|---|---|
| hydra-242b | triage-superseded / stranded-work | Already dispositioned: abandon-with-summary (superseded by landed escalation_ttl 2ee7449). No change. |
| hydra-xxq5 | integration-exhausted, gate-failed (its own lint failed: `isPRAlreadyExists redeclared`) | Duplicate of landed PR-reuse fix. Close as superseded duplicate; do NOT clear park or rebase. |
| hydra-6zwl | integration-exhausted (publisher wedge + disk 99% report) | PR-reuse reuse already landed (PR #389 merge path + publisher dedupe); disk now 78% (verified `df -h /`). Close as superseded. |
| hydra-uqxe | integration-exhausted | Duplicate PR-reuse implementation. Close as superseded. |
| hydra-rsyw | integration-exhausted | cache-cap-guard superseded by landed sibling. Close as superseded. |
| ajantha-…-burr | closed-failed, integration-exhausted | Sibling of the same cluster; store now readable. Close as superseded. |
| ajantha-…-otae | fix bead for e3hn, fix-recursion-capped | Its doc-only conflict (handoff add/add) is the whole wedge. Close as superseded by e3hn triage. |

Parks stay durable-by-design; the cause (WAL poison + duplicate work) is now
repaired/identified, so operators can batch-close these as superseded — do NOT
clear-hard-park and retry, the branches conflict with landed code.

## Circuit-breaker code change? — NO

No hydra code change is warranted:
- Rebase retry (`publishWithRebaseRetry`, MaxPRRebaseAttempts=2) and per-repo
  integration gate already landed.
- PR-already-exists reuse already landed.
- The real gap is upstream: the planner/pan keeps filing duplicate beads for the same
  wedge (xxq5, uqxe, 6zwl, rsyw are all pan-filed variants of one finding). The
  semantic clustering dedupe (fix_semantic_clustered) addresses fix beads; the
  pan-filed cap (MaxOpenFiledBeadsPerProject=2) addresses filing. Remaining lever is
  operator-side dedupe of the six beads, recorded above.
- WAL-poison recovery is an operational runbook item (`br doctor migrate-schema
  recover`, repeat until clean), already documented.

## Verification evidence
- `grep CONFLICT /opt/dev-tools/var/hydra/events/2026-09-29.jsonl | sort | uniq -c` —
  all burst failures conflict on publisher.go / cache-cap-guard.sh / task-e3hn.md.
- `br doctor migrate-schema recover` ×6 on srv-telematics — final runs completed
  ("Run `br doctor migrate-schema plan`..." success tail); `br show` reads both beads.
- `df -h /` — 78% used, below 85% watermark (rsyw's disk premise no longer true).
- `git log origin/development` (srv-telematics) — 2036ffd2 Merge PR #375, pipeline live.
