# Changelog

All notable changes to Hydra are documented here. Every PR (human-, Claude-, or
Hydra-authored) MUST add an entry under **Unreleased** — see AGENTS.md
"Documentation & Changelog Discipline". Format follows
[Keep a Changelog](https://keepachangelog.com/); newest first within a section.

## [Unreleased]

### Added
- Target-Files scope gate on verified close: worker changes outside a bead's
  declared Target Files are warned about or rejected
  (`validation.enforce_target_scope: off|warn|block`, default `warn`).
  *Authored by a Hydra dogfood run (PR #51).*
- Worktree integration reliability: singleton beads integrate their worktree
  branch on verified close; an hourly reconciler sweeps orphaned `hydra/wt-*`
  branches back into the working branch before worktree GC.
  *Authored by a Hydra dogfood run.*
- Learning loop: retry prompts carry the previous attempt's failure summary
  (Reflexion, in-process and across respawns via `HYDRA_PREV_FAILURE`), and
  workers deterministically inject the top-3 CASS playbook rules into every
  prompt (`cm context --json`, fail-open).
- Stale-worktree GC: Charon removes per-bead worktrees older than 24h
  (skipping active workers); validated live at 300 → 18.
- Per-bead worker logs under `$HYDRA_HOME/logs/workers/`; only notable lines
  (errors, 401/429, timeouts) surface to the daemon log (PR #50).
- Leveled logging via `internal/logx` (`HYDRA_LOG_LEVEL`), DEBUG/INFO to
  stdout, WARN/ERROR to stderr (PR #50).
- `eval/extract.sh`: extracts a scope-clean patch of a dogfood run's declared
  Target Files from the target clone.

### Fixed
- Sticky compliance approval: an approved PRD no longer re-runs the analyzer
  every poll until max_cycles (`.hydra/compliance-approved` sentinel,
  restart-durable).
- Hindsight recall: the live recall API ignores `limit`/`min_score` and sends
  no scores — results are now truncated client-side to the server-ranked
  top-k (previously ~65 unranked memories were injected into every worker
  prompt) and the phantom `score: 0.00` display is gone.
- Worker Pi startup: `--offline` flag + jittered startup retries for the
  concurrent provider-list fetch flake; activity timeout lowered 30m → 10m
  (tunable via `HYDRA_WORKER_ACTIVITY_TIMEOUT`) so hung workers recycle
  quickly (PR #49).
- Worker skills are stack-scoped to the project language (a Go project loads
  8 skills instead of 16) (PR #49).
- Worktrees are rooted under `$HYDRA_HOME` (daemon-owned) instead of the
  project's parent directory, which was not writable for per-user projects
  (PR #49).
- Multi-user discovery no longer skips an entire user when their git token is
  unreadable; `br` runs in the repo subdirectory for multi-repo projects; one
  project's `br`/`bv` failure no longer aborts the whole scheduler tick
  (PR #47).

### Documentation
- `docs/hydra-analysis-20260712iv.md`: improvement master plan (phases,
  work orders, eval harness definition).
- Dogfood PRDs: sticky approval, Target-Files scope gate, worktree
  integration reliability.
