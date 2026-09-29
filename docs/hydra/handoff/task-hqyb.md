# task handoff (hydra-hqyb)

## What changed (files + one line each)
- docs/discovery/publisher-wedge-hqyb.md: triage — PR #389 wedge covered by already-landed PR-reuse fix (stale daemon binary is the root cause); disk recovery 99%→86%; root-only cap steps documented.
- CHANGELOG.md: Unreleased entry recording triage + operator follow-ups.
- No code change needed: `resolveOrCreatePR`/`isPRAlreadyExists` PR-reuse (the warranted fix) already committed on this branch (b5eacb0, d6c5752) and verified.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go test ./internal/core/ -run 'TestResolveOrCreatePR|TestIsPRAlreadyExists|TestPRURLFromError' -count=1` — PASS.
- `timeout 60 systemd-tmpfiles --clean` — ran (user-level perms only; no fatal errors).
- `df -h /` — 86% used, 65G free (was 99%).
- `sudo -n true` — FAILED (password required) → journal vacuum + cache caps NOT VERIFIED, recorded as operator follow-up.

## Open edges for the next phase
- Human: merge PR #389 (gh pr merge 389 --merge --admin), then redeploy daemon from development (stale-binary root cause, same as mjws).
- Operator (root): journalctl --vacuum-size, SystemMaxUse cap, npm cache clean for ajantha; disk still above 85% watermark.
