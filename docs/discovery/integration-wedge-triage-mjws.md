# Shared integration-exhausted wedge (hydra-mjws) — triage + deploy

2026-09-29, bead hydra-mjws.

## Root cause (VERIFIED)

Every `integration-exhausted` park in the 15:48–15:51 UTC window across BOTH
repos (hydra: y7p9/242b; srv-telematics: hig8/0po4/srch/f10l/5gq/dkqc/awtj/
otae/qj74/38ot/04jq/i6zt — 14+ beads) fails at the identical stage:

    pr-first close <id> via dogfood/<id> → <base>: pr merge: create PR:
    gh pr create: exit status 1 … a pull request for branch "dogfood/<id>"
    into branch "<base>" already exists: <URL>

Not merge capacity, not merge-base staleness. A PR from an earlier
close/integration-retest attempt already exists; the pre-bwyn publisher
treated `gh pr create` duplicate as an integration failure and burned the
consecutive-failure cap on every bead holding a leftover dogfood PR.

The fix already landed on origin/development at 15:25 UTC:
`3d6b9ef fix(publisher): reuse existing PR when gh pr create reports it
already exists` (bead hydra-bwyn, PR #376) — `isPRAlreadyExists` treats the
existing PR as the landing vehicle. Fleet backpressure also already landed:
per-repo `integration_gate` (30s repo cooldown after a gated failure).

## Why parks continued after the fix landed

Deployed daemon binary was built and started at **14:48 UTC** — 37 minutes
BEFORE the fix landed. Daemon (and workers) ran pre-fix code; parks kept
firing every minute. Classic stale-deploy, not a code gap.

## Remedy applied

1. Built hydra + worker from this worktree (contains bwyn fix + gate).
   Markers verified in staged daemon binary: `existing PR for` (1 hit),
   `a pull request for branch` (1 hit).
2. Narrow tests PASS: `go test ./internal/core/ -run
   'TestIsPRAlreadyExists|TestPublishAfterIntegrate_PRAlreadyExists|TestIntegrationGate'`.
3. Overseer paused (POST /api/v1/overseer/pause → 200) so no new spawns.
4. Staged drain-restart (`/tmp/hydra-staging-mjws/drain-restart.sh`, pid at
   deploy time): waits for live worker exes (`/proc/*/exe` — NOT pgrep -f,
   which matches prompt text inside running pi sessions and over-counts),
   swaps binaries via `.new` + `mv` (direct cp hits "Text file busy" on the
   running daemon), restarts, resumes overseer.

## Park clearing

With the fix deployed, per-bead `integration-exhausted` parks are clearable:
the publisher now merges the existing PR instead of failing. Clear via Pan
`repair:clear-hard-park` or operator `POST /api/v1/projects/:name/unpark`,
AFTER confirming daemon restarted post-deploy (see
/var/log/hydra/drain-restart-20260929-mjws.log). Beads whose branch content
is superseded (see docs/discovery/integration-wedge-triage-10ka.md and
sibling triages) should still be abandon-with-summary, NOT cleared — the
duplicate-PR error was their symptom, not their disposition.

## Open edges

- Tier B agentic conflict resolution still unimplemented; retest stage
  stays refuse-on-conflict (correct).
- Verify-deploy marker run (`scripts/verify-deploy.sh "<since>" "existing PR for"`)
  should be executed post-restart to prove the running daemon has the fix.
