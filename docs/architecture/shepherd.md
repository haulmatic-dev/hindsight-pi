# The Shepherd Layer — Events, Quarantine, Fix Beads, Governor, Verdict Memory

**Status:** living document · **Updated:** 2026-08-01 · shipped tracks: A1 A2 B1 C1 C2 D3 + worker registry / park-and-resume / verdict protocol / liveness-based Charon

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

Read programmatically with `ReadEvents(path)` (torn-line tolerant; a missing
file is an empty stream, not an error).

---

## 2. Close verdicts & quarantine (A1) — `bead_closer.go`, `worktree_manager.go`, `chain_completion_*.go`

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
touch the filesystem. Route handlers resolve the store **per request** — a
store captured at route-registration time is nil because the store opens
later; that bug shipped and returned 503s until fixed.

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

**Pan, the shepherd.** `internal/pan`: a pi-agent run every 60s against a
snapshot of the registry that may place steering commands (parked worker
that overslept, working-but-stalled worker, zombie with a dead PID).
Fail-open: Pan down or unparseable advice → the deterministic rules below
still run. **Disabled** until configured with its own provider key
(`shepherd.pan` in config; see `hydra-config.yaml.example`) — it must not
share the fleet's Kimi account.

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
