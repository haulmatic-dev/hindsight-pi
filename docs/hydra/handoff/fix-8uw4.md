# fix handoff (fix-8uw4, original hydra-7ty3)

## What changed (files + one line each)
- docs/hydra/handoff/fix-8uw4.md: this handoff. Verify-first close — NO code change; the claimed lint-gate defect does not exist on this worktree.

## Verification (commands run + pass/fail)
- Original bead hydra-7ty3 closed `failed` on worker infra error, not a code failure: `❌ Pi rpc turn reached agent_end but the model call FAILED: 400 {"type":"error","error":{"type":"invalid_request_error","code":"1261","message":"[1261][prompt is too long]..."}}`. No lint finding was ever produced.
- Daemon lint gate for the hydra project is `go vet ./...` (parser exit-code, timeout 5m, per /etc/hydra/hydra.yaml). Re-run on this worktree: **exit 0, no output — PASS**.
- `go build ./...` — BUILD_OK, exit 0 — PASS.
- Repo-wide `gofmt -l .` lists 23 files, but ALL 23 are byte-identical-unformatted on `origin/development` itself (UPSTREAM-DIRTY classification via `git show origin/development:<file> | gofmt -l`) and **zero overlap** with `git diff origin/development...HEAD --name-only`. No file changed by this branch is gofmt-dirty; the drift predates this strand and is out of scope (smallest-change discipline).
- `git status --short` — clean before this handoff (no uncommitted source changes).
- Discovery protocol: `hindsight_search` unavailable (skip rule); `cm context` — no matching anti-pattern; `graphify query` ran (quality_gates/gate_close graph confirmed, no duplicate-mechanism concern for a no-op close).

## Conclusion
VERIFY-FIRST CLOSE: lint gate (`go vet ./...`) green, build green, tree clean. The original failure was a model-context infra error ("prompt is too long"), not a code-quality defect. A2 satisfied — defect not reproducible → no implementation, no file modifications beyond this handoff.

## Open edges for the next phase
- Pre-existing upstream gofmt drift (23 files incl. internal/validation/*, internal/pi/*, internal/triage/*, pkg/types/bead.go): harmless to `go vet` today, but any future repo-wide `gofmt -l` gate would fail for every bead. Candidate for its own dedicated hygiene bead, NOT this one.
- `cmd/worker/worker` (a built binary) appears in this branch's changed-file list — build artifact tracked earlier in the strand lineage; cosmetic, not a lint-gate issue.
