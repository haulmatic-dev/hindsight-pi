# PRD: Worker Park-and-Resume, Verdict Closure, and Pan the Shepherd

**Date:** 2026-07-31 (rewritten 2026-07-31 after design review)
**Status:** partially implemented in production as of 2026-08-02 (see As-built deltas below)
**Origin:** production autopsy of bead `hydra-testbed-y05` (see Evidence) plus
three rounds of operator design review.
**Related:** `docs/PRDs/shepherd-d1-cron-20260730.md` (Pan's cron home),
`docs/PRDs/shepherd-b2-pathology-brake-20260730.md`,
handoff `docs/operations/handoff-20260731.md` §3.3 (exit-code semantics),
task #2/A3 (state-based gate truth — merges into Part 5).

Supersedes the first draft of this PRD (per-bead JSON state files,
observer-talks-to-workers-directly). Changes from review: daemon-owned SQLite
registry, verdict-based closure, Pan as the decision-maker, session-file
continuity.

## As-built deltas (2026-08-02)

What is live in `development` and production:

- **Part 1 — Tiered classification:** implemented as marker-first in
  `cmd/worker/park.go` (`decideParkClass`). Tier A auto-park and Tier C close
  are active; Tier B credential-park was deprioritized to a later credential
  rotation story.
- **Part 2 — Park-and-resume:** parking in the same pi process is live.
  Named session files are also live: the worker builds a deterministic
  session id from the bead id, creates a directory under `$HYDRA_HOME/sessions`,
  and passes `--session` and `--session-dir` to pi. A replacement worker (or a
  daemon restart) uses the same id/directory and resumes the session file
  instead of starting from zero.
- **Part 3 — Verdict closure:** `HYDRA_VERDICT` parsing, in-session nudging,
  and `abandon-with-summary` are live.
- **Part 4 — Registry + command queue:** daemon-owned SQLite registry, worker
  reporter, HTTP command queue, and Charon liveness integration are live.
  Registry GC (`Store.ReapStale`) was added later to clean zombie rows on
  startup.
- **Part 5 — Timeout hierarchy:** live. Worker lifetime cap is 2 h
  (`HYDRA_WORKER_LIFETIME_MINUTES`); activity timeout 4 m; park cap 60 m;
  Charon hung-kill uses stale heartbeat (>5 m), not wall time.
- **Part 6 — Pan:** component exists and is configurable, but is disabled in
  production until a dedicated provider key is supplied.

Additional live changes not in the original PRD:

- **Hindsight verdict-first retention** (2026-08-02): workers now retain the
  parsed `HYDRA_VERDICT` instead of raw turn text, dramatically cutting
  Hindsight extraction LLM tokens while improving memory quality.

## Problem

Two coupled defects burn the fleet's compute:

**A. Provider congestion destroys sessions.** When the LLM provider rejects
requests mid-session for capacity reasons (HTTP 403 `concurrent request
limit`, 429, 529), the worker terminates and the pi rpc session dies with all
accumulated context. Every retry attempt spawns a **fresh pi process**
(`executeWithPi`); the only continuity is a lossy text summary
(`HYDRA_PREV_FAILURE`). Observed 2026-07-31: 40 minutes of continuous 403s;
one bead burned ≈50 minutes of compute across 9 worker processes and closed
as failed with zero merged output.

**B. "Completed" means "pi stopped talking".** Worker success is
`agent_end` with no model failure (`cmd/worker/main.go:1470`). Pi can end its
turn halfway through a bead — invisible. Gates check artifacts (diff exists,
no stubs), not semantics (acceptance criteria actually met). The semantic
checker (`validation-agent`) is disabled in production. Gates also misfire
the other way: real work was rejected `empty-diff` and reworked.

### Evidence: `hydra-testbed-y05`, 2026-07-31

| Time | PID | Duration | Outcome |
|---|---|---|---|
| 07:53 | 2004478 | 11m41s real work | killed mid-session by 403 → exit 7, context lost |
| 08:06–08:30 | ×4 | ~10 s each | spawn-die loop on immediate 403 |
| 08:34 | 2114185 | 9m52s real work | success, then rejected `empty-diff` → reworked |
| 08:44 | 2139370 | 27m09s | log capped 32 MB, exit 1 → `closed-failed` |

## Design overview

```
pi (rpc session, named session file)
  ↑ prompt / steer / follow_up
worker (control loop: classify, park, nudge, verdict-parse)
  ↑ status reports ↑ command queue (HTTP, existing :8089 API)
daemon (SQLite registry: single writer; queues; spawn-pause; Charon reads it)
  ↑ verdicts on the ambiguous tail
Pan (pi-agent shepherd, own provider key, fail-open)
```

Six parts. Parts 1–3 are the breakthrough; 4–6 keep the system coherent.

### Part 1 — Tiered error classification (worker)

Marker-first classification (message markers, then error type, then HTTP
status) because Kimi's `type` field is unreliable: engine-overload arrives as
`429 rate_limit_error`; both concurrency-limit and quota-exhaustion arrive as
`403 permission_error`/`access_terminated_error` (all observed live
2026-07-27..31; classifiers `isTransientOverload` / `isConcurrencyLimit` /
`isQuotaError` already embody this).

| Tier | Codes | Action |
|---|---|---|
| **A — auto-park** | 429, 529, 500, 504, 403-concurrency | keep pi alive; park; resume in-session |
| **B — escalation-park** | 401, 402, 403-quota | park + `needs-credential` event + breaker alert; resolved by credential rotation, then graceful respawn-with-resume |
| **C — close** | 400, 404, 413, genuine 403 | deterministic dead-end; no retry |

Tier B rationale: a rotated key never reaches a running process (env is
frozen at spawn, rpc has no re-auth command), so live-resume is impossible —
but work products live in the worktree and in the named session file, so a
replacement worker resumes with new credentials losing little.

### Part 2 — Park-and-resume with worker control loop (cmd/worker)

The worker stops being one-shot and gains a control loop:

- On a Tier A error: **do not exit, do not burn a retry attempt.** Register
  `state: parked` with the daemon, keep the pi rpc process alive, sleep with
  exponential backoff + jitter (floor 30 s, ceiling 5 min, park cap 60 min,
  config `agents.pi.max_park_minutes`).
- On wake: poll the daemon command queue, then resume the **same session**
  (`prompt` / `steer`). pi rpc provides the nudge primitives: `steer`
  (queued while running, delivered after the current turn) and `follow_up`.
- Sessions become **named** (drop `--no-session`; `--session <bead-attempt-id>`,
  `PI_CODING_AGENT_SESSION_DIR`). Payoff: parked workers survive a daemon
  restart — the daemon respawns them with `--session <id>` resume instead of
  redoing work from scratch (today's §3.4 trap).
- Zombie guard: on wake, verify the pi process is alive; if it died, fall
  back to a fresh attempt with Reflexion summary + session-file resume.

### Part 3 — Verdict-based closure (worker prompt ↔ worker parser)

Pi is never killed without a proper closure. Closure has exactly two forms:

1. **Done verdict** — the agent's final assistant message carries:
   ```
   HYDRA_VERDICT: {"done": true, "ac_met": [...], "ac_unmet": [],
                   "files_changed": [...], "verification": [{"cmd": "...", "result": "..."}]}
   ```
2. **Abandon verdict** — `done: false` with a `summary` of what was done and
   what remains (agent-issued, or Pan-issued `abandon-with-summary`).

Rules:

- The verdict is parsed **only from the final assistant message** — never
  from tool results or file contents (spoofing guard); last match wins.
- Verdict missing or `ac_unmet` non-empty → not a completion. Worker nudges
  in-session (≤3, deterministic): `"ACs unmet: X, Y — continue"`. Context-
  exhaustion endings get a distinct `compact-and-continue` nudge.
- Nudges exhausted → escalate to Pan (Part 6).
- A `done:true` verdict is **necessary but not sufficient**: the
  verified-close gates (TDD, diff, scope, stub) remain as the fail-closed
  backstop, and the verdict's `files_changed` is cross-checked against the
  actual worktree diff (kills both failure modes: halfway-close and
  empty-diff false-reject).
- Hard kill (Charon) is reserved for hung processes and is logged as a
  **system failure**, never charged to the bead's fault budget.

### Part 4 — Daemon-owned registry and command queue (cmd/hydra)

- SQLite registry at `$HYDRA_HOME/.hydra/registry.db`, **daemon is the only
  writer** (workers report over the existing :8089 HTTP API). Replaces
  `workers.json`. Schema: `{bead_id, pid, pgid, project, started_at, state
  (working|parked|hung), pi_session_id, last_error {status, type, message},
  tokens_so_far, last_heartbeat, park {since, reason, wake_at}}`.
- Per-worker command queue (rows: `continue`, `steer:<msg>`,
  `compact-and-continue`, `abandon-with-summary`), written by the daemon or
  Pan, polled by workers.
- Scheduler congestion-pause: while any worker is parked (or ≥2
  provider-congestion deaths in 10 min), **pause new spawns** so freed
  capacity goes to parked resumes first. Ends the spawn-die loop.
- Charon reads the registry: kills only registered PIDs/PGIDs; `parked` with
  a fresh heartbeat is exempt; `hung` (no heartbeat, no token progress) is
  the only kill class. Kill decisions stay fail-closed on registry data.

### Part 5 — Timeout hierarchy (replaces one ambiguous knob)

`worker_timeout` (15m) is already violated in production (a 27m09s run).
Split into four knobs:

| Knob | Default | Scope |
|---|---|---|
| activity timeout | 4 min (rpc) | silence while not parked |
| park cap | 60 min | per park episode |
| nudge cap | 3 | verdict-missing nudges |
| **lifetime cap** | 2 h | monotonic, per attempt, **nothing resets it** |

The lifetime cap is the "never stuck forever" proof: every other bound can
be extended by events, this one cannot.

### Part 6 — Pan, the shepherd (new component; Shepherd D1 home)

Pan is a pi-agent (cron from `shepherd-d1-cron-20260730.md`) with a
**separately configurable provider key** — it must stay lucid during the
congestion storms it manages, so it never shares the worker fleet's account.

- Reads the registry; acts only on the ambiguous tail (workers parked near
  cap, verdict-missing after nudges, ambiguous error classes). Deterministic
  rules handle ~95% of events; Pan is not in the per-heartbeat path.
- Writes queue commands (`steer:<msg>`, `compact-and-continue`,
  `abandon-with-summary`) via the daemon.
- Spot-checks done-verdicts against repo state (this absorbs task #2's A3
  state-based gate truth).
- **Fail-open, never gates the pipeline**: if Pan or its provider is down,
  the deterministic defaults (backoff, nudge cap, abandon at lifetime cap)
  run without it. Same contract as the shepherd event stream.

## Closed-loop guarantees

- **Nothing stuck forever**: park cap, nudge cap, lifetime cap, fault budget,
  fix-recursion cap, Charon hung-kill — every state has a monotonic exit.
- **Nothing killed prematurely**: parked workers are exempt from every
  timeout and sweep; kill decisions are fail-closed on registry data;
  hard kill is a system failure, not a bead failure.
- **No halfway closes**: verdict required + gates fail-closed + Pan
  spot-checks verdict against repo state.
- **No rework from congestion**: sessions park and resume; named session
  files survive restarts; credential rotation costs a graceful
  respawn-with-resume, not a rebuild.

## Acceptance criteria

1. A worker whose turn fails on a simulated 403-concurrency error stays in
   the same pi session (same sessionId), parks, and completes after the error
   clears; daemon log shows zero spawn-die cycles in the window.
2. A turn that ends without `HYDRA_VERDICT` is nudged in-session, not
   closed; after 3 nudges the case appears in Pan's queue input.
3. A `done:true` verdict whose `files_changed` disagrees with the worktree
   diff is rejected by the close gate.
4. Parked time counts toward no timeout; a worker parked 45 min then working
   10 min is not killed; a worker exceeding the lifetime cap is gracefully
   abandoned with a summary.
5. Daemon restart with parked workers: workers respawn with `--session`
   resume and continue; no bead restarts from zero.
6. Charon kills only registered PIDs; a parked worker with fresh heartbeat
   is never killed; a hung worker is killed and logged as system failure.
7. Pan disabled/unreachable: all of the above still pass.
8. Regression: genuine code failures (exit 1/5) consume the fault budget and
   escalate exactly as today.

## Risks

- **Slot occupancy**: parked workers hold fleet slots up to the park cap.
  Correct under account-wide congestion (no capacity exists anyway); the
  scheduler spawn-pause prevents competing with parked peers.
- **Verdict gaming**: agents learn to emit `done:true` early. Mitigated by
  gates + files_changed cross-check + Pan spot-checks + verdict-memory
  (repeat offenders become prompt-level doctrine).
- **Registry migration**: `workers.json` consumers (Charon, WorkerTracker
  callers, `scripts/`, eval harness) must move to the registry in one change
  or behind a compatibility view.
- **Pan prompt drift**: Pan's decisions must be logged as typed shepherd
  events with reasons, or it becomes an unauditable black box.

## Non-goals

- Pan talking to pi directly. The worker owns the rpc process stdin; all
  session injection goes worker → pi. No exceptions.
- Serializing pi session state ourselves. Named session files + `--session`
  resume are the mechanism.
- Changing the verified-close gates themselves (TDD/diff/scope/stub stay;
  they become the backstop under the verdict protocol).
