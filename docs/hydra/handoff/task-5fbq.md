# discovery handoff (hydra-5fbq)

## What changed (files + one line each)
- docs/discovery/orphanclaim-stale-binary-triage-5fbq.md: triage — the 4 "orphaned in_progress claims" are human-escalated TDD beads counted by a stale digest binary; disk 99% claim stale at 44%; no code change.
- CHANGELOG.md: Documentation entry under Unreleased.
- Remedy EXECUTED: `./cmd/hydra` built from origin/development tip (ec0fc59, includes 7548196 escalated-label exclusion), installed atomically to `/opt/dev-tools/var/hydra/bin/hydra` (mtime 08:49:28Z); daemon drain-restart executed — `kill -TERM 1731164` accepted, new daemon PID 2176865 up 08:54:50Z.
- Digest verified post-restart: 0 "P0 campaign anomaly" filings after 08:54Z (last filing 08:29:53Z under old binary); 4 fresh working registry rows with new work claimed. PASS.

## Verification (commands run + pass/fail)
- Registry `/opt/dev-tools/var/hydra/.hydra/registry.db`: 7 working rows, all heartbeats <1 min old (incl. this bead 5fbq); 0 live orphans. PASS.
- The 4 counted beads (bxzr/1h4v/wiu6/cxpf) are `escalated` human-gated tdd-validation beads — not orphans. PASS.
- Daemon PID 1731164 started 08:19:41Z; binary mtime 07:44Z < 7548196 commit 08:31Z → deployed digest predates escalated exclusion. VERIFIED.
- `git merge-base --is-ancestor 7548196 ec0fc59` — YES; `git diff HEAD origin/development --stat` — empty (HEAD == tip). PASS.
- `go test ./internal/core/ -run 'TestCollectHealthDigest...' -count=1` — PASS (escalated-bead exclusion regression green).
- `cmp /tmp/hydra-staged-5fbq/hydra /opt/dev-tools/var/hydra/bin/hydra` — DIFFER (staged binary contains fix). PASS.
- `df -h /` — 190G/451G = 44%, below 85% watermark. PASS (stale claim).
- hindsight_search — unavailable ("Hindsight is not connected"), skipped per DISCOVERY PROTOCOL.

## Open edges for the next phase
- Operator restart DONE (was the listed edge): drain-restart executed; OrphanClaims digest now 0.
- If escalated beads (bxzr/1h4v/wiu6/cxpf) linger past escalation TTL (72h), escalation_ttl scanner will demote them.
