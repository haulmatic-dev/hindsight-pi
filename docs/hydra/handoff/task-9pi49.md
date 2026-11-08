# refactor handoff (task-9pi49)

## What changed (files + one line each)
- internal/core/gate_junk.go: all gate git mutations (`rm --cached -r`, removal commit, `reset -q`) now run via new `junkGitRun(dir, args...)` which sets `cmd.Dir` to the worktree/project dir — previously they ran in the daemon cwd, silently no-oping the drop (staged junk stayed staged, AC2 failed); `junkPathsFromSelect` renamed `junkFromPaths`; unused `os/exec` inline command blocks removed. Behavior now matches the pinned GREEN contract verbatim.
- internal/core/gate_junk_test.go: contract fixture repairs — `junkWrite` helper creates junk files on disk before `git add` (add dies exit 128 "pathspec did not match" otherwise), `junkCommitPaths` uses `add --force` (the FR-001 ignore lines injected by an earlier drop ignore the very junk being re-committed), drop-commit baseline `before` counted after the fixture's own junk commit, `junkCommitCount` actually parses `rev-list --count` (it returned len(fields)=1 always), and the AC3 test seeds `mock.beads["proj-junk3"]` so `junk-drop-N` labels persist (mock `UpdateBeadLabels` silently ignores unknown beads).
- docs/architecture/shepherd.md, internal/core/bead_closer_hard_gates.go, .gitignore (inherited GREEN-strand changes, found uncommitted in this worktree; .gitignore `.scratch/` line is another strand's, left as-is), CHANGELOG.md: refactor entry.
- docs/hydra/handoff/task-9pi49.md: this handoff.

## Verification (commands run + pass/fail)
- BEFORE (pre-edit, current HEAD): `go test ./internal/core/ -run 'TestGateJunk_' -count=1 -v` — 5 of 6 FAIL (CommittedJunkDroppedOnce `git [add .tmp-jest/x ...]: exit status 128`; StagedOnly `junk path must be unstaged, still staged: [.jest-cache/transform]` — cmd.Dir bug; ReAdded `git [add .tmp-jest/first]: exit status 128`; ThreadCache `git [commit ...]: exit status 1 nothing to commit`; NonJunkOffScope `git [add README.md]: exit status 128`).
- AFTER: same command — PASS, `ok github.com/haulmatic-dev/hydra/internal/core 0.439s`, all 6 `TestGateJunk_` `--- PASS`.
- Regression: `go test ./internal/core/ -run 'TestCloseGate|TestQuarantineClose|TestCloseDep|TestVerifiedClose|TestGateInfra_|TestGateJunk_' -count=1` — ok.
- `go vet ./internal/core/` — clean. `gofmt -l` on touched files — clean.
- No benchmarks exist for this functionality; no perf check run.

## Open edges for the next phase
- None behavioral; the cmd.Dir fix realizes the already-pinned drop contract (inherited GREEN defect, same class as the recorded `gateExecutableAcceptance` cmd.Dir gap).
