# Changelog

All notable changes to Hydra are documented here. Every PR (human-, Claude-, or
Hydra-authored) MUST add an entry under **Unreleased** — see AGENTS.md
"Documentation & Changelog Discipline". Format follows
[Keep a Changelog](https://keepachangelog.com/); newest first within a section.

## [Unreleased]

### Fixed
- Pan no longer judges beads that have already reached a terminal state: `Shepherd.Candidates()` accepts an optional `TerminalBead` injection and silently skips workers whose bead is closed/completed, preventing repeated MiniMax calls and log spam for dead beads (e.g., 1xs7z's 118M-token parked row). Wired in `internal/core/service_starter.go` with a 5-minute cache and fail-open on lookup errors.
- Directory-shaped bead targets (`Files: cmd/worker/`) could never satisfy the close-path diff gate: `buildTargetSets` treats every target as a file and `hasTargetChanges` exact-matched only, so hydra-2l9's fully-committed worktree was empty-diff rejected 3× (7.5M tokens at risk). `hasTargetChanges` now also path-prefix matches with a `/` boundary. `TestHasTargetChanges_DirectoryTarget`.
- Wrong-repo worktree on unresolved project: scheduler-path beads carry an empty `Project` field; `FindProjectPath` matched nothing, the spawner fell back to the empty field, and `git worktree add` ran in the daemon's cwd — 1xs7z's "srv" chain worktree was a dev-tools checkout (51.6M tokens wandering the wrong repo, 2026-08-23). `FindProjectPath` now derives project from the bead ID prefix; the spawner never substitutes the name-as-path; `EnsureWorktree` refuses empty paths outright.



### Fixed
- Charon kills now consume retry budget: killed processes classify as infra exits on the worker-death path (forced-kill = free retry), so budget/stuck kills respawned forever — 1xs7z burned 51.6M tokens across 4+ kill/respawn cycles on 2026-08-23. `recordKillRetry` counts each kill; the spawn gate (`RetryExhaustedError`) now stops the cycle at `default_retries`.



### Added
- Worker prompt now injects deterministic discovery directives as protocol: every bead is told to run `hindsight_search` once at start, and beads without a `Files:` line (discovery-class beads) are required to run `graphify query` before exploring files, with a fallback to `rg`. Session forensics showed workers skipped graphify entirely and rarely used hindsight_search because both were discretionary skills.

### Added
- Pan can now autonomously close already-satisfied beads: a new `close_satisfied` verdict runs daemon-owned scoped gates on the main checkout and, if green, closes the bead as `already-satisfied (Pan)` with label `closed-satisfied`. This breaks the empty-diff reject loop where a worker correctly finds nothing to do but verified-close refuses an empty diff, burning tens of millions of tokens until a human intervenes (1xs7z, round 3).

### Added
- Worker prompt now states an explicit turn budget (~40 turns) with an over-budget exit: commit progress, emit `done:false` with findings. The turn-economy block implied infinite runway; 200-turn grinds cost 20M tokens (kgkc7/soe53). Also mandates piping test output through `tail` — full jest dumps were the per-turn context flood.



### Changed
- Token budget is now a signal, not an axe: crossing `overseer.max_bead_tokens` enqueues a "converge now" steer (rate-limited 1/hr, labelled `token-budget-soft`) and surfaces the worker to Pan as a judgment candidate — it no longer kills. A hard backstop at 2× kills true runaways, stayed by a fresh Pan `continue` verdict. The kill-at-cap design would have killed ux5 (9M) and acx (11.7M) — beads that landed — and did kill cc0d6 minutes after Pan judged it healthy.



### Added
- Pre-spawn probe now primes the worker prompt on RED: the probe already knows why the scoped gate fails, so its parsed summary + raw log path are written into the bead description (marker-delimited, replaced on re-probe) before spawning. Workers were burning 3-5 discovery turns at ~100K context each re-deriving the failure. Green path unchanged (0-token close); faults still fail open.



### Fixed
- Integration no longer quarantines on `.beads/`-only merge conflicts: the beads mirror is br-owned on main, worktree-side writes are redundant, so conflicts confined to `.beads/` auto-resolve `--ours` and the merge proceeds (65wa0's gate-proven suite fix quarantined on a mirror row, 2026-08-21). Real-code conflicts still abort + quarantine.



### Added
- Per-bead token budget (`overseer.max_bead_tokens`, 0 = disabled): Charon kills a *working* worker whose cumulative tokens exceed the ceiling and marks the bead failed so the retry cap escalates, labelled `token-budget-exceeded`. Non-converging grinders were burning 18-25M before the wall-clock timeout noticed (kgkc7 20.5M, soe53 18.2M). Parked workers are never budget-killed — frozen workers burn nothing.



### Fixed
- Phantom validation churn killed at the source: "No Implementation Files" fired purely from missing `target_files` metadata; on a fully-closed chain verified-close already proved the diffs exist. Metadata-only gap is now advisory (Warning, no fix bead) — kgkc7 burned 20.5M tokens wandering the repo on this phantom. Open leaves keep it Critical.



### Fixed
- PRD validation no longer loses `target_files` metadata: `internal/beads/client.go` now enriches beads from `.beads/issues.jsonl` `metadata`/`bead_metadata`, and leaf beads under validation chains have backfilled `target_files` (hydra-acx, hydra-f3s, hydra-l1n).
- Gate-failure labels never applied: `UpdateBeadLabels` passed repeated `-l` flags in one `br label add`, but br's `-l` is singular (exit 2) — every `gate-failed`/`gate-failed-<gate>` label silently dropped since slice 2. Now one `br label add` per label.



### Fixed
- Config hot-reload now applies `projects.list` changes: `ConfigManager.UpdateConfig` diffs old vs new projects, updates `beadDiscovery`, `beadAnalyzer`, `projectResolver`, and `beadFileReader`, and registers chains/indexing for added projects without disturbing in-flight workers. Previously adding a project to `hydra.yaml` required a daemon restart before its beads were discovered (round 2; hydra-ux5).
- `WorkerEnvBuilder.Build` no longer inherits `HYDRA_DAEMON_GATES` from the daemon's environment; the flag is now set only from the project's `daemon_owned` gate config, preventing leaked state into workers and a flaky unit test under `HYDRA_DAEMON_GATES=1`.
- `internal/pi` `EnsureDirectory` and `EnsureFile` now apply their requested permissions with `os.Chmod` so tests pass under a restrictive daemon umask.
- Empty-diff reject loop on committed work: the verified-close diff gate only inspected `git status` (uncommitted). A worker that commits its own fix in the chain worktree leaves a clean tree → rejected as empty-diff → next worker sees the work done, no-ops, rejected again (srv 65wa0: 24.9M tokens, 2026-08-21). The gate now unions uncommitted changes with `git diff <base>...HEAD` (base = the checkout branch the worktree diverged from).



### Fixed
- Double-spawn race: `Spawn` inserts a `{status: spawning}` placeholder before the process reaches the worker tracker; a concurrent spawn for the same bead mistook the placeholder for an untracked orphan, deleted it, and proceeded (j81fp spawned twice in one second, 2026-08-21). A "spawning" placeholder is now treated as proof of life — rejected as duplicate, never deleted.



### Fixed
- Pre-spawn probe never fired: scheduler-path beads carry an empty `Project` field (the project lives in the bead ID prefix — the codebase pattern is `BeadID.Project()`); the probe's config lookup missed and silently skipped. Zero `Prespawn` log lines while 65wa0 burned 20.7M tokens across empty-diff rounds. Probe now derives project via `beadProject()` (explicit field, then ID prefix).



### Fixed
- Worktree commits now exclude `.beads/` — daemon machinery state (status flips, DB mirror) must never merge back into main. Root cause of the srv soe53 breakage: `.gitattributes` union-merge on id-keyed `issues.jsonl` kept both sides' rows → duplicate ids → br refused to read the DB. Union is right for append-only prose (CHANGELOG), wrong for id-keyed JSONL; the srv repo attribute was reverted and the file repaired (last-wins dedup).



### Fixed
- Daemon-gate bypass holes found by the first dogfood run (hydra-5cl): (1) workers no longer self-close beads when `HYDRA_DAEMON_GATES=1` — closure belongs to the daemon after gates pass (worker self-close had skipped the entire gate loop); (2) worktree setup failure under `daemon_owned` now fails the spawn instead of falling back to the shared checkout (hydra-5cl edited the daemon's own live repo). Root cause of the worktree failure itself: hydra is a submodule — `/opt/dev-tools/.git/modules/components/hydra` was root-owned; chowned to hydra.



### Added
- Pre-spawn gate probe: for `daemon_owned` projects, scoped gates (cmd contains `<scope>`) run in the project checkout BEFORE a worker spawns; already-green beads close at zero token cost with full DAG completion semantics (srv 65wa0 burned 7.9M tokens over 3 empty-diff reject rounds because its suite was already green). Probe only considers scoped gates, requires bead target files, and fails open — probe faults spawn normally. Emits `prespawn_closed` event.



### Fixed
- `TestMultiUser_EndToEnd` no longer relies on a fixed 200 ms sleep to assert worker spawns; it polls active workers every 25 ms with a 10 s deadline, so the test passes reliably under load when the live fleet shares the hydra user's GOCACHE/CPU. Removes the gate skip for this test.
- `IntegrateAndRemove` auto-commits a dirty main checkout (`wip: hydra auto-commit ...`) before merging — uncommitted changes the merge touches used to fail every integration ("Your local changes would be overwritten"), so gate-verified work piled up in quarantine while beads closed as completed (srv-smart-engine 2026-08-19: 9 legacy-era files blocked all integrations). Same-hunk content conflicts still quarantine as designed.
- srv-smart-engine repo hygiene (project-side, not hydra code): `.gitattributes` union-merge for append-only files (CHANGELOG.md, docs/untestable-code-log.md) — every bead appends per PRD §3; adjacent-hunk conflicts made every worktree integration collide. Union merge keeps both sides' lines.



### Added
- Gate `<scope>` templating: `GateRequest.Scope` substitutes into gate commands (single-quote escaped); empty scope drops the placeholder token so the full command runs. The close path derives scope from the bead's target files (`Files:` metadata/description), so a bead fixing suite X is gated on suite X — not on every other red suite in the repo. Enables `daemon_owned` on partially-red projects without reject storms.



### Added
- Daemon gates wired into the close path (FR-002 slice 2): when a project sets `gates.daemon_owned: true`, a successful worker completion runs the declared gates daemon-side in the chain worktree BEFORE commit/integrate/close. Gate failure reopens the bead via the existing rejectClose machinery (bounded by `validation.max_rounds`, then human escalation), labels it `gate-failed`, and rewrites the description with the parsed summary + raw log path so the retry worker starts from evidence instead of re-running the suite. No worktree, missing runner, or unknown parser all fail closed. Daemon startup recovers `running` gate artifacts from a crashed predecessor. Workers on daemon-owned projects get `HYDRA_DAEMON_GATES=1` and a VERIFICATION OWNERSHIP prompt note. `BeadsClient` gains `UpdateBeadDescription` (br update --description); all mocks updated. Default off — legacy path untouched.



### Documentation
- PRD-A FR-002 expanded into the full daemon gate-runner design: Class A/B/C separation (daemon code / declared config + parser registry / LLM judgment), artifact store at `$HYDRA_HOME/gates/`, crash-resilience semantics (artifact before LLM, fail-closed close path, per-project lock), config schema, bootstrap contract. Evidence: co3u8 autopsy (25M tokens, ~85% of turns babysitting deterministic commands).



### Added
- Daemon gate runner (PRD-A FR-002, slice 1): `GateRunner` executes declared gates (test/lint/build) daemon-side with per-project serialization, worktree-only execution guard, timeouts, and atomic artifact persistence at `$HYDRA_HOME/gates/<project>/<bead>/` — artifacts written before any LLM involvement, `FinalizeStaleGateArtifacts` recovers `running` artifacts on restart. Parser registry: `exit-code`, `jest-coverage` (coverage % + failing suites; unparseable output fails closed). Config: per-project `gates:` block with `daemon_owned` flag (default false = legacy path). New `hydra gates verify <project>` bootstrap command dry-runs declared gates on the real checkout. Verified live against srv-smart-engine: lint/build pass; test correctly reports 26.97% < 80% with failing suites named.



### Added
- Worker extension resolver now supports git-installed Pi packages (`git/<host>/<org>/<repo>`, matched by repo segment to tolerate org drift/forks) and directory entry points (resolved to `index.ts`/`index.js` per pi convention). This unblocks the Hindsight pi extension (`hindsight` alias → `@walodayeet/hindsight-pi`) for workers — the first recall channel on the daemon path; npm installs still take precedence.

### Added
- Worker prompts now carry TURN ECONOMY guidance (batch independent tool calls in one message, one comprehensive probe over probe sequences, no full-suite re-runs per micro-edit). Motivation: session forensics on srv-smart-engine showed 25M tokens for one REFACTOR bead, 24.8M of it transcript cache-reads across 167 turns; turn count is the linear multiplier of the quadratic tax. Behavioral hedge only — measured model default is 1.00-1.02 tool calls/turn even with harness-level batching instructions.

### Fixed
- Sustained provider-capacity congestion now pauses spawning: exit-7 ("concurrent request limit") failures are tracked as a consecutive streak, and a streak of 6+ spanning >= 30 minutes opens an escalating spawn cooldown (15m -> 30m -> 60m -> 120m cap); any completed worker resets the streak. Blips (fast streaks) still never pause the fleet. Root cause of the co3u8 storm: 219 rejected turns over 3 days, each respawn re-billing the full transcript (~6.1M tokens, zero output).

### Fixed
- Validation fix-bead churn factory: `RunPRDValidation`/`RunTDDValidation` now dedup against open/in-progress beads by round-insensitive title prefix before creating fix or re-validation beads, and dedup within a single round. Previously every validation round spawned fresh duplicates for unresolved gaps — on srv-smart-engine 355 open machinery beads accumulated against 36 real chain beads, burning ~8M tokens per churn worker. Closed fix beads do not block respawn, so the max-rounds retry escalation still works.
- Fleet-wide priority inversion: every scheduler sort site used numeric-descending priority (P4 first) while beads convention is P0 = highest (`Priority::CRITICAL(0) < HIGH(1) < ... < BACKLOG(4)` in beads-rust). Ready queues therefore handed worker slots to the LOWEST-priority beads first — on srv-smart-engine P3 validation churn repeatedly took slots while 12 ready P1 chain beads waited. Flipped all six sites (`scheduler.GetReadyNodes`, `dag_navigator.GetReadyBeads`, `bead_discovery` br/bv paths, `dag` topological sort), made them stable sorts, and added the missing sort to the event-driven immediate-schedule path. Two legacy tests that encoded the inverted convention were corrected.

### Documentation
- Two planning PRDs from the srv-smart-engine campaign analysis: `docs/PRDs/typed-workflow-templates-daemon-gates-20260813.md` (bead-type workflow templates + daemon-owned deterministic gates + verification consolidation) and `docs/PRDs/memory-recall-quality-20260813.md` (recall telemetry, golden probes, bank hygiene). Planning artifacts only — no behavior change; the legacy chain stays default behind config flags.

### Fixed
- Worktree reconcile is now terminal-gated and quarantine-bounded: it only integrates `hydra/wt-*` branches whose bead is closed/completed/failed (open, in_progress, parked-with-stale-heartbeat, and unverifiable beads are skipped — a parked worker's live worktree and an open bead's 1,356-line worktree were previously treated as orphans and merged prematurely), and a terminal bead whose merge conflicts is quarantined to `hydra/quarantine/<key>` instead of retry-failing every sweep forever (observed: `conflicts=23` every 10 minutes for weeks). Nil status predicate fails closed.

### Fixed
- Validation fix beads no longer churn the board: Warning-severity gaps (e.g. "Missing Acceptance Criteria" — fires on any root bead without AC text) are now advisory log lines instead of worker-spawning fix beads, and all spawned fix/re-validation beads are priority-floored at P3 so validation meta-work never queue-jumps fresh PRD beads. On srv-smart-engine these beads inherited P0-P2 parents; ~25% of a day's commits were validation-round churn while the open board grew instead of draining.
- Charon now reaps ghost registry rows at runtime: workers that die without posting a completion marker (SIGKILL, OOM, daemon kill) left `working`/`parked` rows with frozen heartbeats that only the 24h startup sweep cleared — 19 accumulated in one day, poisoning fleet metrics. New `ReapGhosts` store method runs in Charon's scan cycle (10-minute throttle, 1h heartbeat cutoff) and deliberately spares `completed` rows so completion history survives for metrics. Placed before the no-tracked-workers early return — ghosts accumulate exactly when the fleet is idle.
- Worker token accounting now lands for successful completions: `stateReporter.Stop` no longer claims `state=completed` in its final liveness beat — the daemon report endpoint only accepts working/parked/hung and answered 400, silently dropping the token flush. Every exit-0 row recorded `tokens_so_far=0` while failures recorded correctly; terminal state is now left to the completion marker (`MarkCompleted`).

### Changed
- Default `overseer.max_workers` is now 2 (code default was 6, example was 5) — matches the operator-tuned production value; the AIMD governor scales within the ceiling under provider congestion.
### Added
- Worktree preflight gate (fail-early): before spawning, the daemon verifies a Node worktree has resolvable dependencies — node_modules present, no broken dependency symlinks (the `@haulmatic/common → ../../../lib-nestjs` class), lockfile-keyed success sentinel; missing/broken deps trigger a bounded deterministic install (npm ci/pnpm/yarn, 10m cap). An unrecoverable environment aborts the spawn (bead stays open) instead of burning a worker's lifetime on npm install.

### Fixed
- Activity watchdog no longer kills healthy turns when pi's RPC stdout stalls: pi session-file mtime is now a secondary liveness signal (`ActivityMonitor.SetSessionDir`) — a session write refreshes the activity clock. Closes the RPC-stall blindness found in the 3a4 chain walk (kill 75s after the last session-recorded event).

### Fixed
- Worker no longer watchdog-kills healthy pi mid-turn: pi RPC event lines (tool results carrying file reads/jest output, 71KB observed) exceeded bufio's 64KB scanner default, silently killing the stdout scanner and freezing the activity monitor. New `newPiScanner` (8MiB bound) used for both pi pipes, plus fail-loud post-loop scanner error logs. Root cause of chronic zero completions on tool-heavy beads.

### Added
- **`projects.multi_user_discovery` config gate.** The multi-user project scan (`~/projects` across all users) previously ran unconditionally at daemon startup. Set `multi_user_discovery: false` to restrict execution to `projects.list` only. Default stays `true` so existing deployments are unaffected. Operators can now focus the fleet on a single user's project without code changes.

### Fixed
- **Overseer worker timeout was park-blind and killed healthy workers mid-turn.** `WorkerLifecycle.Wait` used a hardcoded 30m wall clock that counted time the worker spent **parked** waiting out provider congestion. Live incident: `hydra-code-9p7` spent ~20m parked, woke at 07:43:58Z, was doing real work (tool calls at 07:58/08:00) when `[Overseer] Worker hydra-code-9p7 timed out, killing process` fired at 30m0.004s. The worker's own `maxTotalRuntime` already excludes parked time; the daemon-side clock now does too, using a ticker loop that accrues only active time and reads `parked` state from the registry. Also honors `config.Overseer.WorkerTimeout` instead of ignoring it.
- **Pan's stale judgments killed healthy young workers.** Two linked holes, one live incident (`srv-smart-engine-pes`, 2026-08-07: self-aborted 2m20s into a healthy turn → exit 1 → bead fault): (a) shepherd commands are queued per *bead* but judged per *worker instance* — a new worker registering for a retried bead drained the `abandon-with-summary` judged against its dead predecessor; `UpsertWorker` now clears pending commands on PID change (new instance = stale judgments void). (b) Pan hung-judged workers younger than its own 5m threshold (tokens legitimately 0 mid-first-turn, heartbeat predating the worker in the registration race); `Candidates` now grants a 2×`HungAfter` young-worker grace.
- **Signal deaths of workers classified as bead faults.** When Charon SIGKILLed a hung worker (or the OOM killer/operator killed one), the daemon recorded `worker exited with code -1` and classified it as a bead fault — 99 such deaths in 2h on 2026-08-07, each spawning fix beads that died the same way. The worker's own code only ever exits deliberately (1/2/3/5/6/7); a negative exit code is always an external kill, so `isInfrastructureExitCode` now treats all negative codes as infrastructure. Operators see hung-kill retries instead of phantom `closed-failed` + fix-bead recursion.
- **Watchdog-forced kills classified as bead faults.** When the activity-timeout or max-runtime watchdog killed Pi, the kill reason lived only in `cmdErr` while `classifyFailure` reads `result.Error` (pi's stderr) — so these provider-side hangs exited `-1`/`1`, recorded as bead faults, closed-failed beads, and spawned fix beads that died the same way (watchdog-found: `hydra-code-x77`). `appendForcedKillReason` now surfaces the kill reason into `result.Error` so the existing timeout markers classify it as exit 3 (infrastructure); `max total runtime` added as a timeout marker. Operators see fewer phantom `closed-failed` beads and no more fix-bead recursion from congested-provider stalls.
- **Epic auto-close no longer counts failed children.** `CloseReadyEpics` treated any `closed` task child as done — including `closed-failed` ones — so phase epics (`srv-smart-engine-757/tml/m5q`) auto-closed over failed work, reporting whole phases complete with no working code. A `closed-failed` child now blocks the epic unless its `Fix: <title>` bead closed without the failed label. The three wrongly-closed epics were reopened live.

### Fixed
- **Final token flush on worker exit.** Workers exit <30s after the last turn and `Stop()` posted only a bare completion marker (bead id + exit code), so parsed token totals never reached the registry — observed on hydra-code-zuo: worker logged `total=616789`, registry showed `tokens_so_far=0`, feeding Pan's false zero-token stuck verdicts. `Stop` now flushes a full final heartbeat (state + tokens) before the completion marker.
- **Final token flush on worker exit.** Workers exit <30s after the last turn and `Stop()` posted only a bare completion marker (bead id + exit code), so parsed token totals never reached the registry — observed on hydra-code-zuo: worker logged `total=616789`, registry showed `tokens_so_far=0`, feeding Pan's false zero-token stuck verdicts. `Stop` now flushes a full final heartbeat (state + tokens) before the completion marker.
- **Pan command-queue dedup.** Pan re-queued the same verdict every tick for dead workers that never consume their queue — observed live: 511 duplicate `abandon-with-summary` commands on one dead bead, MiniMax tokens burned per tick. Before enqueueing, Pan now skips a decision whose kind is already pending for that worker (fail-open on lookup error).

### Fixed
- **Human-gate close-path guard.** `BeadCloser.Close` now refuses to system-close `approval`/`approval-gate` beads — observed live: the daemon closed `srv-smart-engine-56z` with verdict `accepted` after a worker review, bypassing the human gate (the worker-side `br close` guard held; the daemon path did not). Human gates require an explicit human `br close`.
- **Approval-bead gate leak.** Beads with `Type: approval` (emitted by task-breakdown as human approval gates) were worker-executed whenever they were `open` — only `approval-gate` and in-progress `approval` beads were skipped. Observed live: a worker spent a full turn reviewing `srv-smart-engine-56z`. Approval beads are now skipped at all four gate sites (`dag_navigator`, `overseer.shouldSpawnWorker`, `scheduler` ×2) regardless of status, and route to `AgentHuman`.

### Changed
- **Pattern storage path (ops).** Live `/etc/hydra/hydra.yaml` `intelligence.patterns.storage_path` moved from the code checkout (`/opt/dev-tools/components/hydra/.hydra/patterns`, unwritable by the `hydra` user — every close-time verdict pattern failed with `Failed to store verdict pattern`) to `$HYDRA_HOME/.hydra/patterns`.

### Documentation
- **Roadmap: Pan as the self-healing/self-improving loop driver.** Added `docs/roadmap/bug-fix-loop-closure-20260803.md` §14: Pan's current functions (works/doesn't/planned), fleet-health pass, anomaly→bead, decision memory, and escalation cap. Status remains "under consideration (NOT committed)". Derived from the live 2026-08-06 dogfood session (`hydra-code-fku` watchdog).

- **PRD: park canary — coordinated congestion probing.** `docs/PRDs/park-canary-coordination-20260806.md`: when N workers are parked on provider-capacity, the oldest-parked real worker becomes the canary (probes every 30–60s via its own resumed turn — no synthetic pinger), followers extend to ceiling backoff, and a capacity-available signal wakes followers staggered. Fail-open to today's independent backoff. Proposed, not scheduled.

### Documentation
- **Roadmap: closing the bug-fix loop end-to-end.** New `docs/roadmap/bug-fix-loop-closure-20260803.md` — the agreed plan for ~80% automation of the QA→JIRA→fix→deploy→verify cycle: repro-first RED gate, confidence-tiered auto-merge, JIRA adapter, dev-env sentinel (Playwright/OpenAPI/log-fingerprint probes), pattern fast lane, and reuse for untested features. Mermaid workflows, actor/tooling tables, rollout order, industry references, plus two explicitly non-committed candidates: Piolium security-audit integration and Passmark AI browser regression probes. Planning only; nothing scheduled.

### Added
- **Close-time pattern extraction (verdict → pattern store).** When a worker emits a `HYDRA_VERDICT`, the worker now stores patterns locally at bead completion (outcome + files changed, confidence-gated) instead of waiting for settled-state detection — which stays disabled. Wired via `HYDRA_PATTERNS_*` env vars from `intelligence.patterns` config; fail-open. Also fixes `FilePatternStorage` filename collisions (same-second timestamps overwrote sibling patterns). Graphify indexer: a project with no code files (`No code files found - nothing to rebuild`) is now logged as a skip, not an error.
- **Scheduler congestion pause.** When the registry shows 3+ live parked
  workers (heartbeat within 10 min), the scheduler pauses new spawns for 5 min
  so the park-and-resume loop can drain the backlog before the fleet piles onto
  the same saturated provider slots. The gate is fail-open: a missing registry or
  counter error never blocks the scheduler. See `internal/core/scheduler.go` and
  `docs/architecture/shepherd.md`.
- **Named pi session files for park-and-resume across daemon restarts.** Workers now build a deterministic, file-name-safe session id from the bead id, create a directory under `$HYDRA_HOME/sessions`, and pass `--session` and `--session-dir` to pi. The session directory is exported as `PI_CODING_AGENT_SESSION_DIR` and the session id is reported to the daemon registry so a replacement worker can resume the same context after a restart.
- **Registry GC: reap stale worker rows on daemon startup.** `Store.ReapStale`
  removes terminal (`completed`) rows and rows whose last heartbeat is older
  than the cutoff, plus any queued commands for those beads. Wired into
  `internal/core/service_starter.go` with a 24-hour cutoff so Pan and the
  scheduler never act on zombie registry state left by prior runs.
- **Worker registry + Pan, the shepherd (PRD parts 4 & 6).**
  `internal/registry`: daemon-owned SQLite store replacing `workers.json` —
  worker records (state, pi session id, error triple, token progress, park
  fields, nudge count) and a per-worker command queue (`continue`, `steer`,
  `compact-and-continue`, `abandon-with-summary`). Completed workers are
  never resurrected by late upserts. `internal/pan`: fail-open shepherd
  pi-agent with its own provider key (`shepherd.pan` config) that reads the
  registry and queues decisions for the ambiguous tail — parked-overdue,
  hung, and verdict-missing-past-nudge-cap workers. Everything else stays on
  deterministic rules, which run with or without Pan.
- **Worker registry API + worker state reporter (PRD parts 2 & 4).**
  Daemon endpoints `POST /api/v1/workers/report`, `/workers/completed` and
  `GET /api/v1/workers/commands` (consume-on-read). Workers self-register,
  heartbeat state every 30 s, drain shepherd commands, and report their
  terminal state before exit; `HYDRA_API_URL` is passed at spawn and an
  unreachable registry degrades to pre-registry behavior (fail-open).
- **Verdict closure protocol (PRD part 3).** Worker prompts now demand a
  final `HYDRA_VERDICT` JSON block; an rpc turn that ends without a valid
  done/abandon verdict is re-prompted in the same live pi session (cap 3)
  instead of being torn down — pi is no longer killed merely because it
  stopped talking. Hard kills remain reserved for hung processes and are
  system failures, not bead failures.
- **Park-and-resume on provider congestion (PRD part 2, Tier A).** A failed
  rpc turn classified as provider capacity (concurrency limit, engine
  overload, 429 — marker-first because Kimi's error `type` field lies) now
  parks the live pi session: state reported as `parked` with wake time,
  backoff 30 s→5 min with jitter, park cap 60 min (`HYDRA_MAX_PARK_MINUTES`),
  then the same session is re-prompted. Parked workers are exempt from the
  activity timeout, and parked time is excluded from the max-total-runtime
  budget. Zombie-park guard: if pi died while parked the wake write fails
  and the normal failure path takes over.
- **PRD: worker park-and-resume on provider congestion**
  (`docs/PRDs/worker-park-and-resume-20260731.md`). Workers whose session is
  interrupted by provider-capacity errors (403 concurrency, 429, overload)
  should park the live pi rpc session and resume it after backoff instead of
  terminating and losing all context — the root cause of repeated rework seen
  in the 2026-07-31 `hydra-testbed-y05` autopsy (~50 min compute, zero merged
  output). Rewritten after design review: 3-tier error classification
  (auto-park / escalation-park on credential errors / close), worker control
  loop with named session files (parked workers survive daemon restarts),
  verdict-based closure (`HYDRA_VERDICT` required before pi is killed),
  daemon-owned SQLite registry + command queue, a four-knob timeout hierarchy
  with a monotonic lifetime cap, and "Pan" — a pi-agent shepherd with its own
  provider key that handles the ambiguous tail and is fail-open by contract.

### Fixed
- **Worker session flag broke every fresh bead.** The worker passed `--session <id>` to pi, which resumes an EXISTING session and exits 1 with `No session found matching` when it does not exist — i.e. for every fresh bead. First real fleet run after the named-session PR failed 192/192 executions in <1s and the shepherd marked the beads terminal. Now passes `--session-id`, which creates the named session when missing.
- **Worker never reported token usage to the registry.** Pi emits per-message usage on its rpc stream (one execution: 8,009 usage events, 42k tokens in a single message) and the worker parsed the session stats — then only logged them. `SetTokens` was dead code; the registry always showed `tokens_so_far: 0`, which made Pan's zero-token stuck heuristics condemn healthy workers. The turn-end stats are now forwarded via `reportTurnTokens` (absolute: `get_session_stats` is cumulative per named session).
- **Worker session flag broke every fresh bead.** The worker passed `--session <id>` to pi, which resumes an EXISTING session and exits 1 with `No session found matching` when it does not exist — i.e. for every fresh bead. First real fleet run after the named-session PR failed 192/192 executions in <1s and the shepherd marked the beads terminal. Now passes `--session-id`, which creates the named session when missing.
- **Data race in GraphQL event resolver.** `Resolver.Stop()` closed `eventChannel` while `Broadcast*` methods could still select-send on it, and the broadcaster's shutdown path closed subscriber channels while in-flight send goroutines were still using them — `panic: send on closed channel` under `-race` (CI run 30822045466, 10/10 locally). Stop no longer closes `eventChannel` (the broadcaster exits on `ctx.Done` anyway), and subscriber close/send now share a per-subscriber mutex + closed flag.
- **Graphify indexer test flake fixed.** `TestGraphifyIndexer_*` tests that start a background build now wait for it to drain (`waitIdle` polling `inFlight`) before returning; previously teardown intermittently raced the goroutine's `.gitignore` write and failed `t.TempDir` cleanup with "directory not empty" (~20% of runs).
- **Noisy `br close` exit 2 failures when the target directory is not a beads workspace.** The worker and Charon now check for a `.beads` directory before invoking `br close`; if the workspace is missing (testbed beads, cleaned worktrees, or unconfigured project paths), the close is skipped at debug level instead of logging a warning/error.
- **`go test ./...` now passes by default.** Environment-dependent integration tests in `internal/integration` and `internal/pi` are gated behind the `integration` build tag; a stale hard-coded skill count in `TestSkillUpdater_PerformanceTarget` is now dynamic; and the `fmt.Println` vet warning in `docs/collective-unconscious/examples` is fixed.

### Changed
- **Pan defaults to a dedicated MiniMax key.** `hydra-config.yaml.example` and the shepherd config regression test now use `provider: minimax`, `model: MiniMax-M2.7`, and `api_key_name: MINIMAX_API_KEY`.
- **Runtime state moved out of `/opt/hydra` into `/opt/dev-tools/var/hydra`.**
  Code (`components/hydra`), shared config (`config/pi`) and runtime state
  (`var/hydra`) now sit side by side under `/opt/dev-tools`, split by what changes
  them. `$HYDRA_HOME`, the `hydra` user's home, three systemd units, logrotate and
  the shell-script defaults all point at the new path; `/opt/hydra` remains as a
  compatibility symlink and will be removed after a clean run. `var/` is
  gitignored in the monorepo — state must never sit in a git working tree, which
  is how a `.hydra-events/` directory once got committed. See
  `docs/architecture/filesystem-layout.md`.
- **Per-bead worker logs rotate daily with `maxsize 64M`** instead of weekly with
  `rotate 2`, which could not contain a directory growing at ~15 GB/day.
- **Pi config consolidated to one shared directory.** `pi.worker_config_dir` now
  equals `pi.config_dir` (`/opt/dev-tools/config/pi`), so hydra workers and human
  shells resolve the same skills and extensions. The old generated copy at
  `/opt/dev-tools/config/pi-worker` had drifted to 239 npm packages against the
  canonical 242 with nothing detecting it, and both of its stated purposes were
  obsolete. `scripts/setup-pi-worker.sh` is removed. Operators: see
  `docs/architecture/pi-config.md`.
- **Concurrency governor steps down by one instead of halving**, and gains
  clock-driven recovery (`recovery_interval_seconds`, default 300). A single
  one-hour provider outage previously took the limit 4 -> 2 -> 1 and left it at 1
  for 2.5 hours, because recovery required a full outcome window and a low limit
  suppresses outcomes. Exit 7 no longer feeds the governor.

### Added
- **`pi.manage_config`** (default `false`): whether the daemon may WRITE to
  `pi.config_dir` — create structure, install Pi/LSP, refresh skills on a timer.
  Must stay false whenever the directory is shared with human shells, or daemon
  startup reshapes the team's environment.
- **`scripts/verify-pi-config.sh`**: asserts the two config dirs match, the
  directory is a Pi package root, every allowlisted extension resolves, each
  `~/.pi/agent` points at it, and no retired copy is still referenced.

### Documentation
- **Updated `docs/operations/handoff-20260731.md`** to the 2026-08-03 state:
  `development` now at `b47b527`, `go test ./...` and `go vet ./...` green,
  integration tests gated behind the `integration` build tag, and open work
  trimmed to the remaining runtime/dogfood items.
- **Rewrote `docs/operations/handoff-20260731.md`** for the 2026-08-02 state
  of the box: live park-and-resume, registry GC, Charon liveness, timeout
  hierarchy, Pan status, verification commands, and the remaining open work.
- `docs/architecture/shepherd.md` gained a section on the worker registry,
  park-and-resume, the verdict protocol, Pan, liveness-based Charon, and the
  timeout hierarchy with its env knobs (`HYDRA_API_URL`, `HYDRA_MAX_NUDGES`,
  `HYDRA_MAX_PARK_MINUTES`, `HYDRA_WORKER_LIFETIME_MINUTES`).

### Changed
- **Retention stores the HYDRA_VERDICT, not raw run output** (Hindsight LLM
  Sink A). The extraction LLM (MiniMax) used to receive the full turn text —
  tens of KB including thinkingSignature blobs — per run. Retain now sends
  the parsed verdict (~200 tokens: done, ac_met/ac_unmet, files_changed,
  verification) when one exists, and sanitized output (thinkingSignature and
  base64 runs stripped, 4KB tail-biased cap) otherwise. Memory quality
  improves because extraction sees distilled signal; failure evidence is
  preserved (sanitized tail + the A2 cause extractor reads the worker log
  separately and is untouched).

### Added
- **Workers honor shepherd commands at park wake** (PRD Part 3 consumer).
  Commands were polled over the registry API but never drained — a steer or
  abandon from Pan had no effect. The park-wake path now drains: `steer` and
  `continue`/`compact-and-continue` reshape the wake prompt;
  `abandon-with-summary` fails the turn with `abandoned by shepherd: …`
  (deliberately un-parkable) and closes stdin so pi exits.

### Fixed- **Pan example config no longer sets `thinking: high` for the MiniMax provider.** The `pi` CLI rejects `--thinking-level` for the `minimax` provider, causing Pan invocations to fail on startup; the example config and the shepherd load test now omit the field. Live `/etc/hydra/hydra.yaml` updated.

- **`shepherd.pan` config silently parsed to zero values.** `LoadConfig`
  copies `rawConfig` fields one by one; `Shepherd` (and in
  `LoadConfigUncached`, also `Validation`/`ComplianceCycle`) was declared on
  `Config` but never copied, so Pan stayed disabled in production with no
  error. Added the missing copies plus a load-survival regression test.
- **Charon no longer kills live workers on wall-clock time.** The stuck-worker
  sweep used a 15m wall timeout and on 2026-08-01 killed every fleet worker
  at ~15m: workers parked on provider 403s, and one worker while it was
  emitting its closing `HYDRA_VERDICT`. The sweep is now registry-aware
  (PRD parts 4–5): parked workers and workers with a fresh heartbeat (30s
  reporter) are alive by definition and never killed on time alone; only a
  stale heartbeat (>5m) means hung. Workers without a registry row keep the
  legacy wall-clock behavior.
- **Worker lifetime cap replaces the 15m total-runtime kill.** The worker's
  internal max-runtime (15m) could fire mid-verdict on long TDD chains; it
  is now a 2h monotonic lifetime cap (`HYDRA_WORKER_LIFETIME_MINUTES`),
  parked time excluded. The 4m activity timeout stays the real hang
  detector.
- **Charon fleet deadlock: phantom worker records blocked orphan-bead recovery
  forever.** A worker that died without cleanup left a `running` record in
  `workers.json`; `recoverOrphanInProgressBeads` then believed a live worker
  existed and skipped the bead, while the dead-PID sweep that would clear the
  phantom sat behind `getOpenBeads`, which aborted the whole scan whenever any
  one project's `br list` failed (lib-nestjs/demo exit 2). Result on
  2026-07-31: 11 beads stuck `in_progress`, `ReadyNodes:0 BlockedNodes:14`,
  fleet idle for hours. The dead-PID sweep now runs first, before any br
  dependency; `getOpenBeads` skips failing projects instead of aborting; and
  the orphan-kill decision is fail-closed per project — a worker whose project
  cannot be listed is never killed on the strength of a partial open-bead map.
- **`deploy.sh` no longer overwrites live configuration.** `create_config`,
  `create_systemd_service`, `setup_logrotate` and `setup_firewall` wrote canned
  templates on every run. On 2026-07-31 an ordinary deploy replaced the tuned
  `/etc/hydra/hydra.yaml` with a 749-byte template — losing `max_workers`, the
  provider pool, the governor, the shared Pi config, hindsight and the project
  list — and replaced the systemd unit with one missing
  `EnvironmentFile=/etc/hydra/api-keys.env` and the beads binaries on `PATH`. All
  four steps are now no-ops when the target exists, and require
  `HYDRA_PROVISION=1` on a genuinely fresh box.
- **Per-bead worker logs are capped at 32 MB per stream.** `logs/workers/` had
  reached 30 GB across 1,590 files in two days, with single beads writing 853 MB
  of pi stream JSON. A capped log says so in the file, so it cannot be mistaken
  for complete output.
- **Tests no longer write to production state.** `worktreeRoot()` roots worktrees
  under `$HYDRA_HOME`; the daemon exports it and workers inherit it, so a worker
  running this repo's own suite registered every test worktree in live state —
  10,251 dead entries, each named after a Go test function, which Charon walked on
  every orphan sweep. `TestMain` now pins `HYDRA_HOME` to a per-run temp dir.
- **Retry cap had never fired.** Retry state was cleared on successful *spawn*
  rather than completion, so the count could never reach 2. Provider failures
  (429/timeout/overload/quota) now pace retries without consuming the budget;
  only genuine bead faults exhaust it.
- **Daemon log no longer receives pi's JSON stream** (1.56 GB in 4h50m), which
  also made unanchored log greps report failures that never happened.
- **`deploy.sh` builds packages, not single files** — `go build cmd/worker/main.go`
  ignored sibling files and broke the worker build.
- **Nightly testbed matrix counted `"0\n0"`** from `$(grep -c ... || echo 0)`,
  which disabled its own starvation guard, and its daemon-log filter counted
  worker passthrough (reporting 20 gap-to-prd cycles where the daemon logged 0).

### Removed
- **llm-tldr, entirely.** It went unmaintained, returned zero semantic units on
  Go (silently degrading to grep), and its service on :8787 was not running in
  production — so the structural-intelligence layer had been dead for a while
  without anything failing loudly. graphify replaces it.
  Deleted: `internal/intelligence/tldr.go`, `internal/core/tldr_indexer.go`,
  `internal/core/impact_analyzer.go` (its only caller, `analyzeImpactWithTLDR`,
  was already dead code), the hub's client/indexer wiring, the `ToolTLDR`
  refresh path, `TLDRConfig`/`TLDRIndexingConfig`, and the GraphQL tldr
  plumbing. Two consequences worth naming rather than burying:
  - `ChainPatternExtractor` no longer runs a semantic search per chain. That
    call is the source of the recurring `TLDR indexing failed … after 4
    attempts: indexing timed out after 30 seconds` in the daemon log, so this
    also removes a standing 30s-per-chain stall.
  - GraphQL `Bead.impact` now returns nil. The field and its nullable schema are
    kept, so no API contract changes — and nil is what production already
    returned, since the backing service was down. Wiring it to
    `graphify affected` is a follow-up: real reverse traversal, not a rename.
  `ScopeValidator` no longer borrows its file limit from
  `intelligence.tldr.max_files`; that coupled a scope invariant to a tool's
  config and silently disabled the check whenever the tool was disabled. It is
  now the `maxScopeFiles` constant.

### Added
- **Verdicts feed memory (Shepherd D3, dogfood-built + hand-finished).** The
  system's most expensive judgments no longer evaporate: fresh gap
  adjudications (real/false_positive with reasoning) and failed closes with an
  extracted cause are retained as Hindsight memories at judgment time, and the
  adjudicator now sees up to 3 prior verdicts on similar gaps in its prompt.
  Ambiguous verdicts and cause-less failures are never retained (noise, not
  memory); cached fingerprints never re-retain. Fail-open everywhere — a
  Hindsight outage costs memories, never runs. New event: `verdict_retained`.
- **AIMD concurrency governor (Shepherd B1, dogfood-built).** When provider
  health degrades, the fleet's own request pressure raises its exposure to the
  overload it is stalling on (219 activity timeouts vs 4 clean turns on
  2026-07-28). New `ConcurrencyGovernor` applies TCP-style congestion control
  to worker slots: halve effective concurrency when ≥50% of the last 8
  provider-shaped outcomes failed, regrow one slot per quiet 2-minute window,
  floor 1 — never a hard stop. Only exit codes 2/7 and activity timeouts count
  as failures; a bead failing on its own defect (exit 1/5) never shrinks the
  fleet. Config `overseer.concurrency_governor` (default disabled = byte-
  identical behaviour). Built end-to-end by a hydra dogfood run (commit
  authored by the fleet, harvested after gates closed).

### Fixed
- **A concurrency ceiling was being classified as quota exhaustion**, so the
  daemon froze every spawn for 30 minutes over a condition that clears in
  seconds. Kimi returns "You've reached your concurrent request limit" with the
  same HTTP 403 and `access_terminated_error` type as genuine quota
  exhaustion, and only the message distinguishes them — so the quota markers
  matched. On 2026-07-30 that produced 37 fleet-wide pauses in one day and
  starved a dogfood run to roughly a 10% duty cycle while the account's quota
  was never touched; a single sequential probe against the same provider
  succeeded. Concurrency refusals now classify as exit 7, which feeds the AIMD
  governor (thin the fleet) instead of the circuit breaker (freeze it) — the
  response the provider's own message asks for.
- **Fix-bead dedup was structurally dead.** `br list --json` returns
  `dependencies: null` (only `dependency_count`), so the dependency-based
  duplicate check never matched — `fix_deduped` events stayed at zero across
  every run and run D3 created four identical fix beads for one original.
  Dedup now matches by the deterministic fix title ("Fix: <original title>")
  first, dependencies second.
- **Cause extractor no longer harvests stringified source code.** A worker
  implementing a log parser echoed code containing `--- FAIL:` into its log
  and the extractor shipped that as the "cause" (run D3). Failure markers now
  anchor at payload start and lines carrying literal escape sequences
  (`\n`, `\t`, `\"`) are rejected as echoed source.
- `DefaultEventsDir` no longer falls back to a cwd-relative `.hydra-events/`
  when HYDRA_HOME is unset — tests were writing event day-files into the repo
  (one got committed). Unset HYDRA_HOME now disables the central stream.
- **Failed chains quarantine instead of integrating (Shepherd A1).** Chain
  "completion" only ever meant every bead reached a terminal state — closed-as-
  failed counted the same as completed, so failed chains merged into the working
  branch exactly like successful ones (a deliberately red test landed on the
  testbed's branch this way). Failed closes now get a durable `closed-failed`
  label (issues.jsonl carries no close reason; a label survives daemon restarts
  where in-memory state would not — four mid-run restarts this week),
  `ChainHasFailedBeads` reads it at completion time, and failed chains go to
  `hydra/quarantine/<key>` — work preserved for the fix bead to inspect, working
  branch untouched. Emits `chain_quarantined` and `bead_closed` events.
  `BeadFileReader` now parses labels (previously dropped them entirely).
- **Cause-aware, deduplicated fix beads (Shepherd A2).** A fix bead's description
  now carries the original's exit code plus a `What failed last time` block
  extracted from its newest worker log (`ExtractFailureCause`: priority-ordered —
  model failure > failing test > package FAIL > Error — passthrough JSONL
  excluded except the model-failure marker, 2 KB budget). Creation is deduplicated
  via `findOpenFixBeadForOriginal`: one open fix bead per original, closing the
  duplicate-spawn defect (the A2 build run itself produced SEVEN duplicate
  cause-less fix beads for one bead before this landed — the problem demonstrating
  its own fix). Built by dogfood run A2 (FR-001 initially stubbed; a later fix
  bead completed it), harvested from the working tree since both gates correctly
  closed `failed` on the mid-run mess.
- **Structured daemon event stream (Shepherd C1/C2).** Typed JSONL written by
  daemon code about daemon decisions — worker output structurally cannot enter
  it, retiring the passthrough-contamination class (4 incidents). Two tiers:
  central day-files under `HYDRA_HOME/events/`, and per-repo
  `.hydra/events.jsonl` mirrors (gitignored) so each user sees what hydra did to
  their repo. First emitters wired in the closer: `fix_created`, `fix_capped`,
  `fix_deduped`.
- **rpc mode and the `[rtk, caveman, context-mode]` allowlist are now the
  defaults.** A config that says nothing about the protocol gets both. rpc is the
  default because text mode initialises no extension host at all — verified: a
  marker extension loaded with `-e` in text mode is imported but nothing runs and
  `pi --verbose` lists no extensions, while over rpc the same setup produces
  `RTK rewrite: git status --short -> rtk git status --short`. The allowlist is
  the default because Pi's own discovery loads every installed package (48,315
  tokens on one measured turn versus 12,059 for the allowlist), and the extras —
  themes, editor widgets, subagent UI — cannot be used by a headless worker.
  Opt out with `mode: text` or `extensions: []`; both are explicit values, and
  the defaulting distinguishes them from an omitted key. Defaults are applied
  *after* the raw config is copied, because `LoadConfig` assigns
  `config.Agents = raw.Agents` wholesale — anything set in `DefaultConfig` would
  have been silently discarded by any file containing an `agents:` block.
- Pi rpc mode is now actually wired to the worker execution path. `agents.pi.mode`
  previously existed, parsed, and had a tested `RPCEnabled()` accessor while
  changing nothing: beads run in a separate `hydra-worker` process that
  hand-builds its own `pi --offline --no-session` command, so the daemon's mode
  never reached the process that spawns Pi (and `spawnWorkerViaAgent`, the only
  in-daemon caller of the rpc connector, is dead code). `buildPiEnv` now forwards
  `HYDRA_PI_MODE` / `HYDRA_PI_RPC_AUTO_CONFIRM`, and the worker drives the rpc
  protocol: keeps stdin open (UI-ask replies travel back over it — closing it is
  what made earlier attempts hang), reads the verdict from the event stream
  rather than the exit code (the worker kills Pi itself after `agent_end`, since
  Pi waits for another prompt instead of exiting), treats an `agent_end` carrying
  `stopReason: error` as a failure, and logs per-bead token counts from
  `get_session_stats`. Text mode is untouched and remains the default.
  Verified end-to-end on the VPS: `--mode rpc` in the argv, `agent_end` reached,
  file actually written by the model, worker exit 0.
- **Measured, and it does not yet pay off.** Same bead, three configs:
  rpc with the worker config dir → 10,194 tokens / 18.0s / **no extensions
  loaded**; rpc without it → 61,711 tokens / 87.8s / extensions **loaded**
  (`notify=3 setStatus=4 setWidget=1`); text mode → 14.7s. The premise for rpc
  was that rtk/context-mode/caveman cut tokens; loading them cost **6x more
  tokens and ~5x more wall-clock** on this task. Cause of the split: setting
  `HYDRA_PI_CONFIG_DIR` makes the worker export `PI_CODING_AGENT_DIR`, and with
  that set `pi list` reports "No packages installed" — so today a worker can have
  its skill flags **or** its extensions, not both. Do not enable
  `agents.pi.mode: rpc` expecting savings until that is resolved and measured on
  real beads.
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
- `failed_closes` in the eval metrics was counting the model's own words. The
  daemon streams worker stderr into its own log, and that payload is raw JSONL
  including `thinking_delta` text, so a substring marker matched whatever the
  model happened to write. On run H the `closed as failed` marker matched 7,884
  lines — **7,739 worker passthrough, 145 genuine `[closeBead]` lines** — and the
  metric was reported as 6,707. Marker counting now skips `[stderr]`/`[stdout]`
  passthrough lines. This affected every marker-derived metric, not just this one.

### Changed
- Default Pi extension allowlist is `[rtk, context-mode]`. context-mode was
  dropped a few hours earlier on the strength of one trivial prompt where it only
  added tokens; that prompt gave it nothing worth sandboxing, so the measurement
  was wrong. Its own stats across 155 worker sessions report **4,401,021 bytes
  sandboxed against 485,148 returned and 1,249,024 tokens saved** — lumpy (median
  3,385 per active session, one session 1,095,981), earning its place by
  occasionally avoiding an enormous context load rather than shaving a steady
  percentage. caveman stays out: at ultra it produced **1,103 output tokens against
  927 without it**, plus ~8,500 input tokens of its own instructions, because it
  compresses assistant prose while exempting code — and a worker's output is
  mostly tool calls and code.
- **Gap-churn: fix beads no longer spawn fix beads.** The recursion guard allowed
  three levels, which is how run H produced titles like
  `Fix: Fix: Fix: NFR-001: The impact path stays cheap and offline` and **158 fix
  beads against 18 real tasks**. A fix bead exists to redo work that failed; if the
  fix itself fails, another copy re-runs identical instructions through the
  mechanism that just failed, burning a worker slot and provider quota per attempt
  while burying the original gap. Capped at one fix bead per original
  (`maxFixNesting = 1`).
  Refusal is now also **labelled** (`fix-recursion-capped`). Previously the refusal
  path returned silently, so the bead closed as failed and its dependents unblocked
  — the exact regression `close_blocked_on_failed` exists to prevent — with no
  record of why no fix bead appeared.
- `compliance_cycle.max_cycles` defaults to **2**, down from 5, in both
  `DefaultConfig` and the runtime fallback. Each cycle re-runs the analyzer,
  re-adjudicates and decomposes surviving gaps into a fresh bead chain, so cost
  multiplies rather than adds. Run H spent 5 gap-to-prd invocations with the later
  cycles re-litigating findings rather than discovering new ones. Two still covers
  analyze → fix → verify.
- **A hung provider request no longer costs a worker ten minutes.** In rpc mode
  the activity timeout is now 4 minutes rather than 10, because the two protocols
  emit at very different densities: text mode streams prose and a long tool run
  can legitimately go quiet, while rpc emits an event for every step
  (`tool_execution_*`, `message_update`, `extension_ui_request`), so silence means
  the request has hung. Measured 2026-07-28: a worker logged "prompt sent over rpc"
  at 07:50:09 and emitted nothing until the timeout fired at 08:00:29, while the
  next attempt received a 429 in five seconds — and Pi's own retry backoff is
  `delayMs: 2000`, so a ten-minute gap is never Pi waiting politely. Waiting it
  out wasted a worker slot; across five slots the run stopped closing beads
  entirely (0 in 45 minutes, against 8 in the preceding text-mode window).
  `HYDRA_WORKER_ACTIVITY_TIMEOUT` still overrides both.
- **A transient provider blip no longer freezes the fleet for fifteen minutes.**
  Kimi returns "The engine is currently overloaded, please try again later" as a
  429 with `type: rate_limit_error`. That was classified as a rate limit (exit 2),
  fed the circuit breaker, and three of them inside ten minutes opened the circuit
  for fifteen — and because `fallback_provider` equals the primary there was
  nothing to fail over to, so the spawn gate paused every worker
  (`skipping spawns for 11m32s`) over an error whose own message says to retry
  shortly. Pi already retries it internally. Capacity errors now classify as
  **exit 7**: retryable infrastructure (so no fix bead is spawned) that never
  touches the provider circuit. Genuine throughput rate limits still classify as
  2, and quota exhaustion still wins with 6.
- The breaker no longer claims a failover it cannot perform. With
  `fallback_provider` equal to the primary it logged "failing over to
  kimi-coding" while every worker was in fact paused; it now says so and points at
  the config.

### Changed
- **Default Pi extension allowlist is `[rtk]`, not all three.** Measured over
  three sequential trials of one prompt: no extensions ~12,271 tokens/turn, rtk
  ~16,360 (+33%), rtk + caveman + context-mode ~26,679 (+117%). context-mode is
  tool-call-shaped by design — one dogfood run logged 1,292 `ctx_*` calls against
  85 rtk rewrites — and every tool call is another request, which raises exposure
  to provider capacity errors under five concurrent workers. rtk is kept because
  it compacts tool *output*: bounded cost, saving that scales with command noise.
  caveman and context-mode are plausible but unmeasured; enable per project with
  `extensions: [rtk, caveman]` once there is data rather than fleet-wide.
- **Workers ran with zero Pi extensions, in both protocol modes.** The worker
  exported `PI_CODING_AGENT_DIR=<config_dir>/agent`, but on the deployed layout
  the package set lives in `<config_dir>` itself
  (`pi-worker/settings.json` + `pi-worker/npm/node_modules`, 239 packages);
  `<config_dir>/agent` holds neither. Verified on the box with the same binary and
  only that variable changed: `<cfg>/agent` -> `pi list` reports "No packages
  installed", `<cfg>` -> 13 packages including `pi-rtk-optimizer@0.9.0`,
  `caveman-milk-pi`, `context-mode`, `@aliou/pi-guardrails`. So no rtk command
  rewriting, no context-mode, no caveman, no guardrails — for every bead Hydra has
  ever run. It also explains why rtk's own savings counter on that host had not
  moved since 06-26 despite workers making nearly all the tool calls.
  `resolvePiAgentDir` now probes for the directory that actually holds the package
  set, preferring `<cfg>/agent` so the developer-machine layout
  (`~/.pi/agent/...`) is unchanged, and falling back to the historical value when
  neither looks like a package root.
- Hindsight recall no longer filters by bead type, which had silently switched
  off the recall half of the learning loop. Hindsight's `types` are memory kinds
  (`experience`, `observation`, `world`); bead types (`compliance`, TDD phases)
  match none of them. The old code sent the bead type under the singular key
  `type`, which the API does not accept, so it was dropped and recall stayed
  unfiltered — correcting the key to `types` in this same branch activated a
  filter that had never actually run. Measured on the live bank: no filter → 12
  results, `types:["compliance"]` → 0. Every worker after the mid-run deploy went
  from `Retrieved 5 relevant memories` to `No relevant memories found`, with no
  error logged anywhere. Caught by comparing pre/post-deploy worker logs, not by
  a test — so there is now a test asserting the request carries no `types` key.
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
