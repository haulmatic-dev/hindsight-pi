# GREEN handoff

## What changed (files + one line each)
- `internal/core/spawn_preflight.go`: Implemented "Store-ownership preflight precision" logic — caps the warning to up to 3 offending store paths (`.beads/<name>: uid <n>`) and downgrades the warning to debug level (no `spawn_warn_store_ownership` event) when a `br list --json --limit 1` probe exits 0; failing probes maintain the warning and event.

## Verification (commands run + pass/fail)
- `go test ./internal/core/ -run 'StoreOwnership' -count=1`: PASS.
- `go build ./...`: PASS (exit 0).
- Coverage: target functions = `storeOwnerUID` 80.0%, `offendingStorePathsDetail` 78.9%, `storeOwnershipDetail` 100.0%, `spawnPreflightStoreOwnership` 87.5–88.2%, `brListProbe` 100.0%.

## Open edges for the next phase
- None.
