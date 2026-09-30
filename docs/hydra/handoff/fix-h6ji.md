# fix handoff

## What changed (files + one line each)
- internal/core/integration_retest.go: `runProject` routes multi-repo projects through new `runProjectRepos`, which lists and retests parked beads per repo dir via `resolveRepoPath`; `runBead` now takes an explicit `projectPath` (repo path for multi, project path for single).
- internal/core/overseer.go: `publishOneProject` delegates multi projects to new `publishOneProjectRepos` (PublishRepo per repo dir, identity resolved once on the parent, push-pending aggregated on the parent); `findProjectPathForBead` resolves the repo dir whose bead store contains the exact ID for multi projects (both prefix-match and fallback paths), fixing `persistClosedEpics` "Could not find project path for epic".
- internal/core/multi_user_discovery.go: new `repoDirForBead(project, beadID, beadsClient)` — single projects return `project.Path`; multi projects scan repo stores for the exact bead ID, return "" when not found.
- internal/core/multi_repo_paths_test.go: 3 tests — retest per-repo store hit (parent store errors like real br), epic persistence resolves repo dir for `fmsfe-` prefixed ID, single-project passthrough.
- internal/core/publisher_multi_repo_test.go: multi publish loop never calls parent (benign reasons, no push-pending registered on parent).
- CHANGELOG.md: Fixed entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/core/` — PASS (clean; gofmt diffs in unrelated pre-existing files only, none touched here).
- `go test ./internal/core/ -run 'TestIntegrationRetest_MultiRepo|TestFindProjectPathForBead_MultiRepo|TestRepoDirForBead|TestPublishOneProject_MultiRepo' -count=1` — PASS (4 tests).
- `go test ./internal/core/ -run 'TestIntegrationRetest|TestPublish|TestMultiRepo|TestSelectBead|TestNotifyRepoOpen|TestSetIntegrationOpenNotifier|TestResolveProjectPathByName|TestFindProjectPath|TestPersist|TestEpic' -count=1` — PASS (ok, 2.0s).
- `go test ./internal/core/ -count=1` (full package) — PASS (ok, 65.1s).

## Open edges for the next phase
- `repoDirForBead` scans repo bead stores with `ListBeads` per repo — one br call per repo per epic close and per unknown-prefix bead lookup; fine at fms scale (2 repos), could be memoized if a many-repo project files epics frequently.
- `findProjectPathForBead` multi fallback also benefits `target_overlap.go` call sites (same helper); not separately tested.
- `NotifyRepoOpen` fast-path window keys on `projectPath`; for multi repos the notifier fires with the repo path and `runProjectRepos` selects via repo path, so keys should line up, but no dedicated multi test covers the fast-path sweep.
- Single-type projects registered under "<user>/<repo>" naming already work via existing `resolveProjectPathByName` fallback; untouched.
