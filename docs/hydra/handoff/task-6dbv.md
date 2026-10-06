# discovery handoff (hydra-6dbv)

## What changed (files + one line each)
- docs/discovery/orphanclaim-triage-6dbv.md: triage — all three P0 claims verified stale (orphans, publisher wedge, disk 99%); one real wedged bead named for human review (hydra-95ia churn loop); duplicate twins closed; no code change.
- CHANGELOG.md: Documentation entry under Unreleased pointing at the triage.

## Verification (commands run + pass/fail)
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: 4 `working` rows (n9nq, rhin, cbww, 6dbv) heartbeats <1 min; all other in_progress beads have heartbeat-fresh `completed` rows (<5m grace) except hydra-95ia (completed 12:57, 43m stale — the only live orphan candidate). 0 live orphans. PASS.
- events/2026-10-06.jsonl: 48 `charon_orphan_reopened` (dead_worker), 58 `orphan_escalated_wedged` (q207/rxi6 correctly human-gated), **0 "P0 campaign anomaly" filings** today. Recovery chain live; deployed binary built 12:16 from origin/development tip. PASS.
- Publisher wedge: local checkout HEAD 9993250 on development — `git rev-list --count origin/development..HEAD` = 0 (nothing unpublished), 6 behind; no open publish-head PR on haulmatic-devteams/hydra. Wedge absent. PASS.
- Disk: `df -h /` → 213G/451G = **50%** (watermark 85%) — 99% claim stale. `systemd-tmpfiles --clean` NOT RUN (sudo requires tty password); unnecessary at 50%. PARTIAL.
- Live-store actions: triage comment added to hydra-95ia; `br close` hydra-wwj2 + hydra-gzkr not_planned as duplicate filings (✓ ×2). `br doctor migrate-schema recover` hit WAL poison once ("database is busy (recovery in progress)") but subsequent br commands succeeded.
- hindsight_search at bead start — not connected (skip rule applied). graphify-out/graph.json absent → registry/event/handoff fallback per protocol. PASS.

## Open edges for the next phase
- hydra-95ia needs human/Pan disposition: one fair retry (gates now pass) or abandon-with-summary if the gate-runner-fault loop recurs.
- systemd-tmpfiles hygiene cleanup could not run non-interactively; harmless at 50% usage.
