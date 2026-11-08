# red handoff (zv1r)

## What changed (files + one line each)
- cmd/worker/enrichment.go (new): RED-phase inert stub for brownfield subagent enrichment — `enrichmentImplemented=false`, `enrichAgentRunner` seam type, `enrichDefaultAgentRunner` var (nil), `enrichAgentArgv` argv seam, inert `enrichBeadClaims(bead, projectPath, worktreePath, run) ([]string, string, error)`, `enrichDefaultBudgetTokens() int` (0 = inert). Doc comments name the GREEN obligations and the hydra-ew18 discovery decisions (detached text-mode, `git cat-file -e <base>:<path>` trigger, `docs/hydra/handoff/enrich-<bead>.md` artifact).
- cmd/worker/enrichment_test.go (new): 8 enforce-gated contract tests (`requireEnrichImpl` / `ENRICH_ENFORCE=1`, established codebase RED pattern, skip loudly by default), all driven through `enrichBeadClaims` with a fake-agent runner and real-git fixtures (`git init -b development`; remote-less base = checkout branch per discovery A5):
  - TestEnrichBeadClaims_BrownfieldRunsAgent — all Target Files exist on base → agent called exactly once, claims/summary returned.
  - _GreenfieldSkipsZeroTokens — target missing on base → 0 agent calls, empty output, no error (AC "cost zero tokens").
  - _AntiLaunderingDropsUnclaimedSummaryLines — summary line without a matching claim dropped; claimed line kept.
  - _BudgetElidesOldestWithFooter — overflow elides oldest assertions first, footer `elided: <n>`.
  - _DefaultBudgetIsMaxPackTokens — 16k-char output under default does not elide.
  - _WritesHandoffDoc — doc at `<worktree>/docs/hydra/handoff/enrich-<bead>.md` carrying the claimed summary (injection via loadChainContext, no new channel).
  - _SkipsWhenDocAlreadyExists — once-per-bead memoization (discovery A2).
  - _EnrichDefaultAgentRunner_DetachedNotRPC + _EnrichBudgetReusesContextPackDefault — runner must pass `--no-extensions --no-session` and never rpc; budget constant must equal `internal/core.DefaultMaxPackTokens` (reuse, not redefinition).
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `go vet ./cmd/worker/` — PASS (clean, rc=0).
- `gofmt -l cmd/worker/enrichment*.go` — clean (after gofmt -w).
- `go test ./cmd/worker/ -run 'Enrich' -count=1` — PASS exit 0 (all 8 skip loudly: "RED: brownfield enrichment contract not yet implemented (set ENRICH_ENFORCE=1 to enforce)").
- `ENRICH_ENFORCE=1 go test ./cmd/worker/ -run 'TestEnrich' -count=1` — FAIL 8/8 for the RIGHT reason: every failure quotes `RED: brownfield enrichment contract not yet implemented (set ENRICH_ENFORCE=1 to fail; enrichmentImplemented=false)` — missing feature, not a compile error.
- COVERAGE (measured, phase-specific AC): `go test ./cmd/worker/ -run 'Enrich' -count=1 -coverprofile=/tmp/enrich-cov.out -covermode=atomic` — `coverage: 0.0% of statements`; `go tool cover -func`: enrichment.go `enrichAgentArgv 0.0% / enrichBeadClaims 0.0% / enrichDefaultBudgetTokens 0.0%`. 0.0% is the CORRECT RED-state result — the inert stub is only reachable under ENRICH_ENFORCE=1 (tests gate on the missing feature before touching it). GREEN must re-measure with the flag flipped and report the real number.

## Contract GREEN must implement
- Flip `enrichmentImplemented = true`; set `enrichDefaultAgentRunner` (detached text-mode Pi: `--no-extensions --no-session`, prompt via stdin, captured stdout — pan.RunPi pattern); implement `enrichAgentArgv`, `enrichDefaultBudgetTokens` (reads `context_pack.max_pack_tokens` config, default `internal/core.DefaultMaxPackTokens`).
- Implement `enrichBeadClaims`: trigger = every `extractTargetFiles(bead)` path passes `git cat-file -e <base>:<path>` in the worktree (base via `internal/core.ResolveIntegrationBase`); any missing → greenfield: one log line, return empty, never run the agent. Parse agent output: `claim: <assertion> (verify: <cmd>)` lines → claims; emitter drops summary assertions with no matching claim; oldest-first elision at chars/4 budget with `elided: <count>` footer; write `docs/hydra/handoff/enrich-<bead>.md` under the worktree (skip write + agent if that file already exists); wire into the worker close/pre-attempt path so `loadChainContext` picks the doc up.

## Open edges for the next phase
- Discovery A1 adjudication still open: `chainContextMaxPerFile = 1500` truncates a 40k-token enrich doc at pickup — default proposal (a) exempt `enrich-*.md` from the cap. GREEN should implement or explicitly reject in the handoff.
- Coverage for new code must be re-measured and reported by GREEN with `enrichmentImplemented = true` (`go test ./cmd/worker/ -run 'Enrich' -coverprofile`).
