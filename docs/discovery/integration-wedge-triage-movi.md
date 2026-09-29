# Shared integration-exhausted wedge — triage (hydra-movi, 2026-09-29)

## Beads
- hydra-xxq5 — closed (WAL-poison root cause documented).
- hydra-242b — closed not_planned (superseded by escalation_ttl, per ng18 triage).
- hydra-rsyw — open, integration-exhausted; branch `dogfood/hydra-rsyw` carries an
  obsolete publisher PR-reuse fix that now conflicts with the landed
  `prAlreadyExistsError` implementation (merge-tree conflicts:
  internal/core/publisher.go, publisher_test.go, scripts/cache-cap-guard.sh add/add).
  Disposition: abandon-with-summary; do NOT rebase (would duplicate landed code).
- hydra-6zwl — open, integration-exhausted; work already integrated
  (d6c5752 "hydra: integrate hydra/wt-hydra-6zwl"). Disposition: abandon-with-summary, label stale.
- ajantha-srv-telematics-burr — open, integration-exhausted; branch
  `origin/dogfood/ajantha-srv-telematics-burr` is docs-only and merge-tree vs
  origin/development is CLEAN (rc=0). Disposition: clear
  integration-exhausted/integration-failed for one fair retry.
- ajantha-srv-telematics-otae — fix bead for e3hn closure failure; no
  integration-exhausted label; no park to clear.

## Root causes (all now fixed)
1. Publisher failed on "PR already exists" — fixed upstream: landed
   `prAlreadyExistsError` idempotent reuse; PR #389 merged (state=closed,
   merged=true via API).
2. Poisoned br WAL index ("database is busy (recovery in progress)") on both
   stores — repaired via `br doctor migrate-schema recover`.
3. Host disk 99% — now 78% (100G free) after /tmp debris cleanup.
4. hydra-rsyw gate lint `isPRAlreadyExists redeclared in this block` — transient
   duplicate declaration in an in-flight branch; gone after merging
   origin/development (build green).

## Verification
- `git show origin/development:internal/core/publisher.go | grep -c isPRAlreadyExists` → 0
  (landed name is `prAlreadyExistsError`, lines 339/354).
- `curl -s api.github.com/repos/haulmatic-dev/hydra/pulls/389` → closed/merged.
- `df -h /` → 78% used, 100G free.
- `git merge-tree --write-tree origin/development origin/dogfood/ajantha-srv-telematics-burr` → rc=0.
- `go build ./...` after merging origin/development → PASS.
