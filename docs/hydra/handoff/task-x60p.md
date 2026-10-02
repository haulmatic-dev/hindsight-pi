# task handoff (hydra-x60p)

## What changed (files + one line each)
- docs/discovery/orphanclaim-recurrence-triage-x60p.md: triage — the "2 in_progress beads with no live registry worker" P0 is the pre-grace digest filing loop's own output; fix already landed upstream (PR #70 chain: 5m claim grace 0a2a6c9, pan-filed exclusion 7ef09f6, heartbeat-fresh liveness 0d4aeaa) but deployed daemon binary (built 00:47 UTC) predates the merge (03:20 UTC) — staged rebuild at /opt/dev-tools/var/hydra/bin-staging-x60p/hydra for operator restart. Disk-99% claim stale (host 39%). Runaway zombie bash PID 1561310 killed.
- CHANGELOG.md: Documentation entry under Unreleased.
- No code change required: the proposed remedy already exists on origin/development; re-landing would duplicate PR #70.

## Verification (commands run + pass/fail)
- `git log origin/development --oneline -5` — PR #70 merge 5efedd9 with all three digest fixes present. PASS.
- Deployed binary vs PR #70 timestamps: built 00:47 UTC < merge 03:20 UTC → pre-grace digest running. VERIFIED.
- `go build ./...` from temp worktree of origin/development (5efedd9) → staged binary `/tmp/hydra-staged-x60p/hydra`, copied to `/opt/dev-tools/var/hydra/bin-staging-x60p/hydra`; `cmp` with deployed binary → DIFF. PASS.
- `br list --status in_progress` (after `br doctor migrate-schema recover` for WAL poison) — hydra-x60p/k9tc/4jas/ftah/l867 all P0 campaign-anomaly meta beads. PASS.
- `df -h /` → 165G/451G = 39%. PASS (stale disk claim).
- `br comments add` ×4 on twin beads (k9tc/4jas/ftah/l867) — all ✓. PASS.
- `go build ./internal/core/` in this worktree — PASS. Commit verified via `git show --stat HEAD` (2 files, +56).

## Open edges for the next phase
- Operator action: swap `/opt/dev-tools/var/hydra/bin-staging-x60p/hydra` in as `/opt/dev-tools/var/hydra/bin/hydra` and restart the hydra systemd service (not while workers mid-run). Without restart the digest keeps filing P0 twins.
- After restart: twin meta beads k9tc/4jas/ftah/l867 get workers or close as superseded.
- Registry accounting: fleet/*.db and registry.db files are 0-byte placeholders; live registry is in-process — worth a doc note to avoid future false trails.
