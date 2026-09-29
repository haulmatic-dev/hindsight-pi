# FEATURE handoff

## What changed (files + one line each)
- internal/pan/pan.go: repairWhitelist shrunk to hygiene set (repair-worktree, reap-placeholder, reset-retries, repair-bead-store, clear-hard-park); ActFileBead ("file-bead") added with signature dedupe (filedSigs, normalizeReason on title, project-scope keyed) and exported MaxOpenFiledBeadsPerProject=2 / FiledLabel="pan-filed"; BeadFiler callback + SetBeadFiler; handleFileBead + fileScope; judgment prompt rewritten (names list, file-bead bullet, campaign/push recipes → file-bead proposals).
- internal/core/service_starter.go: SetBeadFiler wiring — resolves project path (pseudo-beads by stripping -push-pending/-campaign-anomaly suffix) and delegates to o.filePanBead.
- internal/core/pan_health.go: filePanBead — caps at 2 open pan-filed beads/project (ListBeads + label scan), creates task bead labeled pan-filed, emits pan_bead_filed event.
- internal/pan/file_bead_test.go: 9 new tests (dedupe per signature/project, volatile-title normalization, no-filer/empty-payload errors, filer-error drops signature, allowed action, hygiene whitelist).
- internal/core/pan_filer_test.go: cap + label + slot-freeing test.
- internal/pan/health_test.go, internal/pan/pan_test.go: updated to shrunken whitelist + new prompt contract.
- CHANGELOG.md: Added + Changed entries.

## Verification (commands run + pass/fail)
- `go test ./internal/pan/ ./internal/core/ -count=1` — PASS both packages (0 failures).
- `go build ./...` — PASS. `go vet ./...` — PASS (exit 0).

## Open edges for the next phase
- Daemon-side handlers for removed verbs (park/unpark-campaign, re-push-integration, prune-worktree-registration, integrate-stranded, log-improvement) remain in service_starter.go, now unreachable via Pan verdicts — future cleanup candidate.
- docs/architecture/shepherd.md still documents the full verb list; update when the shepherd doc is next touched.
