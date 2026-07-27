# pi-rpc — standalone client for `pi --mode rpc`

Isolated from hydra on purpose: it proves what pi's rpc mode can do without any
risk to a live daemon. Bringing these capabilities into hydra is a separate
decision.

## Why

Hydra drives pi in **piped text mode**, where pi's extensions never initialise.
Verified across three controlled runs (with/without `--offline`,
with/without `--no-session`): zero rtk activity, zero `ctx_*` calls in ~4,100
worker logs.

In **rpc mode** the same extensions are alive, and pi exposes a session-control
vocabulary hydra has no access to today.

## Verified end to end (2026-07-27)

| Milestone | Result |
|---|---|
| **M1 handshake** | Full turn lifecycle: `agent_start → turn_start → message_* → turn_end → agent_end`. UI notifications need no reply; only `select`/`confirm`/`input`/`editor` block. |
| **M2 extensions** | Real bash tool executed, and pi reported: `RTK rewrite: git status --short -> rtk git status --short`. **rtk rewriting works in rpc mode.** Hindsight also connected and attempted recall; caveman reported `ultra`. |
| **M3 session control** | `get_state` (live sessionId, thinkingLevel, pendingMessageCount), `get_session_stats` (**token counts: input/output/cacheRead/total**, toolCalls), `get_last_assistant_text`, `get_available_models` (with per-model pricing) — all returned live data. |

Note: `rtk gain`'s counter does not move for pi-invoked rewrites (different
attribution scope), so the rewrite must be confirmed from pi's own notification
stream, not the counter. That mis-measurement is why earlier probes looked like
failures.

## Usage

```bash
pi-rpc --prompt "..." [--provider kimi-coding] [--model kimi-for-coding] \
       [--cwd DIR] [--trace] [--dump raw.jsonl] \
       [--probe-commands get_state,get_session_stats] [--timeout 3m]
```

- `--trace` — every protocol message to stderr (long lines summarised)
- `--dump FILE` — every raw line, untruncated: the only reliable way to inspect
  message payloads and extension notifications
- `--probe-commands` — issue session-control commands after the turn completes
- `--auto-confirm` — answer `confirm` asks with `true`. Default is **deny**: a
  headless session has no human to ask, so an interactive prompt arriving here
  is itself the anomaly, and auto-approving could green-light unreviewed work.

Exit codes: `0` turn completed cleanly · `1` turn failed (including a model
error carried inside an `agent_end`) · `2` usage error.

## Protocol notes worth keeping

- Completion is `agent_end`; `turn_end` is not the end of the run.
- **Model failures hide inside the message** (`stopReason: "error"` +
  `errorMessage`), not in the envelope. A client that only checks the envelope
  reports an expired API key as a passing run — this client did exactly that
  until `MessageError()` was added, and the test pins the behaviour.
- Source of truth for the wire format: pi's
  `dist/modes/rpc/rpc-types.d.ts` plus the reference `rpc-client.js`.

## What rpc unlocks for hydra (not yet adopted)

- `abort` / `abort_bash` — deliberate cancellation instead of the blunt 10-minute
  activity timeout that killed workers 28 times in one run
- `follow_up` — nudge a stalled worker instead of kill-and-respawn, which throws
  away all context and pays for a fresh prompt
- `cycle_model` / `cycle_thinking_level` — escalate a struggling bead to a
  stronger model mid-flight, no respawn (a better mechanism than the
  retry-escalation deferred in `docs/architecture/model-allocation.md`)
- `get_state` / `get_session_stats` — answer "is it hung or thinking?" and read
  real token spend per bead, instead of grepping logs
- `compact` — manage context growth on long beads
