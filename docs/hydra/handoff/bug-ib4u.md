# bug handoff (bug-ib4u)

## What changed (files + one line each)
- docs/hydra/handoff/bug-ib4u.md: this handoff (verify-first close; no code change — the "project path unresolved swallows push-pending alarms" fix is already landed).

## Verification (commands run + pass/fail)
- `rg -n "push-pending|project path unresolved" internal/pan/pan.go internal/core/service_starter.go` — filer fallback present: service_starter.go SetBeadFiler strips `-push-pending`/`-campaign-anomaly` and re-resolves via projectPathForID (PASS).
- `internal/pan/pan.go:881` — handleFileBead is dispatched for pseudo-beads BEFORE the pseudo-bead ignore gate, so push-pending alarms reach the filer and are not swallowed (PASS).
- `internal/core/service_starter.go:622-630` — project path resolution for pseudo-beads implemented (trimmed re-resolve). PASS.
- `go test ./internal/pan/ -run 'TestFileBead|TestHandleFileBead|Pseudo' -count=1` — ok (PASS).
- `git status --short` — clean except this handoff + CHANGELOG entry.

## Open edges for the next phase
- None: filer fallback handles both `-push-pending` and `-campaign-anomaly` suffixes.
- If new pseudo-bead suffixes are added, they must be added to BOTH the filer fallback (service_starter.go) and IsPseudoBeadID (pan.go).
