# Escalation hygiene triage — shared wedge (hydra-bmtb)

2026-09-29. Project-wide escalation-hygiene wedge: multiple beads BLOCKED with
`escalated` label, no human triage loop, ages 98–298h at triage time.

## State before

Six open beads carried `escalated` (br list --json, label scan):

| bead | age | duplicate of |
|---|---|---|
| hydra-ol7 | ~298h | hydra-fit |
| hydra-cip | ~179h | hydra-85x |
| hydra-gvh | ~179h | hydra-n8a |
| hydra-wdp | ~98h | hydra-g4n |
| hydra-1ly | ~98h | hydra-fic |
| hydra-mt8 | ~98h | hydra-g4r |

All six had zero worker activity for the escalation window and no live human
response. Each is a duplicate of an open original bead covering the same work.

## Daemon-side workflow already in place (no code change needed here)

- `internal/core/escalation_ttl.go` — EscalationTTLScanner demotes non-terminal
  beads whose `escalated` label ages past `overseer.escalation_ttl` (default 72h,
  0 disables): label swapped for `escalation-expired`, typed `escalation_expired`
  event, fail-open. (Landed earlier in this chain.)
- `internal/pan/pan.go` + `internal/core/service_starter.go` — Pan
  `clear-escalation` repair verb (stale-escalation recipe: >~2 days, no human
  response, no live worker → clear labels, one fair retry; efficacy gate refuses
  repeats on unchanged condition). (Landed earlier in this chain.)

## Actions taken (live store, /opt/dev-tools/components/hydra)

- `br doctor migrate-schema recover` — cleared recurring WAL-index poison
  ("database is busy (recovery in progress)"); needed 4× during the session,
  once per burst of br writes.
- Removed `escalated` from hydra-wdp, 1ly, mt8, cip, gvh, ol7 (6× ✓).
- Removed stale `needs-human` / `verified-close-escalated` from hydra-mt8 and
  hydra-ol7 (✓ each).
- Added triage comment to each bead: cleared for one fair scheduler retry;
  duplicate-of pointer; if it loops the same death, abandon-with-summary
  instead of re-escalating.

Post-check: 0 beads carry `escalated`; hydra-mt8/hydra-ol7 retain only
`closed-failed,verified-close-reject-empty-diff`.

## Open edges

- Scanner uses UpdatedAt as escalation-age proxy; any label touch resets the
  clock (conservative direction, acceptable).
- No verb clears `escalation-expired`; a demoted bead that loops needs a Pan
  re-escalate or human action.
- Scheduler should pick up the six cleared beads; if any loops the same
  closed-failed death, Pan abandon-with-summary is the prescribed exit, not
  re-escalation.
