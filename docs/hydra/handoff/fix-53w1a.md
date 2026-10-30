# fix handoff (fix-53w1a, original hydra-lbj0)

## What changed
- None (source). Verify-first close: GREEN for "dep-open requeues without a strike, bounded" is already implemented and committed on this integration base — `internal/core/bead_closer_hard_gates.go` carries `depOpenRequeueLabelPrefix = "dep-open-requeue-"` (durable consecutive counter, lines 217-222), `depOpenMaxRequeues = 5`, `requeueDepOpen` (no `rejectClose` call → no verification strike; 5th consecutive rejection escalates with `escalated` + `needs-human` + `verified-close-escalated` labels + one `bead.escalated` event), `clearDepOpenRequeueLabels` (passing gate / no-deps clears the counter), and `gateDependencyOrder` routing chain-member dep-open rejections through the requeue path (lines 40, 186-296). CHANGELOG.md already carries the matching Fixed entry under Unreleased. Same finding as sibling fix bead hydra-8t1fd (original hydra-bg9w).
- docs/hydra/handoff/fix-53w1a.md: this handoff (only file added).

## Verification
- A2 evidence: the original bead hydra-lbj0's exit-1 was pure worker infra — compaction 429 `no account has capacity: disabled` (summarization retries ×2 exhausted), `generation hit the token cap and the summary is incomplete`, then `Activity timeout reached (no output for 10m0s)` / `Pi failed with exit code -1`. No code defect was ever produced; the garbled verdict contained the worker-nudge text, not a failure finding.
- `go test ./internal/core/ -run 'TestDepOpen_|TestCloseDep_' -count=1` — PASS, 9/9 (exit 0, test binary run): TestCloseDep_ChainMemberWithOpenDependencyRejects, TestCloseDep_ChainMemberPassesWhenDependenciesTerminal, TestCloseDep_SingletonWarnOnly, TestDepOpen_ThreeConsecutiveRejectionsRequeueWithoutStrike, TestDepOpen_RequeueCounterLabelIsDurableAndSingle, TestDepOpen_FifthConsecutiveRejectionEscalates, TestDepOpen_PassingGateClearsRequeueLabels, TestDepOpen_BoundCountsConsecutiveRejectionsOnly, TestDepOpen_RequeuesDoNotConsumeStrikeBudgetOfOtherRejections.
- `go build ./...` — PASS ("[OK] Build successful").
- `git status --short` — clean tree before handoff (implementation committed at HEAD ac178caf lineage).
- Evidence grep: all five mechanism symbols present in internal/core/bead_closer_hard_gates.go (gateDependencyOrder routing line 40; constants 221-222; clear 252; requeue 274; escalation branch 283-293).

## AC mapping
- Three consecutive dep-open rejections each requeue without a verification strike — VERIFIED (TestDepOpen_ThreeConsecutiveRejectionsRequeueWithoutStrike + TestDepOpen_RequeuesDoNotConsumeStrikeBudgetOfOtherRejections).
- Fifth consecutive rejection escalates (escalated / needs-human / verified-close-escalated) — VERIFIED (TestDepOpen_FifthConsecutiveRejectionEscalates).
- Counter durable (labels) and counts consecutive-only — VERIFIED (TestDepOpen_RequeueCounterLabelIsDurableAndSingle + TestDepOpen_BoundCountsConsecutiveRejectionsOnly).
- Passing gate clears requeue labels — VERIFIED (TestDepOpen_PassingGateClearsRequeueLabels).

## Open edges
- None for this bead. (Known adjacent recorded-not-fixed item from refactor-ank6/k991: `gateExecutableAcceptance` never sets `cmd.Dir` — separate concern, out of scope here.)
