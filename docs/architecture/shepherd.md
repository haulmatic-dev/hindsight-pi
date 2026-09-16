# The Shepherd Layer — Events, Quarantine, Fix Beads, Governor, Verdict Memory

**Status:** living document · **Updated:** 2026-08-03 · shipped tracks: A1 A2 B1 C1 C2 D3 + worker registry / park-and-resume / named sessions / verdict protocol / liveness-based Charon / scheduler congestion pause / Pan

The Shepherd layer is the set of mechanisms that keep an autonomous fleet
honest and observable: every significant decision leaves a durable record,
failure is bounded and preserved rather than merged or lost, and the system's
own judgments feed its memory. This page is the operational reference for the
pieces and how they connect. The loop-level view lives in
[`system-loop.md`](system-loop.md).

```
worker fails ──▶ closed-failed label ──▶ cause extracted ──▶ fix bead (capped, deduped)
     │                    │                     │
     │                    ▼                     ▼
     │          chain completion check    Hindsight memory (D3)
     │                    │
     │           any failed member?
     │            yes ▼        ▼ no
     │      quarantine branch  integrate
     │
     └──── every step above emits a typed event (C1/C2)
```

---

## 1. Event stream (C1/C2) — `internal/core/events.go`

Typed JSONL records of daemon decisions. The stream **observes the system and
must never break it**: nil sinks, missing dirs, and failed writes are silent
no-ops; a torn final line (crash mid-append) never poisons a reader.

**Two tiers, same records:**

| Tier | Path | Audience |
|---|---|---|
| Central | `$HYDRA_HOME/events/YYYY-MM-DD.jsonl` (day files) | cross-run analysis, the shepherd cron (D1), hydra-eval |
| Per-repo | `<repo>/.hydra/events.jsonl` | the repo's own users — "what did hydra do to my repo and why" |

The per-repo mirror is auto-gitignored (the sink appends `.hydra/` to the
repo's `.gitignore`). With `HYDRA_HOME` unset (dev runs, `go test`) the
central stream is disabled entirely — an earlier cwd-relative fallback wrote
day-files into whatever directory constructed an overseer.

**Record shape** (`Event`): `time` (RFC3339, stamped by the sink), `type`,
`project`, `bead`, `reason`, `detail`, `fields` (free-form map). A typeless
event surfaces as `type:"unknown"` rather than vanishing.

**Event types emitted today:**

| Type | Emitted when | Key fields |
|---|---|---|
| `bead_closed` | any close with reason | `exit_code`, `success` |
| `fix_created` | failed close spawns a fix bead | `original`, `exit_code` |
| `fix_deduped` | an open fix for the same original already exists | `existing_fix` |
| `fix_capped` | fix-of-fix refused (recursion cap) | — |
| `chain_quarantined` | failed chain routed to quarantine | quarantine branch |
| `verdict_retained` | a judgment was stored in Hindsight | `kind` (`adjudication`/`failed_close`), verdict or exit code |
| `satisfied_closed` | Pan closes a bead whose acceptance criteria are already met on the working branch | `tokens` (`0`) |
| `retry_exhausted` | a bead exhausts `default_retries` and has no live worker or registry row | `attempts` |
| `spawn-suppressed-closed` | a queued retry spawn is aborted because the bead became terminal while in the retry queue | `status` |
| `integration-failed` | singleton integrate/push failed verification (merge not contained in HEAD, or push rejected / remote ref did not advance) — bead reopened with `integration-failed` label, worktree preserved | `stage` (`integrate`/`push`), `error` (exact remote stderr) |
| `worktree-preserved-unintegrated` | janitorial GC refused to remove a stale worktree: its branch carries commits not yet in the working branch, it has uncommitted tracked changes, or files under it were modified after creation and within the staleness window (merge-resolution edits, untracked output — hydra-q3p mid-merge reap, 2026-09-12) — reconciler integrates on its next sweep | `worktree`, `branch`, `cause` (`unintegrated_work`/`recent_activity`) |
| `bead_store_suspect` | a project's open-bead count collapsed to 0 or >80% in one discovery tick (or `br list` failed after a healthy baseline) — store presumed corrupt, project excluded from scheduling | `project_path`, `previous`, `current`, `list_error` |
| `bead_store_restored` | `.beads/issues.jsonl` rebuilt from the newest `.br_history` snapshot and the re-listed open count verified recovered — scheduling unblocked | `project_path`, `snapshot`, `open`, `baseline` |
| `bead_store_restore_failed` | snapshot restore could not verify recovery (no snapshot, copy error, re-list failure, or count still collapsed) — project stays blocked | `project_path`, `snapshot`, `error` |

Read programmatically with `ReadEvents(path)` (torn-line tolerant; a missing
file is an empty stream, not an error).

---

## 2. Close verdicts & quarantine (A1) — `bead_closer.go`, `worktree_manager.go`, `chain_completion_*.go`

Singleton close is fail-closed end to end (2026-09-06, hydra-xnx incident): the
merge is verified (`git merge-base --is-ancestor <wt-branch> HEAD`) before
"integrated singleton" is logged, the push is verified (`ls-remote` ref match
inside `PushBranch`) before "pushed" is logged, and the worktree is removed
only after both land. Either failure emits `integration-failed`, reopens the
bead, and leaves the worktree in place. Three root-cause guards sit underneath:
`EnsureWorktree` reuse-reset only fires when the worktree tip is already
contained in HEAD (an unguarded reset rewound branches holding gate-verified
worker commits, after which the integrate silently no-opped); the post-add
reset after reattaching an existing branch is guarded the same way — skip when
the branch tip carries commits not in HEAD, since that path otherwise orphaned
work on every respawn after path cleanup (2026-09-08 srv slice wipe loop); and
push
rejections return the exact remote stderr (GH006 included) instead of a
collapsed "protected branch" summary. A rejected push routes to `maybeOpenAutoPR`
when `output.auto_pr` is on; a created PR preserves the work via the dogfood
branch and the close proceeds, otherwise the close fails. When branch
protection on the working branch is permanent, `output.pr_merge: true` skips
the futile direct push entirely: the publisher force-pushes HEAD to
`dogfood/<bead-id>`, runs `gh pr create` + `gh pr merge --merge --admin`, and
only logs "landed via PR" after `gh pr view` reports MERGED — a
created-but-unmerged PR fails the close closed, same as a rejected push. The
periodic `PublishRepo` tick uses the same PR route with a
`dogfood/publish-<project>` branch when `pr_merge` is set.

The beads store (`issues.jsonl`) has **no close-reason field**, so the close
verdict is made durable as a label: a bead that closes `failed` gets
**`closed-failed`** stamped on it. A label survives daemon restarts where any
in-memory map would not (production restarts mid-run are routine — apt
unattended-upgrades restarts the daemon around 06:11 daily).

At chain completion, `ChainHasFailedBeads` reads those labels and decides:

- **Clean chain** → `IntegrateAndRemove`: squash-merge the chain worktree into
  the working branch, as before.
- **Any `closed-failed` member** → `QuarantineAndRemove`: the chain's commits
  are preserved on **`hydra/quarantine/<key>`**, the worktree and its
  `hydra/wt-<key>` branch are removed, and **nothing merges**. Emits
  `chain_quarantined`.

Why: chain "completion" used to mean only "every bead reached a terminal
state" — a chain whose beads closed *failed* integrated exactly like a
successful one. A known-answer testbed run proved it by landing a
deliberately red test on the working branch.

The same durability rule applies to **escalation**: when verified-close
rejects a bead at max rounds, the bead is left `in_progress` (parked, not
reopened) and stamped **`escalated`** (with `needs-human` and
`verified-close-escalated`), and a typed **`bead.escalated`** event is
emitted. Every consumer that decides respawn must skip labeled beads:
Charon orphan recovery (`[OrphanRetry] ... skipping reopen`), the scheduler
ready filter (`GetReadyBeads`), and the retry-eligible path. Without the
label, bare `in_progress` is exactly the signal orphan recovery keys on and
the bead respawns into the same death the escalation stopped (2026-09-06
empty-diff churn loop: c01hq/65xy7/48bdq).

**Worktree-unavailable hard-park (2026-09-13).** The same durability rule now
covers the spawn path: when `EnsureWorktree` fails (dubious-ownership,
foreign-repo path, unresolvable base), `SpawnInternal` fails the spawn
CLOSED for every project — there is no shared-checkout fallback. The prior
fallback ran the worker loose in the user's main checkout; observed
2026-09-11/12: merge-conflict markers committed into `webapp/.beads/issues.jsonl`,
a project's `.beads` dir wiped by a `git clean -fdx` "fix the repo" worker,
janitorial commits landing on the user's working branch. The bead is stamped
**`worktree-unavailable`** and every respawn consumer skips it (retry queue,
`GetReadyBeads`, Charon orphan recovery), exactly like `escalated`. Recovery
is human: fix the worktree (Pan's whitelisted `repair-worktree` repair runs
`Remove`+`EnsureWorktree` daemon-side), then clear the label.

**Worktree-unavailable hard-park (2026-09-13).** The same durability rule now
covers the spawn path: when `EnsureWorktree` fails (dubious-ownership,
foreign-repo path, unresolvable base), `SpawnInternal` fails the spawn
CLOSED for every project — there is no shared-checkout fallback. The prior
fallback ran the worker loose in the user's main checkout; observed
2026-09-11/12: merge-conflict markers committed into `webapp/.beads/issues.jsonl`,
a project's `.beads` dir wiped by a `git clean -fdx` "fix the repo" worker,
janitorial commits landing on the user's working branch. The bead is stamped
**`worktree-unavailable`** and every respawn consumer skips it (retry queue,
`GetReadyBeads`, Charon orphan recovery), exactly like `escalated`. Recovery
is human: fix the worktree (Pan's whitelisted `repair-worktree` repair runs
`Remove`+`EnsureWorktree` daemon-side), then clear the label.

**Inspecting quarantined work:**

```bash
git -C <repo> branch --list 'hydra/quarantine/*'
git -C <repo> show hydra/quarantine/<key> --stat   # what the failed chain built
git -C <repo> diff main...hydra/quarantine/<key>   # full evidence for the fix bead
```

Re-quarantining the same chain key overwrites the branch — the newest failed
attempt is the one worth reading. Recovery is a human/fix-bead decision:
cherry-pick what is salvageable; the branch is evidence, not a pending merge.

---

## 3. Fix-bead lifecycle (A2 + churn caps) — `bead_closer.go`, `fix_cause.go`

A failed close (with `close_blocked_on_failed`, default on) spawns **one** fix
bead that keeps dependents blocked. The lifecycle has four guards, each born
from a measured pathology:

1. **Cause-aware (A2):** `ExtractFailureCause` reads the failed worker's log
   (`$HYDRA_HOME/logs/workers/<beadID>-<pid>.log`, newest by mtime) and embeds
   a `### What failed last time` fenced block in the fix bead's description —
   the retry starts from evidence, not from zero. Verified live: 26/26 fix
   beads in run B1 carried cause blocks.
2. **Deduped:** an open/in-progress fix bead for the same original blocks a
   duplicate (`fix_deduped` event). *Known gap:* fix beads created by the TDD
   validation path bypass this — run D3 produced five identical FR
   implementations from duplicate discovery-fix beads.
3. **Recursion-capped:** a fix bead for a fix bead is refused at depth 1; the
   refusal is visible as the `fix-recursion-capped` label and a `fix_capped`
   event. Against a seeded impossible requirement this took churn from 158
   beads (3-deep nesting) to 25 (0 nesting).
4. **Infra-shielded:** infrastructure exits (rate limit 2, timeout 3, infra 4,
   quota 6, transient overload 7) never spawn fix beads — the scheduler
   retries with backoff; only code-level failures earn a fix bead.

*Known gap:* the cause extractor matches failure markers (e.g. `--- FAIL:`)
anywhere in the log, including inside source code the worker echoed while
writing files — anchor on the log's line structure before trusting a block.

---

## 4. Concurrency governor (B1) — `concurrency_governor.go`, `scheduler.go`

Additive-increase / additive-decrease over worker slots. When the provider
degrades, the fleet's own request pressure raises its exposure to the very
overload stalling it (measured: 219 activity timeouts vs 4 clean turns in one
storm). The governor thins the fleet instead of freezing it:

- **Signal:** worker completions. Only provider-shaped outcomes count —
  success, or failure with exit 2 (429) or 3 (timeout). A bead failing on its
  own defect (exit 1/5) says nothing about provider health and never shrinks
  the fleet. **Exit 7 is excluded**: it is a capacity blip that clears in
  seconds, and feeding it in made the governor and the provider pool two
  controllers fighting over one actuator.
- **Decrease:** window of 8 outcomes ≥ 50% failures → limit − 1, floor 1.
  Never a hard stop: one worker always proceeds.
- **Increase:** ≤ 1 failure in a full window and ≥ 120s since last change →
  +1 slot, up to `max_workers`.
- **Quiet recovery:** no provider-pressure failure for `recovery_interval_seconds`
  (default 300) → +1 slot, *regardless of how many outcomes have been seen*.
  Driven from the scheduler tick, not from outcomes.
- **Read:** scheduler computes slots from `min(max_workers, Limit())`.
  Every change logs `[Governor] concurrency N -> M (reason)`.

*Why the last two rules look like that.* The original design halved on decrease
and recovered only on a full window of outcomes. Both failed together on
2026-07-31: a single one-hour provider capacity window took the limit 4 → 2 → 1,
and it then stayed at 1 for the remaining 2.5 hours on one completion. Halving
overshoots at the small limits we actually run at, and recovery gated on outcome
volume cannot undo a deep cut, because the cut is precisely what suppresses
outcome volume. A lower limit yields fewer completions, so the window never
refills, so the limit never rises.

Config (default **disabled** — absent block is byte-identical to no governor):

```yaml
overseer:
  concurrency_governor:
    enabled: true        # prod: enabled 2026-07-29
    window_size: 8       # optional, defaults shown
    decrease_threshold: 0.5
    increase_threshold: 0.125
    cooldown_seconds: 120
    recovery_interval_seconds: 300
```

Composes with the provider circuit breaker: the breaker pauses spawns for a
*dead* provider (quota, sustained 429s); the governor thins them for a
*degraded* one. The breaker dominates when both fire.

---

## 5. Worker registry, park-and-resume, verdict protocol (2026-08-01) — `internal/registry`, `cmd/worker/reporter.go`, `cmd/worker/park.go`, `cmd/worker/verdict.go`, `internal/pan`

Built to stop the failure that autopsied as `hydra-testbed-y05`: provider
congestion (Kimi 403/429) killed workers mid-turn, the work was rejected as
`empty-diff`, and the fleet re-did it from zero — ~50 minutes of compute,
nine worker processes, zero merged output. Design:
[`docs/PRDs/worker-park-and-resume-20260731.md`](../PRDs/worker-park-and-resume-20260731.md).

**Worker registry (daemon-owned, single writer).** SQLite at
`$HYDRA_HOME/.hydra/registry.db`. Workers report over the existing API
(`:8089`): `POST /api/v1/workers/report` (register + heartbeat + state
transitions), `POST /api/v1/workers/completed` (terminal), and poll
`GET /api/v1/workers/commands?bead=...` (consume-on-read steering commands:
`continue`, `steer`, `compact-and-continue`, `abandon-with-summary`). The
worker honors them at park wake: steer/continue reshape the wake prompt,
abandon-with-summary fails the turn (`abandoned by shepherd: …`) and closes
the session instead of resuming. The daemon sets `HYDRA_API_URL` when spawning;
without it the worker never reports and behaves exactly as before (fail-open).
The store opens in the service starter, not in `NewOverseer`, so tests never
touch the filesystem. On startup it reaps terminal rows and rows whose last
heartbeat is older than 24 h (`Store.ReapStale`), plus their queued commands,
so Pan and the scheduler never act on zombie state from prior runs. Route
handlers resolve the store **per request** — a store captured at
route-registration time is nil because the store opens later; that bug shipped
and returned 503s until fixed.

**Worker reporter.** `cmd/worker/reporter.go`: registers on start, heartbeats
every 30s, mirrors every park/nudge state change, reports terminal state on
exit. Every failure is logged-and-continue — the reporter must never break
the worker it observes.

**Verdict protocol.** Workers are instructed (prompt) to end with exactly one
```HYDRA_VERDICT fenced JSON block (`done`, `ac_met`, `ac_unmet`,
`files_changed`, `verification`). `cmd/worker/verdict.go` parses it. No
verdict by turn end → the **worker** nudges in-session
(`HYDRA_MAX_NUDGES`, default 3), not a kill. The old behavior — judge
completion by exit code and `git diff` alone — is how finished work got
rejected as `empty-diff`.

**Park-and-resume.** Error classification is **marker-first** (`park.go`
`decideParkClass`): Kimi's error `type` field lies — overload arrives as
`429 rate_limit_error`, concurrency and quota both as `403
permission_error/access_terminated` — so the message text decides:
`concurrent request limit` / `organizational concurrency` → Tier C
provider-capacity (park); `usage limit` / `quota` → Tier B credential (park
+ alert); 5xx / `overloaded` → Tier A transient (park). A parked worker keeps
its pi session alive and waits with capped exponential backoff (25s → 5m);
on wake it re-enters the turn where it left off. Monotonic park budget:
`HYDRA_MAX_PARK_MINUTES` (default 60); exceeding it abandons-with-summary
rather than hanging forever. Parked time is exempt from the worker's
activity timeout and lifetime cap.

**Scheduler congestion pause.** When the registry shows 3+ live parked
workers (heartbeat within 10 min), the scheduler pauses new spawns for 5 min
so the park-and-resume loop can drain the backlog before the fleet piles onto
the same saturated provider slots. The gate is fail-open: a missing registry or
counter error never blocks the scheduler. Implemented in
`internal/core/scheduler.go` (`Scheduler.shouldPauseForCongestion`).

**Pan, the shepherd.** `internal/pan`: a pi-agent run every 60s against a
snapshot of the registry that may place steering commands (parked worker
that overslept, working-but-stalled worker, zombie with a dead PID) or a
`close_satisfied` verdict for a bead whose work is already on the working
branch. Since 2026-09-01 `close_satisfied` no longer closes on a green test
suite alone when the bead names a measurable acceptance command or a
coverageThreshold/ratchet flip; the full acceptance command must pass and
the durable config must contain the named ratchet entry, otherwise Pan
abstains (continue) or forbids the close. Since 2026-08-24 Pan also sees scheduler-level blocked beads — beads
that are READY but have no live worker row because they hit retry exhaustion
or a spawn failure. Without these candidates Pan cannot judge the blockers
that would otherwise deadlock the fleet (e.g. a worktree conflict that
exhausts retries while the pool still reports phantom placeholders). Fail-open:
Pan down or unparseable advice → the deterministic rules below still run. **Disabled** until configured with its own provider key
(`shepherd.pan` in config; see `hydra-config.yaml.example` for a `minimax` / `MiniMax-M2.7` configuration) — it must not
share the fleet's Kimi account.

**Pan abandon audit and live-slot refusal.** Since 2026-09-11 every
`abandon-with-summary` decision is audited with its rationale: one
`[Pan] abandon-with-summary audit` line per decision logging the bead, the
decision origin (`worker` = the bead had a live worker row at candidate-scan
time, `blocked` = it came from the scheduler-level blocked list), the reason,
and the slot holders (`bead:state(pid=N)` for every live worker row). Two
refusal guards prevent a silent kill of healthy work (br-4y5, 2026-09-11: an
abandon was queued citing `worker limit 1/1 already saturated` while br-4y5's
own worker held that slot — the row registered between `Candidates()` and
judgment execution): a *blocked-origin* abandon is refused when the bead now
holds a live worker row (the blocked evidence is stale — the bead itself
occupies a slot), and a *stale pending* abandon is never executed daemon-side
while a live worker row exists. Worker-origin abandons (e.g. hung workers) are
still enqueued for the live worker to consume; truly workerless beads are
still closed daemon-side.

**Pan repair verdict.** Since 2026-08-24 Pan can also issue whitelisted
`repair:<name>` actions for *blocked* candidates. These are daemon-side
repairs, not worker commands: the daemon executes the handler, verifies the
outcome, and emits a `[PanRepair]` JSON audit event (`bead_id`, `action`,
`result`=`ok|error`, `detail`). Handlers are configured in
`internal/core/service_starter.go`:

| Name | Handler | Safety guard |
|---|---|---|
| `repair-worktree` | `WorktreeManager.Remove` then `EnsureWorktree` for the bead's chain key | refuses if the project path or worktree manager is missing |
| `reap-placeholder` | `WorkerSpawner.ClearSpawningPlaceholder` | no-op if no placeholder exists |
| `reset-retries` | `RetryManager.ResetRetryCount` | returns the previous fault count in the audit detail |
| `repair-bead-store` | `BeadStoreGuard.Restore` for the bead's project | refuses if the project path or guard is missing; unblocks only when a re-list verifies the open count recovered |
| `clear-hard-park` | prunes stale worktree registrations, removes `worktree-unavailable`/`integration-exhausted` labels, reopens the bead, resets its retry counter | refuses when the bead carries no hard-park label — not a generic reopen |
| `prune-worktree-registration` | `git worktree prune` on the bead's project | project-scoped: rate-limited per project (30m), not per bead — one repair clears a fleet-wide registered-but-missing wedge (13 webapp beads from one stale registration, 2026-09-13) |
| `re-push-integration` | reruns the publisher (`PublishRepo`) for a `<project>-push-pending` pseudo-bead | fail-closed: errors unless the stranded push actually cleared; project-scoped rate limit |

**Bead-store integrity guard (2026-09-11).** `BeadStoreGuard`
(`internal/core/bead_store_guard.go`) detects a corrupted `.beads` store that
would otherwise blind scheduling silently (srv-smart-engine incident:
`issues.jsonl` deleted by a raced `br` rebuild, 0 issues in `beads.db`, hours
of silent blindness). The guard is fed every discovery tick's per-project
listing; a collapse of the open-bead count to 0 or >80% in one tick — or a
`br list` failure after a healthy baseline — is corruption, never legitimate
mid-campaign. The project is then excluded from all discovery paths
(fail-closed scheduling) until resolved. On the signal the guard emits
`bead_store_suspect` and attempts one automatic restore: copy the newest
`.beads/.br_history/issues.*.jsonl` over `.beads/issues.jsonl` (temp file +
rename), then **verify the effect** by re-listing — the project is unblocked
only if the open count recovered to at least half the pre-corruption
baseline. Success emits `bead_store_restored`; any failure emits
`bead_store_restore_failed` and the project stays blocked (fail-open on
restore failure means "stay blocked and alert", never "unblock anyway").
Further retries happen only via the `repair-bead-store` Pan action above or a
manual restore. This is the detect/self-heal complement to the hydra-50k
write-path fix; the stream events are the shepherd's visibility into both.

Pan rejects a repair if the bead has any non-completed worker row (live worker
safety), if the handler is unknown or nil, or if the same bead was repaired
within the last hour (project-scoped repairs share a per-project 30-minute
window instead). The prompt documents `repair:<name>` as BLOCKED-only, lists
every whitelisted repair (a test pins prompt↔whitelist parity —
`repair-bead-store` sat whitelisted but unlisted for weeks, so the model could
never choose it), and carries the rescue-recipe catalog: hard-park labels are
durable by design and cleared only when the cause is fixed; many BLOCKED beads
with the same reason mean a shared wedge and prefer one project-scoped repair;
PUSH-PENDING means commits exist locally but not on origin; merges, PRs,
deploys, and repo content edits are human-only.

**Stuck-bead candidacy (2026-09-13).** Pan's candidate set used to cover only
worker-origin problems and scheduler-level blocked records — a hard-parked
bead with no retry record (e.g. after a restart cleared in-memory retry state)
was invisible to the shepherd. The `BlockedBeads` provider now also scans
project stores for non-terminal beads carrying `worktree-unavailable`,
`integration-exhausted`, or a >24h `escalated` label, and surfaces stranded
pushes (below) as `<project>-push-pending` pseudo-beads. The
`integration-exhausted` label is set by `failIntegration` after 3 consecutive
integrate/push failures on one bead (2026-09-16 close-reopen loop): below the
cap a failed close reopens the bead as before, at the cap it hard-parks here
instead of respawning into the same failing integrate forever; the counter
resets on the first successful integrate+push. When ≥3 BLOCKED
candidates share a normalized reason, `collapseWedges` replaces them with one
PROJECT-WEDGE candidate so Pan issues a single project-scoped repair instead
of N rate-limited per-bead ones.

**Push-pending visibility (2026-09-13).** `publishProjects` previously
discarded `PublishRepo`'s `(pushed, reason)` result, so a push rejected by
branch protection or a failing pre-push hook retried silently forever — the
"commits not pushed properly" class ajantha reported. A non-benign result now
records a per-project push-pending state (surfaced to Pan as a PUSH-PENDING
candidate with the failure reason) and clears on the first successful or
benign publish. Detection is deterministic; Pan owns only the judgment tail
(`re-push-integration` vs explaining that protection/permission failures need
a human).

**Project-path resolution (2026-09-13).** Multi-user discovery registers
projects as `<user>/<repo>` while bead IDs carry only the repo name, and both
close-path resolvers (`resolveProjectPathByName`, service_starter's
`projectPathForID`) were exact-match only — Pan's abandon/close/repair paths
failed 100% with "project path not found" for every discovered project. Both
resolvers now fall back to repo-name matches inside multi projects and
path-basename matches; exact matches still win.

**Charon is liveness-based now.** The stuck-worker sweep used to kill on
wall-clock time alone and on 2026-08-01 killed every fleet worker at ~15m —
including parked workers and one worker while it was emitting its closing
verdict. `shouldKillAsStuck` now consults the registry: a fresh heartbeat
(30s reporter) or the `parked` state means alive, regardless of age; only a
stale heartbeat (>5m) means hung and earns the kill. No registry row (old
worker, pre-registry daemon) falls back to the legacy wall-clock timeout.

**Timeout hierarchy (PRD Part 5):**

| Bound | Value | Reset by | Owner |
|---|---|---|---|
| Activity (silence) | 4m | any session/token activity | worker |
| Nudges | 3 (`HYDRA_MAX_NUDGES`) | — | worker |
| Park budget | 60m monotonic (`HYDRA_MAX_PARK_MINUTES`) | never | worker |
| Lifetime cap | 2h monotonic (`HYDRA_WORKER_LIFETIME_MINUTES`) | never, parked time excluded | worker |
| Daemon liveness | heartbeat stale >5m | each heartbeat | Charon |

**Environment knobs (set by the daemon or the operator):**

| Var | Default | Meaning |
|---|---|---|
| `HYDRA_API_URL` | unset | daemon API base; unset → worker never reports |
| `HYDRA_MAX_NUDGES` | 3 | in-session verdict reminders before give-up |
| `HYDRA_MAX_PARK_MINUTES` | 60 | monotonic parked-time budget |
| `HYDRA_WORKER_LIFETIME_MINUTES` | 120 | monotonic lifetime cap |

---

## 6. Verdict memory (D3) — `verdict_memory.go`, `gap_adjudicator.go`

The system's most expensive judgments used to evaporate after use. Now:

- **Fresh gap adjudications** (`real` / `false_positive`, with confidence and
  reasoning) are retained as Hindsight memories at judgment time — once per
  judgment, never on cached re-encounters (the fingerprint cache suppresses
  those before adjudication).
- **Failed closes with an extracted cause** are retained at the A2 site.
- **The adjudicator sees its own history:** its prompt gains a top-3
  `Prior verdicts on similar gaps` section from Hindsight search; with no
  client, an error, or no results the prompt is byte-identical to before.

Deliberately **not** retained: `ambiguous` verdicts and cause-less failures —
an unsure judgment is noise, not memory. All paths are fail-open
(`VerdictRetainer` swallows errors): a Hindsight outage costs memories, never
runs. Each actual retain emits `verdict_retained`.

**Worker-run retention** (what Hindsight's extraction LLM sees per bead):
the parsed `HYDRA_VERDICT` when one exists (~200 tokens of distilled
outcome), sanitized output otherwise — thinkingSignature/base64 runs
stripped, 4KB tail-biased cap, failure evidence kept. The raw turn text
never reaches the extraction LLM (2026-08-01, Sink A). Recall itself makes
**no LLM calls**: local embeddings (bge-small) + local cross-encoder
reranker; the only read-side cost is fleet prompt tokens from context
injection.

---

## 7. What's next (planned tracks)

- **State-based gate truth** — run D3 closed all gates clean on a branch with
  zero integrated code; the compliance verdict must derive from repo state.
- **B2 pathology brake** — signature-triggered, self-releasing slowdown on
  known failure patterns; never a hard cap.
- **Pan enablement** — the shepherd exists (`internal/pan`) and the registry
  it watches is live; pending a dedicated provider key and `shepherd.pan`
  config.
- **Registry migration tail** — Charon still sweeps `workers.json` phantoms
  (fixed 2026-07-31, sweep-first); the tracker itself migrates to the
  registry in a later PRD part.
- **D2 nightly testbed matrix** — known-answer runs on cron, gating deploys.
