# red handoff

## What changed (files + one line each)
- internal/core/postclose_sweep_test.go (new): RED tests pinning the post-close verification sweep contract — 5 tests: reopen path (bead reopened to open + `postclose-reopened` label + comment carrying BOTH failing-run outputs), flake guard (fail→pass re-run → `postclose-flake` label, no reopen, no comment), bounded auto-reopen (bead already carrying `postclose-reopened` → `needs-human` added, no second reopen/status write), skips (beads older than 24h and closed beads without `metadata["test_command"]` → zero runner invocations, zero mutations), and a real-git integration test (closed fixture bead whose command `false` fails in a temp default-branch git repo → reopened with label and comment).
- internal/core/postclose_sweep.go (new): inert RED stub — constants + constructor + inert scan()/Start()/Stop() so the package compiles for the lint gate; zero behavior.
- CHANGELOG.md: Added entry under Unreleased.

## Contract GREEN must implement (pinned API)
- `NewPostCloseSweeper(beadsClient BeadsClient, getProjects func() []types.Project, events *EventSink, runCommand func(projectPath, command string) (output string, err error), addComment func(projectPath string, beadID types.BeadID, body string) error) *PostCloseSweeper`
- `(*PostCloseSweeper).scan()` does the work; Start/Stop loop shape can mirror IntegrationExhaustedSweeper.
- Labels: `postclose-reopened`, `postclose-flake`; command source: `metadata["test_command"]`; max age 24h on UpdatedAt; only `StatusClosed` beads considered; command runs in the project's default-branch checkout (projectPath).
- Flake guard = re-run once after a failure; re-run success → flake.

## Verification (commands run + pass/fail)
- `go vet ./internal/core/` — PASS (clean; was `undefined: NewPostCloseSweeper` before stub).
- `go test ./internal/core/ -run TestPostCloseSweep -count=1` — FAIL (RED, right reason) — runtime contract failures, 0 compile errors:
  - TestPostCloseSweepReopensAndAttachesBothOutputs: `expected bead reopened to open, got status ""`
  - TestPostCloseSweepFlakeLabelsWithoutReopening: `expected postclose-flake label, got []`
  - TestPostCloseSweepSecondReopenAddsNeedsHuman: `expected needs-human label on second failure, got []`
  - TestPostCloseSweepIntegrationRealGitRepoReopen: `expected fixture bead reopened, got status ""`
  - (TestPostCloseSweepSkipsOldAndCommandlessBeads passes trivially while inert.)
- `gofmt -l internal/core/postclose_sweep*.go` — clean.

## Open edges for the next phase
- Implement internal/core/postclose_sweep.go to the pinned API; wire Start/Stop in service_starter.go and a config knob if desired (knob not pinned by these tests).
- Event emission (`postclose_reopen`/`postclose_flake` typed events via EventSink) is optional per tests (nil sink passed) but recommended to match sweeper doctrine.
- Coverage (RED, measured): `go test ./internal/core/ -run TestPostCloseSweep -coverprofile` → postclose_sweep.go NewPostCloseSweeper 100%, Stop 100%, Start 0% (nil-guard early return), scan 0% (inert stub); package 0.1%. GREEN must re-measure once behavior lands.
