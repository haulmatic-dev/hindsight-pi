# Changelog

All notable changes to Hydra are documented here. Every PR (human-, Claude-, or
Hydra-authored) MUST add an entry under **Unreleased** — see AGENTS.md
"Documentation & Changelog Discipline". Format follows
[Keep a Changelog](https://keepachangelog.com/); newest first within a section.

## [Unreleased]

### Added
- `.claude/skills/create-prd`: symlinked to the canonical `create-prd` skill in
  `dev-tools/config/pi/skills/create-prd` (pi-agent-config repo), so Claude
  Code and the pi agent share one PRD-authoring skill instead of two drifting
  copies. See pi-agent-config CHANGELOG for the doctrine content.

### Changed
- Refactored `PiConfig.ModelForPhase` to remove the duplicated primary provider/model return path and simplify the override logic. *Authored by a Hydra dogfood run.*
- Further optimized `tools/hydra-eval`: artifact-parsing now uses a single shared `forEachLine` helper, marker/payload extraction is generalized via `payloadAfter`, and `main` parses the daemon log once with `ParseDaemonLog` instead of four separate passes. *Authored by a Hydra dogfood run.*
- Refactored `tools/hydra-eval/parse.go`: artifact-parsing loops are now driven by a shared `scanLines` helper, marker/payload operations use `bytes` instead of `strings` to avoid per-line allocations, and the three public parsers (`ParseCloseEvents`, `ParseBeadsJSONL`, `CountMarker`) share a single scanner setup. *Authored by a Hydra dogfood run.*
- Refactored the verified-close target-less warning: `changedFiles` is now
  computed once for both the diff gate and the scope gate (removing one
  `git status` call), the target-set construction is shared via
  `buildTargetSets`, and the warning logic is extracted into
  `targetlessWarningPaths` for better readability and testability.

### Added
- Multi-user onboarding tooling (`packaging/`): `hydra-onboard <user>` grants
  the daemon durable, minimal access to a user's `~/.hydra` (traverse-only ACL
  on the home dir + read + **default** ACL on `.hydra` so it survives config
  rewrites); `hydra-onboard-sweep` + a systemd timer apply it across all homes
  every 15 min so new users are picked up hands-free. Fixes the recurring
  home-dir-traverse permission gap that silently skipped users (e.g. a real
  user blocked from 2026-07-06 until 2026-07-20).
- Multi-user discovery diagnostics: a config **confirmed present but
  unreadable** (stat succeeds, read fails) is recorded as BLOCKED with an
  actionable remediation (`run: hydra-onboard <user>`) and a startup summary,
  instead of a silent one-line skip; `MultiUserDiscovery.Blocked()` exposes
  the list for status. Homes the daemon cannot even traverse (EACCES on stat)
  are only counted as "unreachable", not blamed on a user — that case is
  indistinguishable from an ordinary non-Hydra user with a locked-down home,
  so per-user remediation there would be a false positive (the root-run
  onboard sweep is the authority that resolves it).
- Per-phase model config: `agents.pi.phase_models` maps TDD phases (`red`, `green`, `refactor`, etc.) to optional provider/model overrides, with case-insensitive lookup, partial (model-only) overrides, and a deep-copied config cache. `PiConfig.ModelForPhase` resolves the effective provider/model for a phase. *Authored by a Hydra dogfood run.*
- `eval/run.sh`: one-command scored dogfood run — clone, seed, approve,
  watch to completion, score with hydra-eval scoped to the run's start
  timestamp.
- Per-phase worker doctrine: RED/GREEN/REFACTOR beads and fix beads now get
  phase-specific working discipline injected into their prompts (fix beads
  follow a strict reproduce→isolate→verify→fix→prove root-cause protocol);
  universal engineering doctrine (evidence-over-narration, smallest-change,
  fail-open vs fail-closed, honest completion) added to AGENTS.md.
- Benchmarks for `tools/hydra-eval` artifact parsers (`ParseCloseEvents`, `ParseBeadsJSONL`, `CountMarker`, `ParseDaemonLog`). *Authored by a Hydra dogfood run.*
- RED-phase unit tests for `tools/hydra-eval` artifact parsers (`ParseCloseEvents`, `ParseBeadsJSONL`, `CountMarker`). *Authored by a Hydra dogfood run.*
- Target-less scope warning on verified close: implementation beads (e.g.
  `fr`, `fix`, `implement`) that declare no Target Files now log the
  non-allowlisted files they changed, while non-implementation beads (e.g.
  `epic`, `approval`, `nfr-*`) remain exempt. Warn-only, never rejects the
  close; allowlisted paths (`CHANGELOG.md`, `docs/**`, `go.mod`, `go.sum`)
  are filtered out. *Authored by a Hydra dogfood run.*
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
- Anti-consultant guard: implementation beads' prompts carry a DELIVERABLE
  CONTRACT (edits must exist on disk; plans are rejected as empty diffs) —
  doctrine-tuned workers had begun honestly delivering plans instead of code.
- Eval metrics accuracy: accepted `[CloseEvent]` lines now carry the real
  validation-attempt count instead of a hardcoded `0` (one-shot-rate was
  degenerate), and `hydra-eval --since <time>` scopes all daemon-log parsing
  to the current run instead of everything since log rotation.
  *Authored by a Hydra dogfood run.*
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
- `docs/roadmap/self-improvement-loop.md`: phased roadmap for Hydra's
  self-observation/diagnosis/fix loop (Observer → Proposer → Actor) with
  safety invariants and dogfooding plan.
- Discovery report for FR-001 Phase-Model Config: `docs/discovery/FR-001-phase-model-config.md` documents requirements analysis, derived data model, ambiguity resolutions, and verification results. *Authored by a Hydra dogfood run.*
- Documented `agents.pi.phase_models` in `hydra-config.yaml.example` and `docs/operations/configuration-guide-20260309.md` with per-phase provider/model override examples and circuit-breaker precedence notes. *Authored by a Hydra dogfood run.*
- `docs/hydra-analysis-20260712iv.md`: improvement master plan (phases,
  work orders, eval harness definition).
- Dogfood PRDs: sticky approval, Target-Files scope gate, worktree
  integration reliability.
