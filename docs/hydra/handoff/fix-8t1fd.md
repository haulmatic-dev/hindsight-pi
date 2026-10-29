# fix handoff (fix-8t1fd, original hydra-bg9w)

## What changed (files + one line each)
- None (source). Verify-first close: GREEN for "dep-open requeues without a strike, bounded" is already implemented and committed on this integration base (`internal/core/bead_closer_hard_gates.go`: `depOpenRequeueLabelPrefix = "dep-open-requeue-"`, `depOpenMaxRequeues = 5`, `requeueDepOpen`, `clearDepOpenRequeueLabels`, `gateDependencyOrder` routing). CHANGELOG.md line 18 already carries the Fixed entry. The original bead hydra-bg9w's exit-1 was a model-call infra failure (`400 {"code":"1261","message":"[1261][prompt is too long]"}`), not a code defect — A2 per pre-implementation cross-verification.
- docs/hydra/handoff/fix-8t1fd.md: this handoff (only file added).

## Verification (commands run + pass/fail)
- `git status --short` — clean tree before handoff; implementation committed at HEAD 959536a1 lineage (feature constants present at internal/core/bead_closer_hard_gates.go:217-293).
- `go build ./...` — PASS ("[OK] Build successful").
- `go vet ./internal/core/` — PASS (VET_RC=0, clean).
- `gofmt -l internal/core/bead_closer_hard_gates.go internal/core/bead_closer_hard_gates_test.go` — clean.
- `go test ./internal/core/ -run 'TestDepOpen_|TestCloseDep_' -count=1 -v` — PASS, 9/9: TestCloseDep_ChainMemberWithOpenDependencyRejects, TestCloseDep_ChainMemberPassesWhenDependenciesTerminal, TestCloseDep_SingletonWarnOnly, TestDepOpen_ThreeConsecutiveRejectionsRequeueWithoutStrike, TestDepOpen_RequeueCounterLabelIsDurableAndSingle, TestDepOpen_FifthConsecutiveRejectionEscalates, TestDepOpen_PassingGateClearsRequeueLabels, TestDepOpen_BoundCountsConsecutiveRejectionsOnly, TestDepOpen_RequeuesDoNotConsumeStrikeBudgetOfOtherRejections (ok, 0.399s).

## AC mapping (B3)
- Dep-open requeue takes no verification strike — MET (ThreeConsecutiveRejectionsRequeueWithoutStrike + RequeuesDoNotConsumeStrikeBudgetOfOtherRejections PASS).
- Bounded — MET (FifthConsecutiveRejectionEscalates + BoundCountsConsecutiveRejectionsOnly PASS).
- Durable single `dep-open-requeue-N` counter label — MET (RequeueCounterLabelIsDurableAndSingle PASS).
- Passing dependency gate clears counter — MET (PassingGateClearsRequeueLabels PASS).
- Reject/terminal/singleton semantics preserved — MET (3 TestCloseDep_* PASS).
- NOT VERIFIED: live daemon behavior (out of scope for a worktree bead; daemon gates re-verify the full suite post-close).

## Open edges for the next phase
- None for this fix. Original failure class (prompt-too-long) is worker/provider infra, not code; if it recurs, the lever is context-pack/prompt-size budgeting, not this file.
