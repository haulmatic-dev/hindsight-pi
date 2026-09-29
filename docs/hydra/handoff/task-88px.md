# DISCOVERY handoff

## What changed (files + one line each)
- docs/discovery/integration-wedge-triage-88px.md: triage of the 6-bead integration-exhausted burst — root causes (br WAL poison + concurrent duplicate PR-reuse implementations), per-bead dispositions, verification evidence.
- CHANGELOG.md: Documentation entry under Unreleased.
- No code change (discovery bead; evidence shows no circuit-breaker change is needed — rebase retry, integration gate, PR-reuse reuse already landed).

## Verification (commands run + pass/fail)
- `grep CONFLICT /opt/dev-tools/var/hydra/events/2026-09-29.jsonl | sort | uniq -c` — burst (17:59–18:05) conflicts all on publisher.go / publisher_test.go / cache-cap-guard.sh / docs/hydra/handoff/task-e3hn.md → duplicate concurrent work, not capacity. PASS.
- `br doctor migrate-schema recover` ×6 on /home/ajantha/projects/srv-telematics — first passes failed (`engine admission recovery changed protected database component "-wal"`), final passes completed; `br show ajantha-srv-telematics-{burr,otae}` now reads both beads. PASS.
- `df -h /` — 78% used (below 85% watermark; rsyw's disk premise stale). PASS.
- `git log origin/development` (srv-telematics) — 2036ffd2 Merge PR #375, pipeline live. PASS.
- Store reads: hydra-242b triage-superseded (already dispositioned); xxq5/rsyw/6zwl/uqxe carry integration-exhausted + pan-filed (duplicates of one finding). PASS.

## Open edges for the next phase
- Operator/Pan: close hydra-xxq5, hydra-rsyw, hydra-6zwl, hydra-uqxe, ajantha-srv-telematics-burr, ajantha-srv-telematics-otae as superseded duplicates; do NOT clear parks or rebase (branches conflict with landed code).
- Duplicate-class filing remains the open gap: 4 beads implemented the same PR-reuse fix concurrently. Semantic clustering covers fix beads; pan-filed dedupe across sibling projects (hydra vs srv-telematics stores) is unaddressed.
- srv-telematics WAL poison recurs under daemon write bursts — watch for recurrence; recovery runbook is repeat-until-clean.
