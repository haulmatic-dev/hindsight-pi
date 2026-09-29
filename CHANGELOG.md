# Changelog

All notable changes to Hydra are documented here. Every PR (human-, Claude-, or
Hydra-authored) MUST add an entry under **Unreleased** — see AGENTS.md
"Documentation & Changelog Discipline". Format follows
[Keep a Changelog](https://keepachangelog.com/); newest first within a section.

## [Unreleased]

### Fixed
- Park no longer blocks Charon orphan recovery (live incident 2026-09-29 10:20 UTC): recoverOrphanInProgressBeads skipped all orphan handling for parked projects, so their dead workers' `running` tracker rows were never reaped — the daemon tracked 6/6 phantom worker slots with 0 live processes and the whole fleet stalled ("All worker slots filled (6/6), skipping bead discovery") until restart. Park's contract is stop-NEW-spawns; the scheduler already consults ProjectParkInfo on every spawn path, so recovery (stale-row reap + orphan reopen) now runs for parked projects too. Test: `TestRecoverOrphanInProgressBeads_ParkedCampaignStillRecovers`.

- Disposition note (docs): integration-exhausted hard-parks **hydra-ycmu** (duplicate closure for the identical root cause surfaced under hydra-242b — treat both heads as one manual-integration review) resolved via proven-rules decision tree.
�