// Brownfield subagent enrichment (FR-003, hydra-ew18 contract).
//
// RED-phase inert stub. GREEN obligations (pinned by docs/discovery/
// brownfield-enrichment-discovery-ew18.md and the TestEnrichBeadClaims_* suite):
//
//   - `enrichBeadClaims(bead, projectPath, worktreePath, run)` returns
//     (claims []string, summary string, err error).
//   - Brownfield trigger: every target file from extractTargetFiles(bead)
//     must exist on the integration base (git cat-file -e <base>:<path>;
//     base from internal/core ResolveIntegrationBase). Any missing target →
//     greenfield: skip with exactly one log line, zero agent tokens.
//   - Subagent: detached text-mode invocation via the injectable
//     enrichAgentRunner seam (NOT rpc mode).
//   - Anti-laundering gate in the emitter: every summary line must have a
//     matching `claim: <assertion> (verify: ...)` line; unmatched lines are
//     dropped.
//   - Summary token budget: reuse context_pack.max_pack_tokens (default
//     DefaultMaxPackTokens, chars/4); overflow elides oldest assertion lines
//     first and appends footer `elided: <count>`.
//   - Output committed as docs/hydra/handoff/enrich-<bead>.md in the worktree
//     so loadChainContext picks it up (no new injection channel).
package main

import "github.com/haulmatic-dev/hydra/pkg/types"

// enrichmentImplemented gates the enforce run of the enrichment contract
// suite. GREEN must flip it to true.
const enrichmentImplemented = false

// enrichAgentRunner is the injectable subagent seam. Production
// implementation launches a detached text-mode Pi child (pan.RunPi pattern:
// --no-extensions --no-session, prompt via stdin, captured stdout). Tests use
// a fake runner.
type enrichAgentRunner func(worktreePath, prompt string) (stdout string, err error)

// enrichDefaultAgentRunner is the production detached runner. GREEN must set
// it (nil = unimplemented); tests inject their own runner.
var enrichDefaultAgentRunner enrichAgentRunner

// enrichAgentArgv returns the argv the production runner would use for the
// given prompt — test seam pinning detached text-mode (no rpc).
func enrichAgentArgv(prompt string) []string { return nil }

// enrichBeadClaims runs the enrichment subagent scoped to the bead's target
// files and returns its claims + budgeted summary. Inert until GREEN.
func enrichBeadClaims(bead types.Bead, projectPath, worktreePath string, run enrichAgentRunner) ([]string, string, error) {
	return nil, "", nil
}

// enrichDefaultBudgetTokens returns the context_pack.max_pack_tokens budget
// (default DefaultMaxPackTokens). GREEN wires the config read.
func enrichDefaultBudgetTokens() int { return 0 }
