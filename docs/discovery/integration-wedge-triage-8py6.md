# Integration-exhausted wedge triage — hydra-8py6 (hydra-242b, hydra-ycmu, srv-telematics cluster)

Date: 2026-09-29. Discovery bead; parks intentionally left standing.

## Scope

Task named: hydra-242b, hydra-ycmu (hydra store) and ajantha-srv-telematics-{i6zt,5l11,04jq,f10l,burr,nygw,kher,5gq} (srv-telematics store at `/home/ajantha/projects/srv-telematics`).

## Verified findings

### hydra store (hydra-242b, hydra-ycmu)
- `br show hydra-242b`: OPEN, labels `integration-exhausted, integration-failed, pan-filed, stranded-work, triage-superseded`; triage comment 2026-09-29 (bead ng18) already documents supersession by landed escalation_ttl (2ee7449) and closing not-planned per d778/014y precedent. Durable label intentionally kept. **No new action needed.**
- `br show hydra-ycmu`: OPEN, labels `integration-exhausted, integration-failed, pan-filed`; body = pan-filed duplicate-class finding for the same stale-escalation wedge already fixed by escalation_ttl scanner + Pan `clear-escalation` repair (both landed on origin/development). Its feature content is done; the integration-exhausted label is a residual marker from the wedge window. **Disposition: abandon-with-summary; park stands.**

### srv-telematics store (the real accumulation)
Direct SQLite read (br CLI intermittently blocked by WAL-index poison — see below):
- **145 issues carry `integration-exhausted`**; 62 of them are already `closed` — the label survives close and permanently inflates wedge counts.
- **91 `hydra/quarantine/*` branches** exist and are never drained (`worktree.quarantine_auto_drain: false`, default). Each quarantined strand parks its bead with `integration-exhausted` + `stranded-conflict`.
- Per-bead inspection: 5gq (stranded-conflict quarantine of pmgh, human-only merge), 04jq (same pattern), i6zt (quarantine of 5gq — quarantine-of-quarantine chaining), burr/nygw/f10l (pan-filed wedge trackers), kher/5l11 (fix/duplicate-class filings).
- Root cause: this project's integration path consistently lands on merge conflicts between stranded branches and the working branch → `integrate-stranded` quarantines → human-only resolution → nothing drains → every new strand adds another parked bead within minutes of the previous one. **Shared wedge, not per-bead failure.** Parks should stand per task guidance.

### Store health (infra edge, not integration logic)
- Recurring WAL-index poison: `Database error: database is busy (recovery in progress)` and `automatic WAL index recovery already failed ... at stage private-recovery`. `br doctor migrate-schema recover` cleared it twice during this session; a third recovery failed at live-recovery with the store busy (daemon concurrency). Same poison hit by prior triage beads (bmtb, s402, ng18). It gates every pan/health/filer scan on this store and likely amplified the mass-pan-filing that produced the wedge bead cluster.

## Code-change proposal (hydra integration handling)

No change to park semantics — durable parks + triage is the correct design and `integration_retest` already covers time-boxed retries. Two targeted hygiene fixes are warranted:

1. **Clear quarantine labels on terminal close** — when a bead closes (any reason), drop `integration-exhausted`, `integration-failed`, `stranded-conflict`, `stranded-work`, `integration-unverified`, `worktree-unavailable` from it (same set the operator quarantine-clear endpoint intersects against). 62/145 live labels in srv-telematics are ghosts on closed beads; today they keep pan fix-filers filing wedge beads forever.
2. **Quarantine branch retention cap** — 91 `hydra/quarantine/*` branches will grow unbounded. Either enable `worktree.quarantine_auto_drain` for repos with a quarantine backlog > N after age > T, or prune quarantine branches when their owning bead closes as superseded/abandoned.

3. **Treat `gh pr create` "a pull request for branch ... already exists" as PR-reuse, not an integration failure** — this proven playbook rule covers the d778-class park (PR #316 existed; the publisher retried `gh pr create` 4× and exhausted). On this error the publisher should locate the existing PR by head and proceed with merge/verification instead of counting a failure.

Explicitly rejected again: Tier B agentic conflict resolution (deliberate human-only boundary, per integration-triage-iojl and integration-wedge-triage-10ka).

## Disposition

- hydra-242b: already triaged (ng18); park stands.
- hydra-ycmu: abandon-with-summary; park stands; wedge it tracked is fixed upstream.
- srv-telematics cluster: parks stand; human must resolve/drop the 91 quarantined strands. Do NOT clear-hard-park. Apply code proposal items 1–2 as separate implement beads.
- Store: operator should run `br doctor migrate-schema recover` on the srv-telematics store during a daemon-quiet window.
