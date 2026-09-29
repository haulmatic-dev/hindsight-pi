# Roadmap: CASS Knowledge Federation (curated cross-pollination)

**Status:** Planning — not scheduled for implementation
**Created:** 2026-09-28
**Owner:** TBD

## Problem

CASS institutional memory is **local per user**; the fleet is **global**.
Measured 2026-09-28:

| Store | Rules | Consumers |
|---|---|---|
| `/root/.cass-memory/playbook.yaml` | ~2,552 | operator (root) pi sessions only |
| `/opt/dev-tools/var/hydra/.cass-memory/playbook.yaml` | **13** | daemon → injected into every worker prompt tail |
| `/home/<user>/.cass-memory/` | small/none | that user's interactive sessions |

Lessons distilled during operator sessions (`cm add`) never reach the workers
that need them; lessons workers save stay in the daemon's store, invisible to
operators. The two stores drift independently.

## Why not the obvious fixes

- **Symlink one shared playbook:** rejected. (1) Write races — workers are
  prompted to `cm add` after fixes, so N workers + operators would write one
  YAML concurrently. (2) Relevance bleed — operator-only rules (deploy
  procedures, token-file paths, drain sequences) score high and would inject
  into worker prompt tails, burning tokens and leaking ops-sensitive detail
  into prompts that get retained to hindsight and logs. (3) Feedback mixing —
  `cm mark helpful/harmful` is context-dependent; a rule vital for operators
  but noisy for workers would be downvoted away globally.
- **Mirror CASS into hindsight:** rejected. Duplicated rules drift (CASS rules
  get deprecated/updated; hindsight copies go stale) and produce conflicting
  recall. Hindsight stays episodic/semantic; CASS stays the curated rulebook.
  Workers already receive CASS via prompt injection and hindsight via the
  DISCOVERY PROTOCOL `hindsight_search` mandate (100% compliance measured
  2026-09-28) — coverage exists on both sides; only CASS *locality* is broken.

## Proposal: one-way curated sync with a reverse review lane

```
root playbook (~2,552 rules)
   │  periodic sync job: keep rule if
   │    finalScore >= threshold  AND
   │    category in {architecture, testing, performance, verification}  AND
   │    content matches no sensitive-pattern denylist (paths to token
   │    files, hostnames, drain procedures, credentials layout)
   ▼
hydra playbook (single writer: the sync job)  → worker prompt injection
   │
   │  reverse lane: rules workers add locally are exported to a
   │  review queue (weekly report or PR), promoted into root's
   │  playbook by a human/operator session — never auto-merged
   ▼
root playbook
```

Properties: exactly one writer per store (no races); denylist + category
allowlist blocks bleed; feedback scoring stays context-local; worker-derived
knowledge still flows upward through review.

## Open questions

- Sync trigger: cron (simple) vs daemon tick (observable in fleet telemetry)?
- Score threshold and denylist contents — needs a first pass over root's
  playbook to size what would flow (many operator rules are deploy/runbook
  shaped and must NOT ship to workers).
- Do other users' playbooks (ajantha, nipuna.j) join the federation or stay
  private? Likely: opt-in read from the shared curated set, private writes.
- Interaction with `cm` upstream: if cm gains multi-store or remote-sync
  support, prefer it over a local script.

## Related measurements (2026-09-28, for context)

- Layer-1 correlation of hindsight recall vs bead outcomes was inconclusive
  (confounded: hard beads both churn more and recall more). The longitudinal
  measure (daily bank-stats snapshot vs fix-rate/respawn-churn trend) is the
  intended instrument — separate small item, not part of this roadmap.
