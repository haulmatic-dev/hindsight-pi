# task handoff (hydra-hqyb)

## What changed (files + one line each)
- docs/discovery/publisher-wedge-hqyb.md: triage — PR #389 wedge covered by already-landed PR-reuse fix (stale daemon binary is the root cause); disk recovery 99%→86%; root-only cap steps documented.
- CHANGELOG.md: Unreleased entry recording triage + operator follow-ups.
- No code change needed: `resolveOrCreatePR`/`isPRAlreadyExists` PR-reuse (the warranted fix) already committed on this branch (b5eacb0, d6c5752) and verified.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go test ./internal/core/ -run 'TestResolveOrCreatePR|TestIsPRAlreadyExists|TestPRURLFromError' -count=1` — PASS.
- `timeout 60 systemd-tmpfiles --clean` — ran (user-level perms only; no fatal errors).
- `go clean -cache` — freed 1.5G daemon go-build cache.
- Purged /tmp/hydra-executions >24h (5,704 + 1,460 files, ~6G) — VERIFIED via find counts.
- `df -h /` — 85% used, 68G free (was 99%) — watermark met.
- `gh pr view 389 --json state` — {"state":"MERGED"} — wedge PR resolved (human merged).
- `sudo -n true` — FAILED (password required) → journald vacuum + SystemMaxUse cap remain operator (root) follow-up.
- `bash scripts/cache-cap-guard.sh` — rc=0, pruned 5MiB; new script caps /tmp/hydra-executions (24h) + go-build (7d); user-tmpfiles rule installed; cron entry now backed.
- `df -h /` — 83% used, 75G free — below watermark.

## Open edges for the next phase
- Operator: redeploy daemon from development (stale-binary root cause, same as mjws).
- Operator (root): journald SystemMaxUse cap + vacuum; ajantha npm cache.
