# Changelog

All notable changes to Hydra are documented here. Every PR (human-, Claude-, or
Hydra-authored) MUST add an entry under **Unreleased** — see AGENTS.md
"Documentation & Changelog Discipline". Format follows
[Keep a Changelog](https://keepachangelog.com/); newest first within a section.

## [Unreleased]

### Added
- Pi connector can speak pi's rpc JSONL protocol (`agents.pi.mode: rpc`,
  default text). This is Phase 0: the connector answers `extension_ui_request`
  asks — previously ignored entirely, which is why every rpc attempt hung once
  `@aliou/pi-guardrails` asked a `confirm` — denies by default unless
  `rpc_auto_confirm` is set, refreshes the activity deadline on *every* event
  rather than assistant text only, and parses `get_session_stats` so token
  counts become measurable per run. `hydra --rpc-selftest "<prompt>"` exercises
  one real turn end-to-end without the daemon.
  *Authored by a Hydra dogfood run (run G), verified by its compliance gate.*
- `tools/pi-rpc`: standalone client for `pi --mode rpc`, isolated from the
  daemon, proving what rpc mode offers before any hydra change. Verified live:
  pi's extensions **do** initialise in rpc mode and rtk rewrites commands
  (`RTK rewrite: git status --short -> rtk git status --short`) — whereas the
  piped text mode hydra uses initialises none of them; and the session-control
  commands (`get_state`, `get_session_stats` with real token counts,
  `get_last_assistant_text`, `get_available_models`, plus `abort`, `follow_up`,
  `cycle_model`) all work. Includes the protocol lesson that model failures
  hide inside the message (`stopReason: "error"`), so an envelope-only client
  reports an expired key as a passing turn.
- Observer signatures for `orphan-recovery` (threshold 3 — one recovery is
  hygiene, a burst means workers are dying) and `quota-exhausted` (threshold 1),
  so both classes surface within one polling interval.
  *Authored by a Hydra dogfood run.*
- Gap adjudication can now be routed to a stronger model at maximum reasoning
  effort (`compliance_cycle.adjudicator_thinking`, alongside the existing
  `adjudicator_model`). Adjudication is the one high-consequence, low-volume
  judgment in the loop — a wrong verdict drops a real defect or spawns a wasted
  bead chain — so it is where paying for reasoning effort is justified, unlike
  the high-volume build phases. `normalizeThinking` coerces `none`/`off` to
  `low` because disabling thinking silently reroutes K3/K2.7 requests to the
  older K2.6 model.
- `scripts/prd-author.sh`: author a PRD on the VPS with the strong model
  (`k3-256k` at `max` effort by default) via the create-prd skill — the
  equivalent of authoring in Claude Code, which is not installed there.
  User-initiated only; Hydra never calls it.
- `docs/architecture/model-allocation.md`: the evidence-based model allocation
  doctrine — intelligence at the low-volume/irreversible decision points
  (PRD authoring, decomposition, adjudication, diagnosis), cheap models for
  the hundreds of build beads, with the run data showing one-shot rate 1.0 on
  K2.7 across every scored run.
- `tools/prd-lint` + task-breakdown preflight: a deterministic linter
  enforcing the create-prd compiler contract exactly as the task-breakdown
  parser reads it (same-line Target Files, Type/Priority, AC bullets, unique
  IDs, resolvable references, nfr-config commands), wired as a fail-open
  preflight into `cli.mjs` (`--no-lint` escape hatch; missing binary skips).
  One chokepoint covering every PRD author, including the compliance cycle's
  auto-generated gap PRDs. All five current PRDs lint clean; the linter lints
  its own PRD. *Authored by a Hydra dogfood run.*
- `tools/hydra-observer` (self-improvement roadmap Phase 1, deterministic
  core): scans the daemon log for known failure signatures (empty-diff storms,
  stuck workers, panics, blocked users, escalations, timeouts, max-cycles),
  compares hydra-eval metrics against a stored baseline (one-shot-rate drops,
  zero-accept runs), and writes fingerprint-deduplicated ranked backlog
  entries. Observation only — no production mutation. Exit-signal verified:
  replaying the 2026-07-22 empty-diff incident produces one critical backlog
  entry; re-runs produce zero duplicates. *Authored by a Hydra dogfood run.*
- Security scan gate: `tools/security-scan` wraps gitleaks + govulncheck and
  emits analyzer-schema gaps (secret values redacted, fail-open when scanners
  are missing); wired into `hydra-compliance-analyzer` as an always-on
  `security-scan` check, so security findings flow through the adjudicator →
  gap-to-prd → beads pipeline and block PRD approval until fixed.
  *Authored by a Hydra dogfood run.*
- Per-user PAT branch publishing: at each scheduling tick (opt-in via
  `output.push`), Hydra reconciles every project's completed worktree commits
  onto its working branch and pushes it to origin **authenticated with the
  project owner's personal access token** (`PushBranchWithToken` builds a
  one-shot `x-access-token:<pat>@host` URL — never persisted, token redacted
  from logs). Only branches ahead of their remote are pushed. Net effect: by
  the end of a run, every repo's working branch (with the per-bead
  micro-commits, `--no-ff`-preserved) is on the remote for a developer to pull
  and build locally. Fail-open — never affects scheduling.
- `hydra-onboard` now also grants the daemon **write** access (`u:hydra:rwX` +
  default ACL) to every project path declared in a user's `~/.hydra/config.yaml`,
  not just read on `~/.hydra`. Without this the daemon could discover a
  per-user project but not build, commit, or integrate into it (its home-owned
  project dir was not daemon-writable). Verifies a write test per project.
- `hydra-onboard-sweep` now re-onboards **every** user each cycle (onboard is
  idempotent) instead of skipping users whose config is already readable. The
  old skip missed projects added — or project dirs created — after a user's
  first onboard, so their write ACLs were never granted. Now adding a project
  (or cloning its dir later) is picked up within one sweep with no special
  command. Non-account home dirs (fixtures, stray/`-`-prefixed names) are
  skipped cleanly, not reported as failures.
- Publishable & observable output: worker commits now use a readable
  conventional-commit message derived from the bead (`BuildCommitMessage`:
  feat/fix/refactor/test/chore from phase or type, PRD/bead trailers) instead
  of `bead(id): title`; singleton beads optionally **push** their working
  branch after integrate (`output.push`/`output.remote`, fail-open and silent
  on protected/rejected/auth); and every repo gets a curated
  `.hydra/activity.log` (`AppendActivity`, auto-adds `.hydra/` to `.gitignore`).
  Foundation (commit_message/git_push/activity_log/config) built by a Hydra
  dogfood run; close-path wiring + activity-log tests finished by hand after
  the wiring beads failed in the run.
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
- Quota exhaustion now pauses the fleet instead of hammering the provider.
  Three compounding defects: (1) `RecordRateLimit` only opened a circuit when a
  fallback was configured; (2) an open circuit merely changed provider
  *selection* — `chooseLocked` ends with "nothing better to offer than the
  primary", so it never gated spawning; (3) the deployed config sets
  `fallback_provider` equal to the primary, making failover a no-op by
  construction. Net effect observed 2026-07-27: the fleet spawned 2-3
  workers/minute for hours against a dead quota, each spawn a full prompt build
  plus a rejected call. Quota now classifies as its own exit code (6, still
  infrastructure so no fix beads), opens the circuit on the FIRST occurrence,
  and the scheduler consults `ProviderBreaker.ShouldPause()` before spawning —
  a 30-minute cooldown, because a billing limit clears on cycle rollover, not
  in seconds like a 429.
- Hindsight recall was sending parameters the API does not accept. The client
  posted `limit`, `min_score`, `days_back` and `bank_id`, but the recall
  endpoint's schema (v0.6.2) accepts only `query`, `types`, `budget`,
  `max_tokens`, `trace`, `query_timestamp`, `include`, `tags`, `tags_match`,
  `tag_groups` — so all four were silently dropped. Every recall therefore
  returned the **entire** memory bank (measured: 49 memories, ~80KB) and the
  client kept the first few with **no quality filtering**, while
  `min_score: 0.7` sat in the config implying a filter that never ran. The
  request now uses `max_tokens` (the API's real size control), configurable via
  `intelligence.hindsight.max_tokens` and `HYDRA_HISTORIESIGHT_MAX_TOKENS`,
  default 800. Measured against the live API: **80,517 B → 15,736 B per
  recall**. A test pins the request body so the phantom fields cannot return.
- The intelligence stack told every agent to run a binary that does not exist.
  `AGENTS.md` and the `code-discovery` / `code-search-and-intelligence` skills
  referenced `hydra-llm-tldr` (27 occurrences); the real binary is `llm-tldr`
  (`/usr/local/bin/llm-tldr`, on the daemon user's PATH). `TLDRIndexer` had the
  same wrong name as its fallback when no path is configured — and production
  config sets no path, so the default was always used. Observed in a run G
  worker: "`hydra-llm-tldr search` / `impact` failed with `EACCES` / function not
  found; I relied on `grep`", i.e. the agent silently degraded to a worse tool
  after burning turns on a command that could never work.
- The prd-lint **preflight was missing from the repository**: PR #72 shipped
  `tools/prd-lint` but not the `cli.mjs` wiring that invokes it, so a fresh
  clone or deploy silently had no PRD gate. Cause: the staging command
  addressed the skill through `pi-config/skills/…`, which is a **symlink** to
  `.agents/skills/`, and `git add` refuses paths "beyond a symbolic link" —
  the fatal error was swallowed by a `2>/dev/null`, so the omission looked
  like success. Now committed via the real path, with `preflight.mjs` and its
  test suite.
- `tools/hydra-compliance-analyzer/main_test.go` (the analyzer-wiring test from
  the security-scan PRD) built `security-scan` by package path from the repo
  root, but that tool is its own module — the test panicked with "main module
  does not contain package". It now builds from inside the module directory
  with an absolute output path.
- Orphan bead recovery, three defects in one path: it ran **after** the
  cleanup cycle's `len(trackedWorkers) == 0` early return, so it was
  unreachable in exactly the situation it exists for (a 65-minute dead stall in
  dogfood run D, beads reopened by hand); its documented 5-minute grace was
  read into a log string but never compared against anything; and it
  force-**closed** incomplete work with `--force`, marking undone beads done and
  unblocking dependents — the failure class the verified-close gate exists to
  prevent. Recovery now runs before the early return, skips beads younger than
  the grace (and beads with no `UpdatedAt`, rather than acting blind), and
  **reopens** via `UpdateBeadStatus(StatusOpen)` — the same path
  `rejectClose` uses — so the scheduler re-attempts the work.
  *Authored by a Hydra dogfood run.*
- Quota exhaustion is now classified as **infrastructure**, not a code
  failure. Kimi returns an exhausted billing quota as HTTP 403 with a
  `permission_error`/`access_terminated_error` payload, which the existing
  rate-limit markers (`429`, `rate_limit`) did not match — so each affected
  bead was retried 4x and then spawned a fix bead, and every one of those
  retries was another rejected call against the quota that was already gone.
  The failure consumed the quota it was failing on. `isQuotaError` now drives
  both `shouldRetry` (fail fast) and `classifyFailure` (exit code 2 =
  infrastructure → no fix bead, scheduler backoff). Observed live 2026-07-27:
  36 useless fix beads from one exhausted quota.
- `tools/gap-to-prd` now emits compiler-contract-compliant mini-PRDs:
  same-line `Target Files` (the old bullet list was unparseable by
  task-breakdown, silently producing targetless gap beads), `Type`/`Priority`
  (P0 for critical)/`Test Command`/`Build Command` per FR, deterministic FR
  ordering with correct `Aggregates` references, and a NEEDS-HUMAN-TRIAGE
  section for targetless gaps. Golden-file test embeds the parser's regexes.
  *Authored by a Hydra dogfood run.*
- Verified-close gate now inspects the bead's **worktree**, not the shared
  checkout, when worktree isolation is active. The diff + scope gates read
  `ResolveRepoPath` (the main checkout, which stays clean until a chain
  integrates), so every worktree-isolated bead whose worker wrote to its
  worktree was falsely rejected as `empty-diff` and cycled through retries.
  Root-caused live: worker wrote `activity_log.go` into its worktree, gate
  saw the clean checkout, rejected it 21×. Now uses
  `WorktreeManager.ExistingWorktree` (non-creating) to check the real tree.
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
- `docs/architecture/system-loop.md`: process-flow map of Hydra's three
  nested control loops (build / learning / meta) with a loop-closure audit —
  automated vs planned vs human-by-design edges marked, plus the known
  verdict-feedback leak and measured compounding evidence.
- `docs/roadmap/self-improvement-loop.md`: phased roadmap for Hydra's
  self-observation/diagnosis/fix loop (Observer → Proposer → Actor) with
  safety invariants and dogfooding plan.
- Discovery report for FR-001 Phase-Model Config: `docs/discovery/FR-001-phase-model-config.md` documents requirements analysis, derived data model, ambiguity resolutions, and verification results. *Authored by a Hydra dogfood run.*
- Documented `agents.pi.phase_models` in `hydra-config.yaml.example` and `docs/operations/configuration-guide-20260309.md` with per-phase provider/model override examples and circuit-breaker precedence notes. *Authored by a Hydra dogfood run.*
- `docs/hydra-analysis-20260712iv.md`: improvement master plan (phases,
  work orders, eval harness definition).
- Dogfood PRDs: sticky approval, Target-Files scope gate, worktree
  integration reliability.
