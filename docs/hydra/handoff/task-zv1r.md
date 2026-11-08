# red handoff (zv1r)

## What changed (files + one line each)
- cmd/worker/enrichment.go: RED-phase inert stub — `enrichmentImplemented=false`, `enrichAgentRunner` seam, `enrichAgentArgv` argv seam, inert `enrichBeadClaims`, `enrichDefaultBudgetTokens()` (0 = inert). Documents hydra-ew18 discovery decisions (detached text-mode subagent, `git cat-file -e <base>:<path>` trigger, `docs/hydra/handoff/enrich-<bead>.md` artifact).
- cmd/worker/enrichment_test.go: 8 enforce-gated contract tests (`requireEnrichImpl` / `ENRICH_ENFORCE=1`, established RED pattern, skip loudly by default) covering all parent ACs via real-git fixtures (`git init -b development`; remote-less base = checkout branch per discovery A5): BrownfieldRunsAgent, GreenfieldSkipsZeroTokens, AntiLaunderingDropsUnclaimedSummaryLines, BudgetElidesOldestWithFooter, DefaultBudgetIsMaxPackTokens (40000), WritesHandoffDoc, SkipsWhenDocAlreadyExists, EnrichDefaultAgentRunner_DetachedNotRPC, EnrichBudgetReusesContextPackDefault.
- CHANGELOG.md: Unreleased/Added entry describing the RED suite + gate.
- docs/hydra/handoff/red-zv1r.md: detailed handoff incl. measured coverage.

## Verification (commands run + pass/fail)
- `go test ./cmd/worker/ -run 'Enrich' -count=1` — PASS, exit 0 (ok; 8/8 skip loudly, daemon gate green pre-GREEN).
- `ENRICH_ENFORCE=1 go test ./cmd/worker/ -run 'Enrich' -count=1` — FAIL 8/8 with "RED: brownfield enrichment contract not yet implemented (set ENRICH_ENFORCE=1 to fail; enrichmentImplemented=false)" — proves RED for the RIGHT reason (missing feature, not compile error).
- `go vet ./cmd/worker/` — clean. `gofmt -l cmd/worker/enrichment*.go` — clean.
- Coverage measured (RED-phase AC): `go test ./cmd/worker/ -run 'Enrich' -coverprofile=/tmp/enrich-cov.out -covermode=atomic` + `go tool cover -func` — enrichment.go funcs 0.0% (inert stub, correct RED state; GREEN must re-measure).
- Commits: 1f7db84e (RED suite, 4 files +361), 253efd6d (handoff w/ coverage evidence). Tree clean for these files.

## Open edges for the next phase (GREEN)
- Flip `enrichmentImplemented=true`; set `enrichDefaultAgentRunner` (detached text-mode Pi, `--no-extensions --no-session`, never rpc per hydra-ew18).
- Implement `enrichAgentArgv` / `enrichDefaultBudgetTokens` (config `context_pack.max_pack_tokens` → `internal/core.DefaultMaxPackTokens` 40000).
- Implement `enrichBeadClaims`: trigger `git cat-file -e <base>:<path>` via `internal/core.ResolveIntegrationBase`; anti-laundering drop; oldest-first elision with `elided: <n>` footer; write `docs/hydra/handoff/enrich-<bead>.md`; memoize on existing doc.
- Discovery A1 open edge: `chainContextMaxPerFile = 1500` (cmd/worker/chain_handoff.go) truncates a 40k-token enrich doc; proposal (a) exempt `enrich-*.md`.
