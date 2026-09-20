# Self-healing session-file resume pilot

This page documents the `self_healing.session_resume` pilot implemented for
[Self-heal recovery A](../PRDs/self-healing-context-recovery-20260915.md).

## What it does

When a worker dies from an infrastructure-class cause (OOM, SIGKILL, host
shutdown, provider capacity signal) and Charon reaps the orphan, the next
respawn can resume the dead worker's Pi session file instead of starting from a
blank transcript. This recovers the context the dead worker already paid for at
cache-read prices.

Resume is gated because naive reuse re-imports pathology:

- **Infra-death only** — `RetryManager.IsInfra` must classify the last failure
  as infrastructure. Wedged or code-fault deaths start fresh.
- **Attempt-boundary injection** — the worker prompt receives an `ATTEMPT
  BOUNDARY` block that tells the model it is a new attempt and must verify
  worktree state before trusting prior claims.
- **HEAD check** — the worktree HEAD is compared against the HEAD recorded at
  the previous spawn. If the branch moved (commit, integrate, manual edit),
  resume is refused because the transcript's claims no longer match reality.
- **Age/size caps** — stale or oversized session files are not resumed.
- **Post-reap only** — resume runs inside `SpawnInternal`, which is only reached
  after duplicate-spawn guards confirm there is no live worker or registry row.

## Configuration

Add under `/etc/hydra/hydra.yaml`:

```yaml
self_healing:
  session_resume:
    enabled: false          # opt-in; default false
    dry_run: true           # measure only, never auto-disable
    infra_only: true        # resume only after infra-class death
    head_check: true        # compare worktree HEAD to stored spawn HEAD
    max_session_age: 1h     # refuse sessions older than this
    max_session_size: 52428800  # 50 MiB
    fix_rate_threshold: 0.5
    reject_rate_threshold: 0.5
    min_samples: 3
```

- `enabled`: master switch. Leave `false` to keep today's fresh-start behavior.
- `dry_run: true`: perform resume (when enabled) but never auto-disable; use
  this to collect the pilot metrics.
- `dry_run: false`: auto-disable resume when resumed beads exceed either
  threshold over `min_samples` resumed outcomes.

## Metrics and auto-disable

The pilot counts three outcomes for resumed beads and for fresh beads:

- `success` — bead closed as completed.
- `fix` — a fix bead was created because the original closed as failed.
- `reject` — verified-close rejected the completion.

When `dry_run` is false and either the resumed fix rate or the resumed
verified-close rejection rate exceeds its threshold over at least
`min_samples` resumed beads, resume is automatically disabled and a log line
is emitted:

```
[SessionResumePilot] auto-disabling session resume: fixRate=... rejectRate=... over N resumed beads
```

## Visibility

- Spawner logs `resuming Pi session for <bead>` or `not resuming session for
  <bead>: <reason>`.
- Resumed beads are labelled `session-resumed` in the bead store.
- The worker log shows `Resuming existing Pi session for <bead>`.
