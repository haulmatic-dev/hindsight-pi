# Brownfield subagent enrichment — discovery (FR-003, hydra-ew18)

Purpose: pin the contract GREEN must implement for FR-003 "Brownfield subagent enrichment emitting claims + summaries", resolve ambiguities, and map each AC to the existing mechanism it reuses.

## Existing mechanisms to reuse (verified on this branch)

| Need | Existing mechanism | Location |
|---|---|---|
| Target-file parsing | `extractTargetFiles(bead)` — metadata `target_files` (JSON array or comma-separated), `target_file`, deduped. **Do NOT add a second parser.** | `cmd/worker/main.go:949` |
| Token budget (default 40000) | `ContextPackConfig.MaxPackTokens` / `MaxPackTokensOrDefault()`, `DefaultMaxPackTokens = 40000` (chars/4 estimate) | `internal/core/config.go:53-71`, `internal/core/context_pack.go:51-53` |
| Oldest-first elision precedent | `ApplyPackTokenBudget` — trims oldest-first, `elided: id1, id2, ...` footer | `internal/core/context_pack.go:120-152` |
| Injection channel (frozen context) | `loadChainContext(worktree)` reads `docs/hydra/handoff/*.md`, sorted by `handoffPhaseRank`, each truncated to `chainContextMaxPerFile = 1500`; injected into prompt tail via `extras.ChainContext` at prompt build | `cmd/worker/chain_handoff.go:76-…`, call site `cmd/worker/main.go:1571` (AC text says "main.go:2562" — that is the ChainContext *rendering* block at the current line drift; the actual pickup point is the `loadChainContext` call inside `executeWithPi`) |
| Base-branch resolution | `ResolveIntegrationBase(cfg, projectPath)` — config knob > remote truth (ls-remote symref) > local symref > dev/main > checkout (remote-less) | `internal/core/integration_base.go` |
| Detached subagent invocation precedent | Pan judgment runs Pi as a detached one-shot text-mode child (`--no-extensions`, `--no-session`), not rpc mode | `internal/pan/pan.go` (RunPi) |
| rpc mode | `PiConfig.RPCEnabled()`; rpc turn owns protocol state + verdict — wrong fit for a fire-and-forget subagent | `cmd/worker/main.go:98-103` |

## Resolved decisions (GREEN should follow)

1. **Invocation mode: detached text invocation, not `--mode rpc`.** AC allows either. rpc turns own verdict protocol state and are session-shaped; the enrichment subagent is one-shot with no verdict. Follow the `pan.RunPi` detached pattern (`--no-extensions --no-session`, plain prompt via stdin, captured stdout).
2. **Brownfield trigger check:** for each target file from `extractTargetFiles`, run `git cat-file -e <base>:<path>` inside the worktree where base = `ResolveIntegrationBase(...)` for the project. All exist → brownfield (run enrichment). Any missing → greenfield → skip with exactly one log line, zero LLM tokens. Unresolvable base → skip (fail-open to no enrichment, never blocks the bead).
3. **Output contract shape:** `enrich(bead) (claims []string, summary string, err error)` where each claim is `claim: <assertion> (verify: <evidence command or pointer>)`. The **anti-laundering gate** lives in the emitter, not the subagent: split summary into lines/assertions; any line with no matching claim is dropped (fail-closed emission), never silently rendered into the handoff doc.
4. **Summary budget:** reuse `context_pack.max_pack_tokens` (default 40000, chars/4). Overflow elides oldest assertion lines first and appends footer `elided: n` (a count, per AC — distinct from the pack's `elided: id1, id2` footer; parameterize or write a local footer, do not repurpose `ApplyPackTokenBudget`).
5. **Doc artifact + injection:** enrichment output is committed as `docs/hydra/handoff/enrich-<bead>.md` in the worktree; `loadChainContext` picks it up next attempt/integration — no new injection channel. It rides the normal close-path `CommitBead` commit.
6. **Test seam:** the subagent runner must be injectable (function field), so `go test ./cmd/worker/ -run 'Enrich' -count=1` runs against a fake agent — no real Pi in tests, matching the fake-GhRunner/fake-runner precedents.

## Ambiguities for adjudication (GREEN blockers)

- **A1 — loader cap conflicts with budget:** `chainContextMaxPerFile = 1500` (chain_handoff.go:22) will truncate a 40k-token enrichment doc to 1500 chars at pickup. Either (a) exempt `enrich-*.md` from the cap, (b) raise the cap for enrich fragments, or (c) accept the 1500-char effective injection. Default proposal: (a) — the enrichment doc is already token-budgeted by AC; double-budgeting defeats it.
- **A2 — enrichment cadence:** per-attempt or per-bead? Running the subagent on every retry burns tokens repeatedly; per-bead memoization needs durable state. Default proposal: once per bead (write the doc if absent; skip if `enrich-<bead>.md` already exists in the worktree).
- **A3 — worktree availability:** enrichment needs the worktree (base-branch file check + doc write). Beads without an ensured worktree (target_file-less config tasks are excluded by the all-targets-exist trigger anyway) — confirm enrich only runs for beads with an existing worktree; otherwise skip with the greenfield log line.
- **A4 — line number drift:** AC cites `main.go:2562` for pickup; on this branch the ChainContext injection block is ~2566 and the loader call is ~1571. GREEN should target the loader/`extras.ChainContext` path, not a literal line.
- **A5 — greenfield "base branch" for repos with no remote:** `ResolveIntegrationBase` returns checkout-branch for remote-less repos; "exists on the base branch" then means "exists at HEAD". Acceptable (the worktree was cut from the base); document, don't special-case.

## Open edges for the next phase

- RED first: pin `Enrich` contract (brownfield trigger, claims/summary anti-laundering, budget footer, skip line, fake-agent test) before implementation.
- The `enrich` phase fragment must survive `handoffPhaseRank` ordering (rank 3 = last; acceptable, enrichment is auxiliary context).
- No benchmarks exist for the enrichment path; add none in GREEN unless a hot loop appears.
