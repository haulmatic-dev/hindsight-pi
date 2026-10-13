# green handoff (a518)

## What changed (files + one line each)
- internal/core/postclose_sweep.go (new): NFR-001 — `verificationCommandFor` (metadata `test_command`/`verification_command`, else "Test Command:"/"Verification command:" description line), `BeadCloser.verificationCommandGuard` (runs command via gate runner; nonzero exit → `verification-command-failed` rejection; fail-open only when no runner or runner fault), `PostCloseSweeper` (daily cadence over 7-day window; closed-completed beads whose verification command now exits 1 get `postclose-escape` label + typed `postclose_escape` event; terminal beads never reopened; 7d per-bead escape cooldown; fail-open).
- internal/core/bead_closer.go: guard wired in the Close success path after daemon gates + terminality recheck, before commit/integrate — covers verified-close AND singleton-integrate (same route).
- internal/core/satisfied_close.go: guard wired in BuildCloseSatisfied before the acceptance-command/legacy-gate branches — covers satisfied-close.
- internal/core/config.go: `overseer.postclose_sweep` bool (default true).
- internal/core/overseer.go + service_starter.go: sweeper construction (HYDRA_HOME gate runner) + Start/Stop wiring.
- scripts/escape-rate-weekly.sh (new): weekly query — store mode (br list closed in window, escape-rate from `postclose-escape` labels) + `--events` mode (`postclose_escape` count).
- internal/core/postclose_sweep_test.go (new): 9 tests — extractor table, guard red/green/fail-open, Close red-reject + green-close-singleton, satisfied-close red-refuse + green-close, sweeper flags-escape, nil-safe Start/Stop.
- CHANGELOG.md, hydra-config.yaml.example, docs/architecture/shepherd.md: operator docs.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS. `go vet ./internal/core/` — PASS. `gofmt -l` touched files — clean.
- `go test ./internal/core/ -run 'TestVerificationCommand|TestClose_VerificationCommand|TestBuildCloseSatisfied|TestPostCloseSweeper' -count=1` — PASS (9 new).
- `go test ./internal/core/ -count=1` — PASS (full package, 361.9s).
- Weekly query demonstrated with real output (fixture store via br in cwd, cleaned after):
  `escape-rate-weekly.sh .scratch-escape/proj 7` → closed beads=1, escapes=1, rate=100.0%; `--events /opt/dev-tools/var/hydra/events` → escapes=0 (feature is new, no escapes recorded yet).

## Open edges for the next phase
- PRD docs/PRDs/executable-acceptance-postclose-verification-20261002.md FR-001 (task-breakdown `verification_command` propagation) is a sibling bead; this bead consumes the contract (`test_command` metadata + description line) but does not yet read the `**Verification Command:**` fenced block FR-001 specifies — extend `verificationCommandFor` when FR-001 lands.
- The sweeper re-runs commands in the project path (main checkout), not the bead's worktree — matches satisfied-close precedent; a worktree-scoped pass could be added later.
- Gate runner absent → guard fails open; projects with test_command-carrying beads but no HYDRA_HOME gates are unguarded (documented in shepherd.md).
