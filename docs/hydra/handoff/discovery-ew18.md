# discovery handoff (hydra-ew18)

## What changed (files + one line each)
- docs/discovery/brownfield-enrichment-discovery-ew18.md: FR-003 enrichment contract pinned — reuse `extractTargetFiles` (main.go:949), detached text-mode subagent per pan.RunPi (not rpc), brownfield trigger = `git cat-file -e <base>:<path>` with base from `ResolveIntegrationBase`, anti-laundering gate in the emitter (unclaimed summary lines dropped), budget reuses `context_pack.max_pack_tokens` (default 40000) with `elided: n` footer, output committed as `docs/hydra/handoff/enrich-<bead>.md` picked up by `loadChainContext` (no new channel), injectable agent runner for fake-agent tests.
- CHANGELOG.md: Documentation entry under Unreleased.
- No production code change — discovery bead; RED/GREEN are the next phases' obligations.

## Verification (commands run + pass/fail)
- `cm context "brownfield subagent enrichment claims summaries handoff" --json` — queried; no anti-pattern match (top bullet 0.30 score, unrelated CASS enrichment).
- `graphify query "brownfield subagent enrichment claims summaries" --budget 2000` — graph.json present (17.6M); returned existing anchors (extractTargetFiles, buildPiPrompt, loadChainContext, RPCEnabled) — parser/injection reuse confirmed.
- Code inspection verified: extractTargetFiles cmd/worker/main.go:949; loadChainContext + chainContextMaxPerFile=1500 cmd/worker/chain_handoff.go:22,76; ChainContext injection call site main.go:1571, render block ~2566; DefaultMaxPackTokens=40000 internal/core/context_pack.go:53; ResolveIntegrationBase internal/core/integration_base.go; pan.RunPi detached Pi precedent internal/pan/pan.go; RPCEnabled main.go:98-103.
- hindsight_search at bead start — not connected (skip rule applied).

## Open edges for the next phase
- RED must pin: Enrich signature, brownfield trigger matrix, claims/summary anti-laundering, `elided: n` footer, one-line greenfield skip, fake-agent double (`go test ./cmd/worker/ -run 'Enrich' -count=1`).
- A1 (GREEN blocker): enrich doc will be truncated to chainContextMaxPerFile=1500 at loader pickup — exempt `enrich-*.md` from the cap (proposed).
- A2: enrich once per bead (skip when enrich-<bead>.md exists), not per attempt.
- A3: enrich requires an ensured worktree; skip otherwise with the greenfield line.
