# feature handoff

## What changed (files + one line each)
- internal/api/operator.go: FR-033 — operator endpoints POST /api/v1/projects/:name/park, /unpark (registry.SetProjectPark), GET /api/v1/quarantine (hydra/quarantine/* branches via core.ListQuarantineBranches + open beads carrying quarantine labels per configured project), POST /api/v1/quarantine/:bead/clear (removes only quarantine labels, intersected against {stranded-work, stranded-conflict, integration-unverified, integration-exhausted, worktree-unavailable}). Every mutation emits typed audit event (project_parked/project_unparked/quarantine_cleared). Nil registry store → 503.
- internal/core/quarantine_drain.go: new ListQuarantineBranches(projectPath) ([]QuarantineBranch, error) — read-only for-each-ref over refs/heads/hydra/quarantine/*; non-git dir or no refs → empty, nil error.
- internal/core/overseer.go: exported accessors for the operator API: RegistryStore(), EventSink(), BeadsClient(), Projects().
- internal/api/server.go: RegisterOperatorRoutes wired after worker registry routes; also fixed comment (`s.overseer.RegistryStore` matches the existing getStore pattern).
- internal/api/operator_test.go: 7 tests — park/unpark roundtrip through store, 503 nil-store, unknown project 404, missing project field 400, quarantine list shape, label intersection.
- CHANGELOG.md: Added entry under Unreleased.
- docs/architecture/shepherd.md: operator API documented next to the worker registry section.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS
- `go vet ./internal/api/ ./internal/core/` — PASS
- `go test ./internal/api/ -count=1` — PASS (whole package incl. pre-existing worker_registry tests)

## Open edges for the next phase
- Park endpoint keys the registry row by project name; the scheduler's isCampaignParked keys by projectKey(projectPath) — identical only when project names equal the path basename (true for standard configs). Multi-user /home/.../projects layouts diverge; park-by-name there writes a row nothing reads.
- Quarantine list invokes `br` per configured project (ListBeads) — slow with many projects; no pagination.
- clear endpoint returns 200 with empty removed[] when the bead has no quarantine labels (idempotent no-op, not an error).
