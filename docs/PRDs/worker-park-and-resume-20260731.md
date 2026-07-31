# PRD: Worker Park-and-Resume on Provider Congestion

**Date:** 2026-07-31
**Status:** proposed
**Origin:** production autopsy of bead `hydra-testbed-y05` (see Evidence)
**Related:** `docs/PRDs/shepherd-b2-pathology-brake-20260730.md` (spawn-loop brake),
handoff `docs/operations/handoff-20260731.md` §3.3 (exit-code semantics)

## Problem

When the LLM provider rejects requests mid-session for capacity reasons
(HTTP 403 `concurrent request limit`, 429 rate limit, engine-overload), the
worker **terminates and destroys the pi rpc session**. All accumulated context
— files read, decisions made, partial edits — is lost. The scheduler respawns a
fresh worker that starts from zero.

Verified mechanics in `cmd/worker/main.go`:

1. Every retry attempt calls `executeWithPi`, which spawns a **fresh pi
   process**. Attempt N has none of attempt N-1's session; the only continuity
   is a lossy text summary via `HYDRA_PREV_FAILURE` (Reflexion).
2. Pi's internal request retry budget exhausts in seconds; sustained
   congestion (observed: 40 minutes of continuous 403s on 2026-07-31) always
   outlasts it.
3. After 3 attempts the worker exits (code 7), the scheduler waits ~60 s and
   respawns — into the same congestion. Observed: 4 spawn-die cycles of ~10 s
   each, then a 9m52s successful run whose output was later discarded by an
   `empty-diff` rejection, then a 27-minute fix run that failed.
4. Exit 7 ("engine overloaded, clears in seconds, burns no quota") is
   deliberately hidden from both the breaker and the concurrency governor.
   That assumption is false for account-level concurrency limits, which
   persist as long as the account is saturated. The daemon therefore takes no
   coordinating action during congestion.

### Evidence: `hydra-testbed-y05`, 2026-07-31

| Time | PID | Duration | Outcome |
|---|---|---|---|
| 07:53 | 2004478 | 11m41s real work | killed mid-session by 403 → exit 7, work discarded |
| 08:06–08:30 | ×4 | ~10 s each | died on immediate 403; records left `running` forever |
| 08:34 | 2114185 | 9m52s real work | success, then rejected `empty-diff` → reopened → reworked |
| 08:44 | 2139370 | 27m09s | log capped at 32 MB, exit 1 → `closed-failed` |

≈50 minutes of compute, zero merged output, one bead closed as failed.

### Cost model

Under park-and-resume, the 07:53 session would have parked ~30 minutes and
resumed with full context, producing the close in one session. The fleet lost
~4 session-rebuilds × ~10 min of duplicated exploration for this bead alone,
plus the spawn-die slot churn. This is the primary source of "beads execute
very slowly even discounting server delays."

## Design

Four coordinated parts. Parts 1–2 are the breakthrough; 3–4 keep the rest of
the system from fighting them.

### Part 1 — Worker park-and-resume (cmd/worker)

When a turn fails with a **provider-capacity error class** (403 concurrent
request limit, 429, overload — see `concurrency_classification_test.go`,
`transient_overload_test.go` for the existing classifiers):

- Do **not** exit and do **not** burn a retry attempt. Enter PARKED state:
  exponential backoff with jitter, floor 30 s, ceiling 5 min, total park cap
  60 min (config: `agents.pi.max_park_minutes`, default 60).
- Keep the pi rpc process alive. On each park wake, re-prompt the **same
  session** ("continue") instead of building a new prompt for a new process.
- Emit a heartbeat line every park cycle:
  `[hydra] PARKED bead=<id> waited=<m> reason=provider-capacity next=<s>s`.
  This is the liveness signal for Parts 2 and 4 and for the observer.
- If the park cap is exceeded, exit with the capacity exit code as today —
  the work is genuinely unstartable this hour, scheduler backoff takes over.

### Part 2 — Timeout semantics: parked is not stuck

- The 4-minute rpc activity timeout must treat PARKED heartbeats as activity.
  (Today any wait longer than 4 min of silence is killed — this part is what
  makes parking possible at all.)
- `overseer.worker_timeout` (wall-clock, 15m) must exclude accumulated parked
  time, or migrate to a progress-based timeout: kill only when there is no
  token progress AND no parked heartbeat for N minutes. Note the enforcement
  gap already observed: a 27m09s run under `worker_timeout: 15m`.
- Charon's stuck-worker sweep must skip workers in PARKED state (heartbeat
  file or tracker flag, not log scraping — see Part 4).

### Part 3 — Exit classification

Account-concurrency 403 is not exit 7's "clears in seconds":

- New class: **provider-congestion** (sustained, account-level). Distinct from
  transient overload (exit 7) and rate limit (exit 2).
- While parked, no exit happens at all, so this class primarily feeds the
  scheduler signal in Part 4 and the park-cap-exceeded path.

### Part 4 — Scheduler coordination & observer

- When the last M worker exits/starts show provider-congestion (e.g. ≥2
  capacity deaths or park-cap exits in 10 min), the scheduler **pauses new
  spawns** for a backoff window, letting parked workers inherit capacity as it
  frees. This ends the spawn-die loop competing against parked peers.
- Worker writes a machine-readable state file
  (`$HYDRA_HOME/.hydra/worker-state/<bead>.json`: `state: parked|working,
  since, reason, tokens_so_far`) so Charon, the observer, and the scheduler
  read state without parsing logs.
- Observer/shepherd stall detection classifies each worker from this file:
  `working` (token progress), `parked` (waiting on provider, healthy),
  `hung` (no progress, no park, provider answering) → only `hung` is a kill
  candidate. The observer cannot resume a dead session — its role is
  detection, not resumption; parking is the worker's job.

## Non-goals

- Cross-process session resume (serializing pi session state to disk and
  rehydrating a new process). Park-and-resume keeps the process alive instead.
  If pi later gains `--resume <session-file>`, this design upgrades cleanly.
- Changing the verified-close `empty-diff` gate (separate workstream, task
  #2/A3 state-based gate truth).

## Acceptance criteria

1. A worker whose turn fails on a simulated 403-concurrency error remains in
   the same pi session (same sessionId) and completes after the error clears,
   with `[hydra] PARKED` heartbeats in its log.
2. Parked time does not count toward worker timeout; a worker parked 45 min
   then working 10 min is not killed.
3. During a congestion storm, new spawns pause; zero spawn-die cycles
   (<30 s lifetime) in the daemon log for the window.
4. Observer/Charon never kill a parked worker; a hung worker (no progress,
   provider healthy) is still killed within its timeout.
5. Regression: genuine code failures (exit 1/5) still consume the fault
   budget and escalate exactly as today.

## Risks

- **Slot occupancy**: a parked worker holds a fleet slot up to 60 min. With
  `max_workers: 4` and account-wide congestion this is correct (there is no
  capacity to use anyway), but the pause in Part 4 must not deadlock against
  parked peers: parked workers release their provider-pool slot and re-acquire
  on wake.
- **Zombie park**: pi process dies while worker sleeps. Park wake must verify
  the rpc process is alive and fall back to a normal fresh attempt (with
  Reflexion summary) if not.
- **Heartbeat staleness**: the state file must carry a timestamp; consumers
  treat heartbeats older than 2 cycles as `hung`, not `parked`.
