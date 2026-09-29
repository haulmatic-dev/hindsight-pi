# Changelog

All notable changes to Hydra are documented here. Every PR (human-, Claude-, or
Hydra-authored) MUST add an entry under **Unreleased** — see AGENTS.md
"Documentation & Changelog Discipline". Format follows
[Keep a Changelog](https://keepachangelog.com/); newest first within a section.

## [Unreleased]

### Added
- Target-scope overlap serialization at spawn (FR-003): a ready bead whose declared target files overlap a live worker's declared targets is deferred (skipped this tick, typed event `spawn-deferred-target-overlap`, fail-open) instead of spawned — two workers on the same files race, each close-path target-scope gate then mis-attributes the other's edits as off-scope, and merge integration hits structural conflicts. Reuses the close-path `beadTargetFiles` parser (no second parser); targets are normalized (symbol suffix stripped, `filepath.Clean`) exactly like the close-path scope gate. Consulted on both the scheduler spawn funnel and the retry queue; store-read failures fail open and the close-path gate remains the backstop. Tests: `TestTargetsOverlap`, `TestTargetScopeConflict_*` (internal/core/target_overlap_test.go).

### Changed
- Chain handoffs are per-bead fragments (FR-001, autonomous-resolution-layer PRD): the worker prompt now directs every bead that changed files to write `docs/hydra/handoff/<phase>-<bead-id>.md` instead of the shared `docs/hydra/handoff/<phase>.md`. Parallel beads of the same phase (notably discovery FRs) previously collided on one file — `discovery.md` alone accounted for 568 of 570 integrate failures. `handoffPhaseRank` ranks fragments by phase prefix so `green-5xvh.md` still sorts with green; the loader needed no change. Tests: `TestChainHandoffFragmentName`, `TestHandoffPhaseRankFragments`, `TestLoadChainContextFragmentOrder` (cmd/worker/chain_handoff_test.go).

### Fixed
- Stranded-work blind spots closed (FR-021, 2026-09-29): beads quarantined by `integrate-stranded` (label `stranded-conflict`) were invisible to Pan — the blocked-bead label sweep only knew the hard-park labels — so a merge-conflicted worktree sat unresolved forever. The sweep now surfaces open `stranded-conflict` beads as BLOCKED candidates (policy extracted into `panBlockedReason` for testability). Symmetrically, the stranded detector skipped open beads as "still resumable", but open beads labeled `stranded-conflict`/`integration-unverified` are no longer resumable; the detector now accepts them and re-labels with `stranded-work` for dedup. Tests: `TestPanBlockedReason` (internal/core/service_starter_pan_labels_test.go), `TestDetectStrandedWork_AcceptsOpenQuarantineLabeledBead`, `TestDetectStrandedWork_AcceptsOpenIntegrationUnverifiedBead` (internal/core/stranded_work_test.go). *Authored by a Hydra dogfood run.*
- Daemon-side Hindsight pre-injection was silently empty fleet-wide (2026-09-28): `DefaultContextProcessorConfig().MaxQueryLatency` was 500ms while measured server-side recall p50 is ~4.6s, so every `queryHindsightForContext` call was cancelled and fell into the "No relevant memories" path. Default raised to 15s (recall is fail-open), the recall now runs concurrently with worker startup via `hindsightContextFuture` (awaited lazily at prompt-build, so multi-second recall overlaps skill-flag building and Pi spawn — ~0 added wall time), and the daemon pre-query now sends `budget: low` (new `hindsight.WithBudget` option) to bound candidates server-side and cut rerank latency. Tests: `TestDefaultContextProcessorConfig` (agents), hindsight package suite. *Authored by a Hydra dogfood run.*
- Beads hard-parked with the `integration-exhausted` label are no longer respawned by the scheduler retry queue, the DAG ready filter, or bead discovery (ag6i, 2026-09-27): a bead whose close failed at integrate/push was labeled `integration-exhausted` (quarantine) by `failIntegration`, but only Charon and the Pan blocked-scan consulted the label — the other three respawn paths kept spawning it (~95 respawns of one bead in 24h, each re-emitting its verdict in ~30s; 474 spawns vs 57 beads, ~49 worker-hours). All three paths now skip the label; only Pan repair or a human clears it. Tests: `TestGetReadyBeads_SkipsIntegrationExhausted`, `TestFilterByDAG_FiltersIntegrationExhausted` (internal/core/integration_exhausted_gate_test.go). *Authored by a Hydra dogfood run.*
- Orphan recovery no longer bypasses the park funnel (2026-09-27 402 outage): a frozen provider makes workers look dead (stale heartbeat), so Charon `recoverOrphanInProgressBeads` reopened in-progress beads of PAN-parked campaigns and the scheduler respawned them straight through the park. Orphan recovery now skips parked projects entirely (`registry.ProjectParkInfo`), matching `campaignParked` on both spawn paths. Test: `TestRecoverOrphanInProgressBeads_SkipsParkedCampaign` (internal/core/charon_orphan_test.go). *Authored by a Hydra dogfood run.*
- Daemon gates no longer die on the 10m go-test timeout (hydra-c0qj gate runs, 2026-09-27): `failIntegration` sleeps its exponential integrate backoff (30s→210s cumulative) inside tests that loop it, so `internal/core` spent ~390s asleep and exceeded the go-test 10m per-package timeout — the gate showed `panic: test timed out after 10m0s` with zero test failures. `integrationBackoffBase` is now a var and `internal/core`'s TestMain pins it to 1ms; the package drops 444s → ~50s. Production backoff unchanged (30s base, verified via the same constant's non-test default).
- Duplicate integrate after a successful PR landing no longer logs a scary `[Integrate] FAILED` + CloseBead WARN (hydra-kf2t, 2026-09-27): Charon orphan recovery respawned a worker while the first close was still running its multi-minute daemon gates (the bead stays `in_progress` until gates finish), so a second close raced the first into singleton integrate. The loser's `gh pr create` failed with `GraphQL: No commits between development and dogfood/hydra-kf2t` — benign, but logged as an integration failure. Three guards: (1) the close path re-checks bead terminality after the gate run and skips integrate/close if a racing close already closed the bead; (2) `publishAfterIntegrate` skips the PR entirely when the local tip is already contained in the remote base branch (`tipMergedProbe`: fetch + `merge-base --is-ancestor`, fail-open); (3) a `gh pr create` "No commits between" error is classified as already-landed and treated as a landed no-op instead of an integration failure. Tests: `TestPublishAfterIntegrate_PRTipAlreadyMerged_NoOp`, `TestPublishAfterIntegrate_PRNoCommitsBetween_Benign` (internal/core/publisher_test.go). *Authored by a Hydra dogfood run.*

### Added
- Host disk-watermark telemetry in the Pan fleet digest (2026-09-25 disk-full incident, br I/O error 28): `collectHealthDigest` now stats the filesystem holding `/` (which holds /tmp, $HYDRA_HOME and the working state) and renders a `host: disk=N%` anomaly line in the digest text when usage exceeds the 85% watermark (`defaultDiskWatermarkPct`, overridable via `Overseer.SetDiskWatermark`). Telemetry only, fail-open on statfs errors — host anomalies never become campaign-anomaly pseudo-candidates, because the repair verbs resolve a project from the key and "host" is not a project. Tests: `internal/core/pan_health_test.go`, `internal/pan/health_test.go`.
- Disk janitor artifacts (same incident): `packaging/systemd/hydra-tmpfiles.conf` ages /tmp entries after 7 days (the incident found 237.6G across 8015 top-level entries — leaked mongo-mem fixtures, npm/cargo temp, worker execution reports — with no rule) and /root/.cache + /root/.hermes/sessions after 30 days; install is documented in host-setup.md §9c. §9c now also runs `systemd-tmpfiles --reload /etc/tmpfiles.d/hydra-tmpfiles.conf` after copying, so the rule takes effect without a reboot.
- Fix-bead verify-first directive (operator 2026-09-25, the tin0/jx78 case): fix beads previously told the worker to "Address the failure above, then re-run the original task", so a worker redid 60+ minutes of already-green work after a post-verdict exit-1. Fix-bead prompts now lead with a VERIFY-FIRST step: re-run the original verification commands against the inherited worktree; if they pass, close immediately citing that evidence and do NOT modify files — only redo what verification proves broken. The fix bead's description additionally carries the original attempt's recorded HYDRA_VERDICT (ac_met list verbatim, extracted from the worker log via new `ExtractRecordedVerdict` in internal/core/fix_cause.go). Tests: `cmd/worker/phase_guidance_test.go`, `internal/core/fix_cause_verdict_test.go`, `internal/core/bead_closer_fix_cause_test.go`.
- Pan closure-audit duty (operator 2026-09-25, the jx78 hollow-verdict case): open auto-fix beads (`label auto-fix`, type `fix`) now surface as `FIX-AUDIT:` judgment candidates so Pan can close a redundant fix bead before a worker redoes finished work. Pan never trusts a recorded verdict alone — each candidate carries the close reason, the recorded HYDRA_VERDICT snippet, and a daemon-side re-verification that re-runs the original bead's scoped test gate in the original's inherited worktree (30-minute cache; unknown on any doubt). Verdict claims done AND verification confirms → `close_satisfied` (the satisfied gate still re-proves full acceptance daemon-side); verification contradicts the verdict → the fix proceeds and Pan flags the original verdict as hollow. Division of labor per the mandate: closer keeps crisp mechanical closes, Pan owns judgment + cheap re-verification, fix-worker verify-first stays last resort. Tests: `internal/pan/fix_audit_test.go`, `internal/core/fix_audit_test.go`.
- Pre-close diff-verification gate accepts empty closes with a worker-declared reason (Class-2 fix-bead reduction, duty 2): when the verified-close diff gate finds no changes on the bead's declared targets, the closer now reads the worker's last HYDRA_VERDICT from its log and honors three self-reported accepted-empty reasons — `docs-only` (every changed path is documentation), `chain-deferred-to-root`, and `superseded` (both require a truly empty diff). Verdict must be done:true; failed verdicts are never honored. Empty diff with NO accepted reason still rejects and reopens exactly as before (reopen with prior context, never a fix bead), and an accepted docs-only close also skips the target-scope gate, which would otherwise immediately re-reject the same close for touching non-code paths. New helpers in `internal/core/empty_close_gate.go`; wiring in `verifyBeforeClose` (bead_closer.go); coverage in `internal/core/bead_closer_empty_gate_test.go`.

### Fixed
- PublishViaPR now falls back to the daemon's own GitHub credential when the caller passes an empty token (operator handcode, 2026-09-28): the periodic publish tick only carries the project owner's PAT, which is absent for daemon-owned repos, so the hydra repo's tick looped every 8 minutes on `gh pr create: exit status 4` (unauthenticated). New injectable `prMergeResolveToken` seam + test asserting the fallback token reaches push/create/merge.
- Daemon dogfood pushes no longer run client-side pre-push hooks, and push errors no longer leak the GitHub PAT (operator handcode, 2026-09-27): the pr_merge dogfood push went through husky's `lint:ci && build` hook, so one eslint error (`EventLike` unused import) in integrated telematics work rejected EVERY dogfood push and stranded 64 verified closes as local-only commits; and the failure's error string carried the full authenticated push URL — token included — into the daemon log 906 times. `defaultPRMergeGitPush` and `defaultAutoPRGitPush` now pass `--no-verify` (the daemon already ran its own gates) and route error text through `redactToken`. Two behavioral tests: a failing pre-push hook no longer blocks the push (with an armed-hook control), and an unreachable-host failure contains `***` instead of the token.
- AutoPR no longer strands every verified close behind GH013 branch protection (operator handcode, hydra-f40k, 2026-09-25): the protected-push fallback saw the bead-owned `dogfood/<id>` branch already on origin and skipped the entire step — including PR creation and merge — so five verified closes (o91/ehp/rki/04f/s0s) plus the 7m2 br-upgrade rollout stranded in local integration commits while their beads sat terminal; recovery took two manual dogfood PRs (#294 partial + #295 full). The fallback now distinguishes the three real states: branch + open PR → attempt `gh pr merge --admin` immediately (the earlier integration may never have merged it; only a verified MERGED state counts as preserved); branch without open PR → stranded artifact, recovered via `PublishViaPR` (force-update the bead-owned branch, open the PR, merge, verify MERGED); fresh branch → same `PublishViaPR` path, so a created-but-unmerged PR can never again count as landed. New `autoPRGhListOpen` exec hook (`gh pr list --head <head> --state open`). Fail-open on every gh/git error; fail-closed on unmerged. Tests: `TestAutoPR_MergesExistingOpenPR`, `TestAutoPR_RecoversBranchWithoutOpenPR`, `TestAutoPR_FailClosedWhenMergeFails` (internal/core/publisher_test.go). Operators: set `output.pr_merge: true` to route integrates through the same verified-merge path as the primary flow — the direct-push-plus-fallback combination exists only for repos without the knob.
- Pan re-judged stale parked candidates every tick (dup-continue trickle, 2026-10-04): the judgment dedup keyed a WHOLE-SET signature, so any volatile change in one candidate (token-budget counts, digest text) reset the signature and re-judged every stable parked candidate at the tick interval — each verdict a duplicate `continue` the pending queue then skipped (`skipping duplicate continue for ... (already pending)`), constant small GLM burn with zero new information. Dedup is now a per-candidate cache keyed by candidate state hash (bead ID + state + normalized reason); a candidate is re-judged only when its state changes or the 15-minute re-judge valve expires per candidate. Tests: `TestJudgmentDedupPerCandidateIsolation` (internal/pan/judgment_dedup_test.go). *Authored by a Hydra dogfood run.*
- Worker env no longer carries the inert `KIMI_API_KEY` (routing-audit leftover): workers spawn pi with provider `kimi-router` (baseUrl `127.0.0.1:9900`) where the router injects the upstream key itself, so the worker env builder's built-in `KIMI_API_KEY` default was dead weight that kept confusing routing audits and would silently re-activate if a direct `kimi-coding` provider were ever referenced. `WorkerEnvBuilder.Build` now strips inherited `KIMI_API_KEY`, forwards `HYDRA_PI_API_KEY_NAME` only when `api_key_name` is explicitly configured (no implicit default on either daemon or worker side), and the daemon appends an API-key env var only for explicitly configured key names. Operator follow-up outside the worker workspace: prune the now-unused `kimi-coding`/`kimi-coding-plan` providers from the shared Pi config `models.json` — the hydra worker cannot do it (files `root:pi` 0644, hydra uid has no write; requires operator sudo). Order matters: `settings.json` `defaultProvider: kimi-coding-plan`/`defaultModel: kimi-k2.7-code` must move to `kimi-router`/`kimi-for-coding` first, then both providers are safe to drop (`kimi-router` exposes the same model ids). Tests: `internal/core/worker_env_builder_test.go` (TestWorkerEnv_NoImplicitKimiAPIKey). *Authored by a Hydra dogfood run.*
- Pan no longer parks campaigns on human-problem publisher wedges (operator 2026-09-25, 3 unpark fights in one morning): the shepherd's judgment prompt treated `park-campaign` as the default remedy for any `-campaign-anomaly` candidate, so a publisher wedge caused by GH013 branch protection (human-only approval gate) got the campaign frozen — an action operators then had to undo by hand for a cause only humans can fix. The prompt's campaign-anomaly recipe now says a publisher-wedge anomaly whose reason indicates branch protection, required approvals, or permissions is a HUMAN problem, not a chain-discipline violation: choose `repair:log-improvement` naming the human action and rules URL, never `repair:park-campaign`. Test: `TestBuildPromptPublisherWedgeIsHumanProblem` (internal/pan/pan_test.go). *Authored by a Hydra dogfood run.*
- ContextPack write loop no longer hammers an unwritable br store (root-cause of the 2026-09-25 br-show exit-8 storm's lock contention): a project whose pack writes fail (e.g. the daemon user cannot write the store) was rebuilt and retried on every 2-3s scheduler tick forever. `ensureContextPack` now rides a per-project circuit breaker (`internal/core/context_pack.go` + `worker_spawner.go`): each failed write pass schedules exponential backoff (1m doubling, 30m cap) during which spawns proceed unguided, 3 consecutive failures open the breaker with one typed `pack_breaker_open` telemetry event, and a successful pass emits `pack_breaker_close` and resets. Fail-open throughout: the pack is an optimization and never gates a spawn. Tests: `internal/core/context_pack_breaker_test.go`.
- HYDRA_VERDICT emission is now idempotent per bead (bead gwce follow-up): a repeated verdict with identical content is consumed instead of re-emitted — worker-side `dedupVerdict` (cmd/worker/verdict.go, dedup key = bead ID + SHA-256 of the normalized verdict JSON) drops a re-emitted duplicate at verdict acceptance, and the daemon-side `VerdictRetainer.Retain` (internal/core/verdict_memory.go) drops a duplicate verdict-memory write per (bead_id, output hash). A genuinely new verdict (agent kept working and changed its claim) still passes through both gates. Prevents double verdict-memory writes and re-entry storms; tests: `TestDedupVerdict_*` (cmd/worker), `TestVerdictRetainer_DuplicateVerdictConsumed` / `_DistinctVerdictsRetained` (internal/core). *Authored by a Hydra dogfood run.*
- Hollow-green RED verdicts rejected: a `done:true` HYDRA_VERDICT whose verification entries contain suite load crashes (`Cannot find module`, `Test suite failed to run`, `SyntaxError`) is downgraded to `done:false` with the load errors recorded as unmet ACs, and the nudge loop tells the agent why. Expected-failure wrappers over suites that fail to import are not written tests — proven live when RED bead ajantha-srv-telematics closed done:true claiming "exit 0, 9 tests" while its suites died at load (`Cannot find module '../e2e-harness'`) and the wrapper was only caught by it.failing expected-failure counting. Gate lives in `normalizeVerdict` (cmd/worker/verdict.go) at verdict acceptance, before retention and before turn-end closure; a clean verification pass is untouched. Tests: `cmd/worker/verdict_test.go` (TestNormalizeVerdictHollowGreen). *Authored by a Hydra dogfood run.*
- DAG edge loading no longer melts the br store under load: `bv --robot-graph` (one call per repo, ~0.3s) replaces the per-bead `br show --json` N+1 pattern (86 contended CLI calls per rebuild). On bv failure the builder falls back to per-bead fetch, and a bead whose dependencies cannot be determined by EITHER path is marked `deps_unknown` and held back from readiness (fail-closed) — root cause of the 2026-09-25 gate-leak incident, where br-show lock contention silently dropped 43 HOLD-gate edges and 43 campaign beads scheduled against an explicit pause. HOLD beads themselves (type/label `hold` or `HOLD:` title prefix) are now excluded from both scheduling enumeration paths AND — the durable fix — at the spawn funnel (`ShouldSpawnWorker`) and the spawner itself (`SpawnInternal` refuses HOLD beads): enumeration-site filters leak, as proven when the immediate-scheduling path (BV-Plan candidates → filterByDAG → SpawnWorkersForNodes) reached the ready gate directly and a worker executed-and-closed it, silently unblocking the campaign twice in one day (telematics mz0v and zibq, 2026-09-25). bv upgraded to v0.25.0 for the edge-type-complete `--robot-graph` output. The retry-queue spawn path (which bypasses `ShouldSpawnWorker`'s DAG-node check by design) now also consults DAG InDegree directly — fix-bead retries rcoy/tin0/qfcr spawned straight through the pause gate during the same incident because that path never looked at blocking edges. Tests: `internal/core/dag_edges_failclosed_test.go`.
- Parallel same-repo integrates no longer die on append-only shared-file conflicts (root cause of the 2026-09-24 telematics closed-failed wave cd8h/yc2/tg7g/rylg): (1) the repo-local union merge driver now covers both `CHANGELOG.md` and `.gitignore`, and the integrate merge additionally auto-resolves by union when ALL conflicted paths are append-only shared files (both sides' lines survive, matching git's merge=union semantics) — real-code conflicts still abort; (2) `failIntegration` now applies exponential backoff (30s→8m cap) before returning, and a retry arriving sooner than the base interval is logged as rapid-fire contention and does not advance the consecutive-failure exhaustion cap; (3) the `integration-exhausted` event carries a `conflict_paths` field parsed from the merge failure (fail-open telemetry). Regression tests: `internal/core/union_shared_conflicts_test.go`, extraction unit test. *Authored by a Hydra dogfood run.*
- Worker execution reports (`/tmp/hydra-executions/*.json`) finally carry real `duration`, `attempts`, and `turns` plus the token breakdown: the report was written inside `executeWithPi`, before the caller set `Duration` and `Attempts`, so every record showed duration=0/attempts=0 and the turn-reduction program had no per-session baseline. Write moved to `runWorker` after those fields are final; `ExecutionResult.TurnCount` added (nudge turns included) and `readExecutionOutputFromFile` now parses duration/attempts/turns/tokens back. *Authored by a Hydra dogfood run.*
- HYDRA_VERDICT re-entry investigated (bead gwce emitted done:true twice 2026-09-24): not a verdict-parsing bug — both worker runs parsed done:true and succeeded; the daemon close path failed at integration (`[Integrate] FAILED ... CONFLICT (modify/delete): test/jest-e2e.json deleted in HEAD and modified in hydra/wt-...`) so the bead stayed open and was re-executed, then hard-parked `integration-exhausted`. Behavior is the designed fail-closed path; no worker change made.

### Added
- Pan tier-1 trajectory intelligence: the shepherd now judges worker trajectories, not just snapshots. `collectWorkerTrajectory` (`internal/core/pan_trajectory.go`) parses a live worker's pi session jsonl (turn_end events, bash commands, edit/write calls) into a `pan.Trajectory`; `internal/pan/trajectory.go` fires conservative tier-1 flags — turns > 2× bead-class baseline (SYNTAX 5 / RED 20 / GREEN 30 / fix 10 / default 40), same command repeated ≥ 8×, ≥ 6 file edits without an intervening test run, ≥ 8 test runs — and only flagged workers surface as `TRAJECTORY:` candidates for Pan's tier-2 LLM judgment (steer / close_satisfied / abandon-with-summary, all existing verbs; the prompt's rescue recipes now name the tier-2 protocol: converging → continue/steer, goal already satisfied → close_satisfied with evidence, clearly looping → abandon-with-summary, never abandon slow-but-progressing). Tier-1 also covers no-new-files-in-N-turns (≥ 15 turns with edits but zero file-creating writes) and same-file-reedited-without-test-run (≥ 3 edits of one file since the last test run). Root cause addressed: tin0 ran 73+ minutes redoing committed work while every mechanical signal looked healthy. Fail-open (unreadable session disables the class per bead), young workers (<10 min) exempt, and healthy trajectories never burn the judgment budget.
- Pan judge-degradation alerting: the shepherd now counts consecutive pi-judge invocation failures; at 5 it logs `[Pan] ERROR judge DEGRADED` and emits a typed `pan_judge_degraded` event (re-firing every 25 further failures), and on the next successful invocation emits `pan_judge_recovered` with the outage length. Root cause of why: an audit on 2026-09-23 found 6494/6494 judge invocations had failed across Aug–Sep (exhausted MiniMax Token Plan, a hydra-side `--thinking-level` arg incompatibility, OOM kills) with zero signal beyond one `log.Printf` per tick — every Pan judgment for weeks was the deterministic-rule fallback and nobody knew. New hooks `OnJudgeFailure`/`OnJudgeRecovered` on the shepherd, wired to the daemon event sink in `service_starter.go`.
- Integration-retest stage (Tier A, opt-in `integration_retest.enabled`): the overseer tick periodically rechecks `integration-failed`/`integration-exhausted` beads whose `hydra/wt-*`/`hydra/quarantine/*` branch still exists — `merge-tree --write-tree` against `origin/<base>`, gates in a scratch worktree (never the user's main checkout), and provably-clean branches land via the existing dogfood PR path (`dogfood/<id>-retest` + `gh pr merge --admin`, verified MERGED) with the bead closed and labels stripped. Rate-limited (default 1h interval, 5 attempts then give up), emits typed `integration_retest` events, fails open. Conflicts are never resolved here (Tier B is a separate bead). *Authored by a Hydra dogfood run.*

### Fixed
- OpenRouter 402 `billing_error` ("requires more credits, or fewer max_tokens") now classifies as quota exhaustion (exit 6) instead of a code failure: the 2026-09-28 credit outage killed every worker turn and respawned it ~8min later with no spawn freeze, because none of `quotaErrorMarkers` matched the payload. New markers `billing_error`, `requires more credits`, `insufficient credits` feed the existing `RecordQuotaExhausted` 30-minute spawn pause. Tests: `cmd/worker/quota_classification_test.go`. *Authored by a Hydra dogfood run.*
- Pan judgments now invoke pi with `--no-extensions`: Pan's prompt is fully self-contained yet every judgment was paying the full interactive extension load (measured on the worker path at 48,315 vs 12,059 tokens/call). Per-tick tax removed; matches the worker invocation doctrine.
- Runtime max-workers updates now reach the AIMD concurrency governor: `POST /api/v1/overseer/max-workers` (and config reload) updated only the state controller, leaving `ConcurrencyGovernor.maxWorkers/limit` at construction-time values, so `effectiveMaxWorkers = min(new, governor)` silently clamped any runtime increase until daemon restart (observed 2026-09-24: bump 8→12 with slots stuck at 8/12). `Overseer.SetMaxWorkers` now also calls the new nil-safe `ConcurrencyGovernor.SetMaxWorkers` (increase raises the limit to the new ceiling immediately; decrease clamps down), exposed via `Overseer.GetConcurrencyLimit()`, pinned by `TestSetMaxWorkersHandler_SyncsGovernor`. *Authored by a Hydra dogfood run.*
- br store rewrites no longer leave multi-user stores flapping locked (2026-09-22 02:23–03:33 telematics + buddhi `pack_created write failed ... exit status 8` storm): `br` recreates `.beads/issues.jsonl` mode 0600 on every rewrite, and the daemon's unit PATH put the raw beads-rust release dir ahead of `$HYDRA_HOME/bin`, so daemon br calls bypassed the `packaging/br` shim entirely and nothing repaired the owner-only mode after a non-shim write. Two changes: (1) the shim now stat-probes the store after read-only subcommands (`list`/`show`/`ready` — the daemon's every-tick polls) and repairs group perms when the probe shows regression, instead of never repairing until a mutating call; (2) `scripts/deploy.sh` installs the shim to `$HYDRA_HOME/bin/br` and both unit templates put `$HYDRA_HOME/bin` first on PATH so the daemon execs the shim, not the raw binary. Regression coverage in `packaging/br-shim-test.sh` (14 assertions). Upstream beads-rust root fix (setgid `.beads` → export 0660) ships as a git-apply-verified patch for root-side apply at `docs/hydra/handoff/beads-rust-jsonl-setgid-0660.patch` — the infra checkout is root-owned and not writable by the daemon user. *Authored by a Hydra dogfood run.*
- Publisher no longer fails open *silently*: `PublishRepo` emits typed events (`publish_push_attempted`, `publish_push_succeeded`, `publish_push_rejected`, `publish_pr_invoked`, `publish_pr_opened`) and a log line for every push/PR attempt, rejection, and skip — the srv-telematics campaign (2026-09-18..22) integrated ~28 times locally with zero push log lines and zero PRs, closing 50 beads of invisible work. Events flow through the wired `SetPublishEventSink`; fail-open behavior is unchanged (an event-write failure can never break a push). Also: push-pending older than 15 minutes now surfaces as a `publisher wedge` anomaly in the Pan health digest, so locally-merged-but-unpublished work is Pan-detectable before it strands a campaign. *Authored by a Hydra dogfood run.*
- `IntegrateBranch` no longer merges into the human's main checkout: the `--no-ff` merge runs in a daemon-owned scratch worktree (detached at the checkout's tip), then the checkout's branch advances by fast-forward — or by bare `update-ref` when uncommitted human WIP overlaps the files the merge touched (WIP files stay byte-identical on disk). The old in-checkout merge required a clean checkout, and the janitorial `wip: hydra auto-commit` fallback's gates failed 288 times during the telematics campaign, poisoning every integrate into quarantine. `autoCommitDirtyMain` is no longer on the integrate path. *Authored by a Hydra dogfood run.*
- Standalone beads now receive context packs via chain-of-one fallback in `ensureContextPack`: previously the function skipped any bead not in a registered chain, leaving standalone fix-beads (the 68-turn p50 unguided cohort) without guidance. A chainless bead is now treated as `AllBeadIDs=[self]`, producing the same mechanical pack, SHA-keyed refresh, and prompt-tail injection as chained beads. Covered by `TestEnsureContextPack_StandaloneBeadGetsChainOfOnePack`.
- Live acceptance test hygiene for worktree registrations (hydra-g4e follow-up, 2026-09-21): the ad-hoc live acceptance run against the real buddhi fixture repo left a `hydra/wt-*` worktree registration behind, so the next real spawn failed with `branch already used by worktree` (git exit 128) until manual prune. `internal/core/spawn_path_resolution_test.go` now ships `removeWorktreeRegistration` (force-remove worktree + prune + delete the actual checked-out `hydra/wt-*` branch, best-effort) with unit coverage, plus an opt-in `TestSpawnInternal_LiveAcceptanceWorktreeCleanup` (`HYDRA_LIVE_SPAWN_VERIFY=1`, optional `HYDRA_LIVE_SPAWN_REPO`) that registers the cleanup via `t.Cleanup` against the real fixture repo. Audit of every `EnsureWorktree` test found all others already use `t.TempDir()` repos.
- Sibling `file:` dependencies are provisioned into the repo's worktree root at `EnsureWorktree` time: npm install resolves `file:../lib-nestjs` (e.g. srv-telematics' `@haulmatic/common`) as a node_modules symlink relative to the worktree, which dangled because the sibling only exists in the main checkout's parent — preflight then failed with "dependency check failed after install: 1 broken symlink(s)" and the bead looped on spawn failure (telematics incident 2026-09-21). `provisionSiblingDeps` scans package.json `dependencies`/`devDependencies` for `file:` specs, resolves each against the project path, and creates an idempotent symlink at the shared worktree root pointing at the sibling's on-disk path (wrong/dangling links are repaired; missing siblings are logged and skipped, fail-open). *Authored by a Hydra dogfood run.*
- Project park now suppresses retry-queue and fallback-path spawns, not just DAG-path spawns: the retry-queue loop in `Scheduler.Tick` and the scheduler-less `Overseer.spawnWorkersForNodes` fallback called `spawnWorker` without consulting `campaignParked`, so a parked campaign kept retry-spawning (srv-telematics spawned for hours with park rows present in the registry, 2026-09-21). Also adds the first unit coverage for `campaignParked` (parked / unparked / nil-registry fail-open). Operators can again trust that parking a project actually stops new workers.

### Added
- Multi-user discovery auto-onboards the beads-group permission convention on every discovered repo (bead: telematics-incident repro 2026-09-21, where a user-owned scratch repo without the convention failed every spawn with `exit status 8`/EACCES): `.beads` becomes group `beads`, `g+rwX,o-rwx`, setgid, `issues.jsonl` repaired from br init's mode-0600 to group rw, and `<repo>/.git/worktrees` group-shared. Idempotent — repos already conforming are not touched — and fail-open: repos the daemon user cannot chgrp/chmod (another user's files) are logged and left for the root-run `hydra-onboard` sweep. Convention documented in `docs/operations/host-setup.md` §8. *Authored by a Hydra dogfood run.*
- RTK end-to-end evaluation decision record (`docs/operations/rtk-evaluation.md`): fleet transcript analysis over 996 sessions (1,602 compactions, 34% later re-opened, median 2-turn recovery but heavy tail) plus the 2026-09-21 controlled micro A/B (underpowered, 1 compaction event). Decision: keep rtk in the fleet allowlist; reconfigure follow-up (`preserveExactSkillReads: true`, raise compaction floor) with a two-window fleet A/B procedure and decision rule. *Authored by a Hydra dogfood run.*
- Context pack agentic scout fallback (bead 2 of the orientation-burn reduction program): when the mechanical pack leaves chain-member requirements unmapped (best graphify anchor below confidence 0.7), the daemon runs ONE bounded read-only Pi scout pass per chain build — `max_turns` turn cap (default 20) enforced by prompt budget plus a wall-clock process-group kill, fleet provider/model identity so spend lands in the normal llm-router lane accounting, output merged into the pack's `files` as provenance-`agentic` anchors (symbol + verbatim anchor snippet) and persisted to every member's `agent_context` as before. Re-scout is targeted only: at SHA refresh, agentic anchors that broke are re-scouted per-requirement, never a whole-chain redo; a daemon-wide mutex stops overlapping spawns double-spending. Typed `pack_scout` event (`scout_ok`/`scout_failed`, detail carries requirement/mapping counts + duration) attributes the cost; scout failure is fail-open — the spawn proceeds with the mechanical-only pack. Gated by `context_pack.scout.enabled` (default false). Full design: docs/architecture/context-pack.md. *Authored by a Hydra dogfood run.*
- SoL-Pi pilot support (Action Fusion + ObservationPack, conservative vendor config only): when `sol-pi` is in the phase extension allowlist and its package resolves under the shared Pi config, the worker writes the conservative `.pi/sol-pi.json` into the bead worktree, excludes `.pi/` via the worktree-local `.git/info/exclude`, and grants per-run project trust with `--approve` (SoL-Pi reads project config only when the project is trusted and rpc mode never prompts). Provisioning failure or a missing package is a logged no-op — the extension loads with everything disabled. EPR and Online Context Compact stay disabled by policy (extra remote call / breaks frozen-prefix caching). Operator guide + A/B protocol: `docs/operations/sol-pi-pilot.md`. *Authored by a Hydra dogfood run.*
- Chain context packs (bead 1 of the orientation-burn reduction program): the daemon builds a mechanical context pack just-in-time at chain first-member spawn — gate commands from the project's `gates` block, dominant spec-pattern scan, 2-level target-dir listing, and best-effort graphify requirement→file anchors — and writes it to every chain member's `agent_context` via `br update --agent-context` (br 0.2.10+). The pack refreshes SHA-keyed at each member spawn (mechanical sections regenerated on SHA move, agentic anchors re-validated with broken ones dropped as `pack_miss` events) and renders into the worker prompt TAIL only, so the frozen cache-stable head is untouched. Fail-open throughout: a pack failure logs and the spawn proceeds unguided. Typed events `pack_created` / `pack_refreshed` / `pack_miss` on the fail-open event stream. The verified-close scope gate enforces against pack file anchors when a bead declares no Target Files, so guided beads get real enforcement instead of advisory; file reservations are consulted against the same anchors with an advisory `pack_reservation_offscope` event (the reservation stream is informational — the close-time scope gate is the blocking enforcement). *Authored by a Hydra dogfood run.*
- Turn-economy measurement harness (`scripts/turn_economy.py`): productized analyzer over `{HYDRA_HOME}/sessions/<bead-id>/*.jsonl` producing per-bead/per-chain/per-campaign turn, token, zero-action, repeat-read, RTK-recovery, thinking-split, and per-phase (p25/p50/p75/max) aggregates, with gate first-try pass and empty-patch guardrails that exit 2 on regression. Replaces ad-hoc session forensics for every context-pack / prompt-tail / turn-budget / RTK decision. Unit tests in `scripts/tests/` (`python3 -m unittest discover -s scripts/tests`); operator guide at `docs/operations/turn-economy-measurement.md`. *Authored by a Hydra dogfood run.*
- Visible per-turn turn budget: workers now inject a non-binding "You have used N of ~B turns" reminder into the live prompt tail every `agents.pi.turn_budget_reminder_cadence` turns (default every turn). The soft budget B is configurable per phase via `agents.pi.turn_budgets` and defaults above fleet p75 (red 150, green/refactor 300, others 200) so it paces rather than binds. Config flows from YAML through `HYDRA_PI_TURN_BUDGET` and `HYDRA_PI_TURN_BUDGET_CADENCE` env vars. *Authored by a Hydra dogfood run.*

### Changed
- br (beads-rust) upgrade path 0.2.10 → upstream v0.7.0 verified AND partially rolled out (hydra-7m2): upstream absorbed ~99% of the local patch set (doctor fm-* detectors, validation size-cap removal, RequireTypedReferences, agent_context column, inheritance rendering, upgrade asset naming), so the residual port is only the JSONL store-mode fix (`jsonl_store_mode`: setgid `.beads` → 0o660 else 0o600) re-authored for the restructured v0.7.0 sync engine (`docs/hydra/handoff/beads-rust-v0.7.0-port.patch`, git-apply-verified; supersedes `beads-rust-jsonl-setgid-0660.patch`). Verified: `cargo test --lib` 3233 passed / 0 failed, plus setgid canary (setgid store → 0660, non-setgid control → 0600). Rollout executed: v0.7.0 binary installed at `/opt/dev-tools/config/pi/bin/br`, telematics + webapp stores migrated to schema 19 via `mv db family + br init` recipe (JSONL authoritative, issue counts preserved, create/close through shim verified, zero EACCES/NOT_INITIALIZED in daemon log post-migration, old 0.2.10 coexists safely with v19 stores). Remaining root-side (one shell): daemon PATH reorder or infra release-binary replacement, hydra-main + infra store migrations, fleet rsync of the vendored tree — full runbook with executed results and rollback backups: `docs/operations/br-upgrade-v0.7.0.md`.
- First-class campaign park mechanism replaces the HOLD-gate bead injection used by `repair:park-campaign`. A new durable `project_parks` registry table is the source of truth, the scheduler skips all spawns for a parked project in both spawn paths, and `repair:unpark-campaign` releases the project. This removes the gate-rooted chain identity problem that made the injected approval gate (not the epic) the chain key and confused the reconciler. *Authored by a Hydra dogfood run.*

### Fixed
- Per-repo spawn path resolution + wrong-repo guards (telematics incident, 2026-09-21): auto-discovered multi-repo projects stored a bare relative repo name in bead provenance, so spawns resolved relative paths and git walked up to the daemon's monorepo. `DAGBuilder` now records the absolute per-repo path (`resolveRepoPath`) for every listed bead; `SpawnInternal` refuses empty or relative project paths with a typed `spawn_refused` event; and `EnsureWorktree` verifies `git rev-parse --show-toplevel` matches the resolved project path before `git worktree add`, emitting `worktree_refused` on mismatch. Also hardens `findUserForProject`, `PrefixGuard`, and close-path resolvers against relative repo paths.
- Discovery paths no longer return escalated-labeled beads as ready-to-run: `BeadDiscovery.filterByDAG` now applies the `escalated` label gate after `br ready`, `bv --robot-plan`, and `bv --robot-triage`, matching the existing gate in `dag_navigator.GetReadyBeadsFromDAG`. Prevents the class of incidents where a bead already escalated to a human was respawned from the discovery intake (kgo, 2026-09-11; hydra-wdp). *Authored by a Hydra dogfood run.*
- Non-fast-forward push rejection no longer strands singleton closes (hydra-7k1, 2026-09-16): `publishAfterIntegrate` now recovers via fetch + merge of the remote tip + one retry (`PushBranchWithNonFFRecovery`), non-FF and rule-violation rejections classify for the auto-PR fallback, and verified-close accepts a worktree whose HEAD is a strict ancestor of the main checkout HEAD (commit-level `close-accepted-already-integrated` event) instead of looping `empty-diff` after a locally-landed merge. *Authored by a Hydra dogfood run.*
- Housekeeping: cleaned up nipuna webapp campaign aftermath by deleting the stale `docs/hydra/deviations/flow-runner-rename-20260915-compliance.md`, `docs/hydra/handoff/green.md`, and `docs/hydra/handoff/red.md` handoff artifacts from the webapp repo, and verified that quarantine and `hydra/wt-webapp-*` branches/worktrees are already gone. *Authored by a Hydra dogfood run.*
- Verified PrefixGuard integration (original bead hydra-7ed) after a worker non-zero exit; the auto-namespacing implementation now passes build, vet, and the internal/core test gate. *Authored by a Hydra dogfood run.*
- Fixed multi-user auto-discover repo sentinel: `MultiUserDiscovery.discoverRepos` now recognizes real br projects via `.beads/issues.jsonl` while still accepting legacy `.beads/beads.db`, so PrefixGuard runs on auto-discovered `type: multi` repos. *Authored by a Hydra dogfood run.*
- Versioned worktree-key resolution (webapp-tc5 incident, 2026-09-16): a stale `hydra/wt-<key>` branch forced silent branch versioning to `hydra/wt-<key>-v2`; chain completion re-derived the branch by exact key, logged `SKIPPING integrate ... (nothing to integrate)`, and 1,642 insertions sat stranded until hand recovery. Three guards now close the class: (1) the stale-branch reattach path in `EnsureWorktree` logs loudly that downstream lookups must use the recorded key, and `WorktreeManager.ResolveBranch` reports the branch actually checked out (never re-derived); (2) the spawner records the chain's real worktree key + branch on `ChainInfo` (`RecordChainWorktree`, preserved across re-registration), and chain completion resolves the branch in order — recorded → exact → newest `-vN` (each fallback logged), emitting a typed `chain_integration_unverified` event when truly nothing exists instead of a silent skip; (3) the orphan reconciler normalizes a `-vN` suffix off the branch-derived key before active-key/terminal checks (that exact-match miss was why the versioned worktree survived 1h+ of sweeps) and integrates the real branch via `IntegrateBranchAndRemove`. *Authored by a Hydra dogfood run.*
- Per-repo bead attribution in bv discovery (Repos[0] bug): `findFromBV`/`findFromBVPlan` attributed every bead of a multi project to `Repos[0]`, so `stillCompleted`/`GetBead` queried the wrong repo's store for repos[1..n]. Discovery now invokes bv per repo (matching the per-repo `br` model) and resolves each bead's repo from the item's `repo` field when bv emits one, else from the invocation target; a loud once-per-project log fires when a 2+-repo project has no per-item repo field. Verified against real bv output: current bv (v0.16.4) emits NO repo field on triage/plan items, so per-repo invocation is the correct fix. Also, all three bv paths now skip a bead whose store status is terminal (bv's disk cache can go stale). Covered by unit tests (mock bv, incl. cross-repo ID collision) and a real br/bv scratch-multi-repo integration test. *Authored by a Hydra dogfood run.*
- Duplicate project names + silent chain-integration miss (webapp zpf incident, 2026-09-16): two projects named `webapp` made `findProjectPathForBeadID` return the FIRST name match, the chain's worktree branch did not exist there, and `Integrate`'s "nothing to integrate" nil return read as success — 17/17 beads closed while the work sat unintegrated. Three guards now close the class: (1) config load rejects duplicate project names (`DuplicateProjectNameError` — daemon fails startup instead of silently degrading to defaults; hot reload rejects via `ValidateConfig`); (2) chain registration records the root bead's project path in `ChainInfo.ProjectPath` and chain completion integrates against that recorded path, falling back to logged name resolution only for pre-upgrade chains; (3) the completion callback captures the branch tip before teardown, verifies it is contained in HEAD after integrate (mismatch → typed `chain_integration_unverified` event + `integration-unverified` label on the root bead), and logs every outcome — integrate success, skip with reason, integrate failure, quarantine-empty. *Authored by a Hydra dogfood run.*
- Reconciler deleted a live worker's worktree (webapp-zpf "husk" incident, 2026-09-16): worktree protection depended on in-memory chain registration, so a routine chain-timeout unregister silently stripped it and the hourly sweep removed the sandbox 8 minutes later — the worker flailed in the empty directory for 25 minutes. Three layers now prevent the class: (1) workers report their `worktree_key` on the registry row and Charon's protection set unions every fresh-heartbeat row's key — protection is derivable from durable state, immune to chain timeouts and daemon restarts; (2) the reconciler skips any worktree with writes younger than 30 minutes regardless of what registry/chain/bead state claims; (3) workers run a heartbeat canary — a worktree run whose `.git` vanishes mid-run parks as `workspace-destroyed` and exits instead of burning turns in a husk.
- Close-reopen loop on integration failure: `failIntegration` now counts consecutive integrate/push failures per bead and, at 3, escalates to the durable `integration-exhausted` hard-park label (already respected by the scheduler, Charon, and Pan) instead of a plain reopen that respawned the bead into the same failing integrate forever. Operators previously saw accepted-close→open cycles burning full worker runs (webapp-9ah: 4 cycles in 4 minutes); the counter resets on a successful integrate+push.
- CHANGELOG merge conflicts between parallel chains: every project repo now gets `CHANGELOG.md merge=union` in its repo-local `.git/info/attributes` (uncommitted, idempotent) when a worktree is ensured, so two chains that both append changelog entries no longer conflict on integrate. Worst union outcome is a rare duplicated entry instead of a failed close.
- Worktree on the wrong branch: `EnsureWorktree` now detects a reused worktree sitting on the campaign branch instead of its `hydra/wt-*` branch (the worker would commit straight onto the campaign branch and every close then failed "not something we can merge"). Clean cases are repaired in place (`checkout -B`); unintegrated worker output fails closed for manual inspection instead of being destroyed.

### Added
- Session-file resume pilot (Self-heal recovery A): gated resume of a dead worker's Pi session on orphan respawn. Resume is allowed only after infra-class death (`RetryManager.IsInfra`), only on retry attempts, only when the orphan has been reaped (duplicate-spawn guards), only when the session file is within age/size caps, and only when the worktree HEAD still matches the stored spawn HEAD. The worker switches from `--session-id` to `--session` and injects an `ATTEMPT BOUNDARY` prompt that tells the model to verify prior claims before trusting them. A dark-metrics pilot records fix-bead and verified-close-rejection rates for resumed vs fresh beads and auto-disables resume when resumed outcomes are worse (`self_healing.session_resume` config). *Authored by a Hydra dogfood run.*

- Chain handoff artifacts: a completed TDD-chain phase writes `docs/hydra/handoff/<phase>.md` (fixed template: what changed / verification / open edges); integrate commits it to the integration branch and the next phase's worktree carries it. The next worker embeds the frozen handoff in its prompt tail (new `ChainContext` prompt extra, phase-ordered) so it builds on verified outcomes instead of re-deriving them via billed tool calls. Prompt head stability preserved (pinned by test); `docs/` was chosen because srv gitignores `.hydra/` and docs/ is already scope-gate allowlisted. *Authored by a Hydra dogfood run.*

- `output.pr_merge` (default false): when the working branch has PERMANENT branch protection, the publisher stops attempting direct pushes (which can never succeed) and lands every integrate via PR — force-push HEAD to `dogfood/<bead-id>` (or `dogfood/publish-<project>` for the periodic publisher), `gh pr create`, `gh pr merge --merge --admin`, then verifies MERGED state via `gh pr view` before reporting success (fail-closed: a created-but-unmerged PR surfaces as an error, the close aborts, worktree preserved). Uses a true merge commit, not squash, so local and remote histories stay convergent across repeated integrate cycles. Applies to both the singleton-close path (`publishAfterIntegrate`) and the periodic `PublishRepo` tick. *Authored by a Hydra dogfood run.*
- Bead-store integrity guard + Pan auto-restore: `BeadStoreGuard` (`internal/core/bead_store_guard.go`) tracks per-project open-bead counts across discovery ticks; a collapse to 0 or >80% in one tick (or a `br list` failure after a healthy baseline — the issues.jsonl-deleted shape from the srv-smart-engine 2026-09-11 incident) is treated as store corruption, never a legitimate mid-campaign event. The project is excluded from all discovery paths (fail-closed scheduling), a typed `bead_store_suspect` event fires, and one automatic restore rebuilds `.beads/issues.jsonl` from the newest `.br_history` snapshot — unblocking only if a re-list verifies the open count recovered (events `bead_store_restored` / `bead_store_restore_failed`); failure stays blocked and alerts. Pan gains a whitelisted `repair-bead-store` daemon-side repair that re-runs the verified restore for the bead's project. *Authored by a Hydra dogfood run.*
- Cache hardening pack for prompt-cache reuse across respawns: workers now spawn Pi with `PI_CACHE_RETENTION=long` (pi-ai sends `cache_control {type:ephemeral, ttl:1h}` instead of the default 5m TTL; harmless if the provider ignores the marker), respawn prompts sort Hindsight memories and CASS bullets by content hash with server rank as tiebreak so prompts are byte-identical whenever the underlying set is unchanged (warm cache from turn 0), and `scripts/cache-ttl-probe.sh` measures whether the coding endpoint actually honors the ttl:1h marker (probes `cache_read` at +35min/+65min). Operators: full probe takes ~65min — run under `systemd-run`/`nohup`, not inside a worker turn. *Authored by a Hydra dogfood run.*

- llm-router deployed at /opt/llm-router (systemd `llm-router.service`, 127.0.0.1:9900) fronting the Kimi coding account; workers now spawn pi with provider `kimi-router` (live-ops config, models.json) so all worker LLM traffic gets lane gating, budgets, and prefix-SHA logging at the router. Pan (MiniMax) stays direct. docs/operations/host-setup.md §6b documents the install and the models.json override gotchas (per-model baseUrl shadows provider-level; pi built-ins can't be rerouted by id collision — use a fresh provider id).

- Pan rescue-surface expansion (items from the 2026-09-13 manual-rescue audit): three new whitelisted daemon-side repairs — `clear-hard-park` (prunes stale worktree registrations, removes `worktree-unavailable`/`integration-exhausted` labels, reopens the bead, resets its retry counter; refuses when no hard-park label is present), `prune-worktree-registration` (project-scoped `git worktree prune` — one repair clears a fleet-wide registered-but-missing wedge), and `re-push-integration` (reruns the publisher for a stranded project, fail-closed until origin holds the commits). Project-scoped repairs rate-limit per project (30m) instead of per bead (1h) — a per-bead limit would need 13 hours to clear a 13-bead shared wedge. Pan's candidate set now includes no-worker stuck beads (hard-park-labelled beads even with no retry record, >24h escalations, PUSH-PENDING pseudo-beads), and ≥3 BLOCKED candidates sharing a reason collapse into one PROJECT-WEDGE candidate so Pan issues one project-scoped repair instead of N per-bead ones. The prompt lists every whitelisted repair plus the rescue-recipe catalog (label durability, shared-wedge preference, push-pending guidance, human-only boundary for merges/PRs/deploys/repo edits); a test pins prompt↔whitelist parity.
- Push-pending visibility: `publishProjects` no longer discards `PublishRepo`'s `(pushed, reason)` result — a failed push records a per-project push-pending state surfaced to Pan as a `PUSH-PENDING` candidate (`<project>-push-pending`) with the failure reason, cleared on the first successful/benign publish. The "commits not pushed properly" class (reported by ajantha 2026-09-13; the 2026-08-25 48a/2l9 orphaned-work incidents were the same class) is now observable and repairable instead of silently retrying forever.

- Host log retention hardened: hydra's logrotate config gains `maxage 365` (delete logs older than one year) and journald gains bounds (`SystemMaxUse=500M`, `MaxRetentionSec=365day` in `journald.conf.d/99-size-retention.conf`) — covers llm-router, which logs to journald only. Vacuumed 3.9G → 456M. host-setup.md §6c documents both mechanisms and verification commands.

- Worker prompt cache instrumentation: every spawn logs `prompt_sha256`, `head8k_sha256`, and `prompt_bytes` (log line carries an empty reserved `account=` field for the future multi-account router). Two same-project beads now share a 1,676-byte invariant doctrine head (was 0 — the per-bead type/title line led the prompt), and the invariant/variable block order is pinned by tests so provider-side prefix caching (Kimi caches by prefix hash; `PI_CACHE_RETENTION=long` already set) can pay across beads and retries.

- Pan no longer judges project-level pseudo-beads to death: `<project>-push-pending` candidates are repair-only in the decision loop (abandon/close/worker commands are ignored — the first live PUSH-PENDING, webapp's diverged push 2026-09-14, was abandoned every ~2 minutes with each attempt failing "project path not found"), and an unchanged pseudo-bead reason is re-presented at most once per 30 minutes so a human-only condition does not consume a judgment call every tick. The prompt now spells out the pseudo-bead rule.

- host-setup.md documents the live worker extension allowlist (`rtk`, `caveman`, `context-mode`, `hindsight`, `pi-cache-optimizer`), the npm-vs-git resolution paths, and verify-pi-config.sh's npm-only false failure for git-installed packages. Operator install procedure for a new worker extension: `pi install` into the shared config + one allowlist line.

- Pan close paths (`abandon-with-summary`, `close_satisfied`) and repair handlers no longer fail with "project path not found" for projects registered by multi-user discovery: discovery names projects `<user>/<repo>` while bead IDs carry only the repo name, and both resolvers (`resolveProjectPathByName`, service_starter's `projectPathForID`) were exact-match only — observed as a 100% failure rate on every Pan close/repair attempt against discovered projects (2026-09-11/13). Both resolvers now fall back to repo-name matches inside multi projects and path-basename matches; exact matches still win.
- Pan's repair prompt listed only 3 of 4 whitelisted repairs — `repair-bead-store` sat invisible to the model for weeks, so the 2026-09-13 webapp store corruption (#3) had to be repaired by hand. Prompt now lists the full whitelist (parity pinned by test).

- Janitorial auto-commit hardening: `autoCommitDirtyMain` now excludes `.beads/**` from its staging scope (matching `WorktreeManager.Commit`, so br-mirror rows never ride a wip commit onto the working branch), and both commit paths refuse — skip + `ALARM` log, unstaging — when any staged file contains git conflict markers (`git diff --cached -G '^<<<<<<<|^=======|^>>>>>>>'`). Janitorial commits had baked conflict-marker states into git 3+ times across repos (846bdd1 mid-merge capture, 9f74f940 unparseable package.json), poisoning every downstream gate. *Authored by a Hydra dogfood run.*
- Worktree GC no longer reaps worktrees that merely LOOK stale: staleness was judged solely by the `.git` pointer file's mtime (creation-time only), so a dep-parked bead's worktree being actively edited — a merge-conflict resolution in progress — was `RemoveAll`'d and the resolution lost (hydra-q3p, 2026-09-12). The sweep now also preserves any worktree with file activity after creation and within the staleness window (`worktreeRecentlyActive`: newest file mtime > `.git` mtime + 2-minute checkout margin AND < `maxAge` old; untracked output and in-progress merges qualify, a pristine checkout does not) and emits `worktree-preserved-unintegrated` with `cause: recent_activity`. Existing dirty-branch/uncommitted-tracked preservation is unchanged, and uniformly-old clean-integrated worktrees are still reclaimed. *Authored by a Hydra dogfood run.*
- CloseEvent no longer lies `accepted` when the close never lands: the accepted verdict now fires only after the `CloseBead` write succeeds (it used to fire from the verify gate, so 538 accepts were logged while every `CloseBead` write aborted at integrate/push). Aborts report distinct verdicts — `integration-failed` (integrate/push verification failed) and `close-failed` (store write errored) — keeping the metrics stream consistent with the store. *Authored by a Hydra dogfood run.*
- Shared-checkout fallback removed — worktree setup failure now fails the spawn CLOSED for every project, not just daemon-owned-gates ones. The old fallback ran the worker loose in the user's main checkout; observed 2026-09-11/12: merge-conflict markers committed into `webapp/.beads/issues.jsonl` (br store unreadable), a project's `.beads` dir wiped by a `git clean -fdx` "fix the repo" worker, janitorial commits landing on the user's working branch. `SpawnInternal` now hard-parks the bead with a durable `worktree-unavailable` label (mirrors `escalated`) and returns an error; the retry queue, DAG ready filter, and Charon OrphanRetry all skip labelled beads until a human fixes the worktree and clears the label. *Authored by a Hydra dogfood run.*
- Multi-repo projects no longer get their live workers orphan-killed (2026-09-11 12:38: 10 webapp workers killed ~38s after spawn). Charon's bead scans now run per-repo (`repoScanTargets`: `project.Path` joined with each repo name — never the multi parent dir, which has no `.beads`), the fail-open failed-list set is keyed by that repo path so worker records match, and the new `orphanVerdict` refuses to kill any worker whose registry heartbeat is fresh — an orphan verdict now requires absence from the registry, not br-list failure. Pan daemon-side abandon/close-satisfied resolve repo paths via `resolveProjectPathByName`, which prefers discovered (multi-user) projects over static config, fixing the `project path not found` abandon failures. Auto-discovered multi repos now store absolute paths. *Authored by a Hydra dogfood run.*
- OrphanRetry no longer strands killed-worker beads: liveness now requires a running PID or a fresh registry heartbeat, not merely a `running` tracker row. The 2026-09-11 hang wave left 14 webapp beads `in_progress` behind dead-PID tracker rows that the old check counted as live, so the sweeps never reopened them (scheduler treats `in_progress` as handled). On reopen Charon also reaps the stale row and emits a typed `charon_orphan_reopened` event. Escalated-skip, terminal guard, and grace period unchanged. *Authored by a Hydra dogfood run.*
- Phantom worker slots no longer wedge scheduling until restart (2026-09-11 12:00: `tracked=2 running` / `slots filled 2/2` with 0 live processes and an empty registry — the completion path removed the registry row but never decremented the in-memory active set). Charon's tick now reconciles the in-memory `activeWorkers` set against live PIDs before the no-tracked-workers early return, and reaps real-PID slots whose process is dead unless the tracker shows a live replacement PID or the registry shows a fresh heartbeat (heartbeat outranks `ps` so a live worker is never double-spawned). Previously both in-memory reconcilers ran only after the early return — unreachable in exactly the all-dead state they exist to fix. *Authored by a Hydra dogfood run.*
- Pan abandon-with-summary no longer risks killing healthy work (br-4y5, 2026-09-11: abandon queued citing `worker limit 1/1 already saturated` while br-4y5's own worker held the only slot — its registry row landed between candidate scan and judgment execution). Every abandon decision now logs an audit line with its rationale (decision origin `worker|blocked`, reason, live rows, slot holders as `bead:state(pid=N)`), blocked-origin abandons are refused when the bead now holds a live worker row, and stale pending abandons are never executed daemon-side while a live row exists. Hung-worker abandons (worker-origin) and workerless daemon-side closes are unchanged. *Authored by a Hydra dogfood run.*
- Worker pi exec no longer misattributes a missing worktree to the pi binary: when `cmd.Dir` is removed between spawn and exec (reconciler integrate/quarantine, worktree GC), Go's fork/exec reports the chdir ENOENT against the binary path (`fork/exec /usr/bin/pi: no such file or directory`) — gybo4 (2026-09-10) burned an hour as 148 false 'provider failures' while pi was healthy. The worker now stats the working directory before exec and fails with the true cause (`worktree dir missing: <path>`), classified as infrastructure (exit 4), not a code failure. Charon's worktree sweeps additionally protect live chain worktrees (active beads are mapped to their chain-root key — a member bead's key never matched before) and any worker started within a 10-minute grace window, and the spawner now logs each `EnsureWorktree` outcome (created/reused/failed) per spawn instead of staying silent. *Authored by a Hydra dogfood run.*
- LSP server installs (`internal/pi/lsp.go`) no longer touch the global npm prefix: hydra's own `npm install -g lsp-client`/`typescript-language-server` rewrote `/usr/lib/node_modules` and `/usr/bin` symlinks, and workers spawning `/usr/bin/pi` mid-swap died with `fork/exec /usr/bin/pi: no such file or directory` — every pi-ENOENT burst since 2026-08-11 was self-inflicted (auditd `pi_nodemodules` watch, 2026-09-09). LSP npm installs now use `npm install -g --prefix $HYDRA_HOME/npm`, checks and `ExecuteLSPCommand` resolve binaries from that prefix first, with PATH as fallback. Verified: 60/60 pi spawns across 3 concurrent prefix installs, 0 ENOENT, global prefix byte-identical. *Authored by a Hydra dogfood run.*
- Scheduler's in-memory `completed` set is re-validated against store state before filtering: a bead whose close was later rejected/escalated (left `in_progress`) or that was reopened externally used to linger in the set and be silently filtered every tick (`Filtered out 1 beads: completed=1`) until daemon restart (gybo4, 2026-09-09: spawned instantly after restart). `BeadDiscovery` now prunes non-terminal entries at the `br` filter and via `stillCompleted` (which re-reads the bead, keeping the cached entry only on store-read failure) in the `bv` plan/triage paths; `WorkerLifecycle.HandleCompletion` additionally prunes the entry at the write site whenever a close is rejected/escalated, so a close failure can never leave the bead in the set. *Authored by a Hydra dogfood run.*
- Pan run-analysis fixes: (1) judgment dedup signatures now normalize volatile ages/timestamps/counters out of candidate reasons — the age ticked every minute, defeating dedup so the same escalated set re-judged every 60s (~31 judgments/hour observed); the judgment prompt still sees raw reasons. (2) Repair efficacy: after 3 attempts of the same repair on the same (action, scope, normalized reason), Pan refuses further attempts and classifies human-only — ending the observed pattern of 66 prune-worktree-registration executions on one wedge in a day. A changed reason is the natural reset. *Authored by a Hydra dogfood run.*

- Daemon pushes bypass client-side git hooks: `PushBranch`, `PushBranchWithToken`, and the auto-PR push now run `git push --no-verify`. Hooks like husky's pre-push (`npm run test:cov`, >10min silent) are interactive-dev affordances that fail or hang in headless daemon checkouts — with the main checkout's node_modules gutted they die with `husky: command not found` and silently broke `output.push` for srv-smart-engine (2026-09-09: `error: failed to push some refs`). Server-side hooks still run; the daemon re-runs its own verification gates after push. Locked by `TestPushBranch/failing_client-side_pre-push_hook_is_bypassed` (RED without the flag). *Authored by a Hydra dogfood run.*
- `EnsureWorktree` post-add reset is guarded the same way as the reuse-reset: when a worktree path was cleaned between attempts but its `hydra/wt-*` branch still carried gate-verified worker commits, reattaching the branch was followed by an unconditional `reset --hard <base>` that orphaned the commits on every respawn — the worker redid the work, committed, and the next respawn wiped it again (srv-smart-engine af7v7/udasa/80ro0/q8awv/h8xuu/ulz1m/b5pco/fbbqz/2rccn, 2026-09-08: sessions show 12-spec deliveries with 200/200 green reruns, then empty-diff rejections and wrongful escalations). The reset now fires only when the reattached tip is already contained in the working branch; otherwise the worktree keeps the worker's tip and verified-close's `git diff base...HEAD` union sees the committed work. Third wipe vector closed (after reuse-path and GC guards).
- Worktree GC is fail-closed on in-flight work: `CleanupStaleWorktrees` (Charon's hourly sweep) used to `RemoveAll` any stale worktree directory unconditionally, wiping deliverables between verdict and verified-close — srv-smart-engine-48bdq completed its slice twice (commits a17b1ebd1/eb25e8ba0, 52/52 green) and each wipe destroyed the working tree, so verified-close saw an empty diff and rejected, three wipes = three wrongful escalations until manual merge (2026-09-06/07). The GC now checks the worktree's `hydra/wt-*` branch for commits not contained in the working branch (`git rev-list --count HEAD..<branch>`) and the worktree for uncommitted tracked changes (untracked `node_modules` etc. deliberately ignored) before removing; a definitive positive preserves the worktree and emits a typed `worktree-preserved-unintegrated` event (`worktree`, `branch`, `cause`) — the reconciler lands the branch on its next sweep. The check fails open only when the source repo/git queries are unavailable so a broken check never wedges the sweep. Charon's `EventSink` is now wired at overseer startup (previously unset, so its emission path was a silent no-op). *Authored by a Hydra dogfood run.*
- Integrate/push is fail-closed: `Integrate` verifies the merge actually landed (`git merge-base --is-ancestor <wt-branch> HEAD`) before "integrated singleton" is logged, `PushBranch`/`PushBranchWithToken` verify the remote ref advanced (`ls-remote`) before "pushed" is logged and now return the exact remote stderr on rejection (GH006 branch-protection text surfaces verbatim instead of a collapsed "protected branch"), and the singleton worktree is removed only after both land. Either failure emits a typed `integration-failed` event (`stage`, exact `error`), reopens the bead, and preserves the worktree — previously a rejected push was swallowed, the bead closed `completed`, and gate-verified commits dangled unreferenced (hydra-xnx f6269c0/d61ae5d, 2026-09-06; 951bf82 09-05). Root-cause guard underneath: `EnsureWorktree` reuse-reset only fires when the worktree tip is already contained in HEAD — the unguarded reset rewound branches holding committed worker work, after which the integrate silently no-opped ("Already up to date"), the no-op push verified equal, and the loss looked like success. Rejected pushes route to `maybeOpenAutoPR` when `output.auto_pr` is on; a created PR preserves work via the dogfood branch and the close proceeds, otherwise the close fails loudly. *Authored by a Hydra dogfood run.*
- Dead session plumbing removed: `internal/agents` PiConnector built a `hydra-<bead>` session ID but passed `--no-session`, so every RPC run was ephemeral and unresumable — it now launches `pi --session-id hydra-<bead>`; gap adjudication likewise moves from `--no-session` to a per-bead-purpose `--session-id hydra-adjud-<bead>` (never shared across beads, so prior adjudications cannot bias later verdicts). *Authored by a Hydra dogfood run.*

### Documentation
- Correct the multi-user discovery sentinel documented in `docs/operations/host-setup.md`: repos are recognized by `.beads/issues.jsonl` (legacy `.beads/beads.db` remains a fallback marker), matching `MultiUserDiscovery.discoverRepos`.
- Document multi-user project discovery in `docs/operations/host-setup.md` and `docs/guides/admin-guide.md`: per-user `~/.hydra/config.yaml`, `type: multi` + `auto_discover: true`, and the PrefixGuard `<username>-<reponame>` `issue_prefix` convention. Operator removal of `webapp` from `projects.auto_discover_exclude` in `/etc/hydra/hydra.yaml` is out-of-scope for this change and remains blocked until hydra-7ed and hydra-n4g are verified live.
- Lifetime-fit sizing rule: create-prd gains §3.2b (≤12 source files per FR for read-and-modify-each-file work, slice+flip pattern, exotic-environment separation, slice-scoped acceptance) and task-breakdown gains a matching hard rule plus a verbatim guardrail block for every bead spec (no generator scripts, no node_modules rebuilds, no config edits, slice-scoped test runs only). Driven by measured failures: whole-module beads poj8g/e2lqf demanded 539/811 turns and 66M/94M tokens and died incomplete; ≤12-file beads complete first attempt.
- `AGENTS.md` Documentation & Changelog Discipline gains rule 4 (host-setup sync): any change altering host requirements (systemd unit/drop-ins, paths, binaries, OS packages, Pi config, `/etc/hydra/*`, resilience tooling, deploy procedure) must update `docs/operations/host-setup.md` in the same commit/PR; the doc itself carries a matching banner pointing at the rule.
- `docs/operations/host-setup.md`: complete bare-host → running-fleet provisioning guide (users/groups, layout, pi install, shared config, systemd unit + drop-ins, live config, swap + auditd resilience, deploy procedure, smoke test, day-2 recall commands). Every step carries a verification; each warning cites the incident that produced it.

- Pan's abandon-with-summary (and any synthetic-worker close) no longer dies with `project hydra not found`: `BeadCloser.Close` falls back to the config resolver (`resolveProjectPathByName`, the #225 family) when the registry-backed resolver returns empty. Live catch 2026-09-15: Pan's judgment was correct on the stale escalated hydra beads but the close itself could never execute — the third resolver site, now consistent with the other two.

- Worker no longer retries a cleanly-completed rpc turn: pi cannot exit on its own after `agent_end`, so `finish()` always kills it by SIGTERM, and `executeWithPi` returned that by-design kill as a non-nil error. The caller's retry loop treated a perfect run as a failure and the daemon re-prompted the bead forever — hydra-xnx was re-prompted 4x after `done:true` verdicts on 2026-09-06 (2,113 "Execution failed on attempt" lines fleet-wide are this shape). The rpc success branch now suppresses the intentional-kill error; `TestExecuteWithPi_RPCSuccessReturnsNilError` locks it (RED without the fix). Deploy: rebuilt `$HYDRA_HOME/bin/worker` + `hydra`; restart the daemon after workers drain to activate. *Authored by a Hydra dogfood run.*
- Escalated beads are no longer reopened: a verified-close rejection at max attempts now applies the durable `escalated` label (alongside `needs-human`/`verified-close-escalated`) and emits a typed `bead.escalated` event, and every respawn-deciding consumer skips labeled beads — Charon orphan recovery (`[OrphanRetry] bead %s carries the "escalated" label ... skipping reopen`), the scheduler ready filter, and the retry-eligible path. Previously the refusal left the bead bare `in_progress`, which is exactly the signal Charon's orphan recovery keys on: c01hq/65xy7/48bdq respawned into the same empty-diff death for hours (65xy7 burned 4 attempts in 3m28s). *Authored by a Hydra dogfood run.*
- Cache-aware token accounting splits provider usage into fresh input, cache read, cache creation, and output; `tokens_so_far` remains the provider-facing total for observability while `tokens_fresh` (fresh input + output) now drives Pan arbitration, Charon's soft cap, and the hard backstop. This stops cache-heavy sessions (e.g. 121M cache reads) from tripping the 30M backstop. `cmd/worker/pi_rpc_turn.go` parses per-message `message.usage`, carries counters across nudges/resumes, and reports both totals in the completion JSON. `internal/registry/registry.go` persists the breakdown; `internal/pan/pan.go` and `internal/core/charon.go` arbitrate on `TokensFresh`.
- Park tests (`cmd/worker/park_test.go`, `cmd/worker/park_commands_test.go`) now use mutex-protected buffers and closure flags, eliminating data races under `-race` that surfaced when diagnosing the previous gate failure.
 the spawner now re-reads the bead's store status before forking, drops terminal beads from the retry queue, emits a `spawn-suppressed-closed` event, and refuses to mark a terminal bead as `in_progress` in `SpawnInternal` as defense in depth. Prevents the e2lqf/poj8g resurrection vector that burned 123.7M/69.7M tokens after the beads had already closed.
- Pan close-satisfied no longer closes beads whose only evidence is a green test suite when the bead description names a measurable acceptance command or a coverageThreshold/ratchet flip. It now runs the full acceptance command and requires exit 0; if only a test suite is described it abstains (continue, not close); if a ratchet/flip is named but the durable config lacks the entry, the close is forbidden. Fixes the srv-smart-engine-1817l false `closed-satisfied` at 73.28% functions coverage vs the 80% acceptance threshold.
- Close-path guard now honors the zero-token infrastructure invariant: `Wait`'s infra-failure guard consulted only the exit code, so a pi-spawn failure (`fork/exec /usr/bin/pi: no such file or directory`, seen in bursts since 2026-08-11; killed srv-smart-engine-52q9u/u93jp on 2026-09-01) closed beads failed at 0/3 retries with fix beads minted — despite #194 already recording such deaths as infra in the fault accounting. The guard now asks `isInfrastructureExitCode(exitCode) || zeroTokenFailure(worker)`, the same question as the accounting layer, so a worker that never started leaves the bead open for retry instead of closing it failed.
- Zero-token worker deaths are now classified as infrastructure failures and never consume a bead's 3-attempt fault budget: a worker that produced no tokens never attempted the task, so its death cannot be a code fault. This is defense in depth at the accounting layer — a provider-capacity signal arriving in a transport shape worker-side classification has not seen yet (the RPC `turn_end` 403 shape burned the fault budgets of hcj3h/nl7r3/iz5z7/ehzq2 on 2026-08-30/31 before hydra-zzb landed) can no longer kill a bead. `internal/core/worker_lifecycle.go` adds a `tokensLookup` (wired to the registry's `tokens_so_far` in `internal/core/overseer.go`); nil lookup or registry miss preserves the old behavior.
- Daemon startup now reconciles dead-PID worker registry rows left by restart. Orphan `working`/`parked` rows are marked `completed` with `orphan-reaped`, their pending Pan commands are dropped, and the bead can respawn. This unblocks the srv-smart-engine-d73cg/9n2sb class incident where orphaned rows persisted after the 2026-08-31 06:30 restart and blocked respawn for ~23 minutes. `internal/registry/registry.go` adds `ReconcileDeadPIDs`; `internal/core/service_starter.go` calls it before Pan starts.
- Park now aborts the in-flight pi turn before sleeping so the provider-side HTTP stream closes. A dangling stream counted against the account's concurrency limit and poisoned the lane for the next wake, causing workers to 403 each other even when the lane was free (srv-smart-engine 9n2sb/d73cg, 2026-08-31 06:01-06:26). `cmd/worker/park.go` sends the `{"type":"abort"}` RPC command; `cmd/worker/pi_rpc_turn.go` ignores the resulting `agent_end`/`error` cleanup so the wake reprompt uses a clean lane.
- RPC `turn_end`/`message_end` provider-capacity 403s now park the live pi session instead of exiting 1. Kimi's `concurrent request limit` is reported inside `message.errorMessage` with `stopReason=="error"` (srv-smart-engine-4u9v8 burned a retry fault; srv-smart-engine-d73cg parked correctly). `cmd/worker/pi_rpc_turn.go` extracts `turn_end`/`message_end` so `decideParkClass` can route to the existing park-and-wake path.
- Pan workerless abandon deadlock: terminal `abandon-with-summary` verdicts for beads with no live worker row are now executed daemon-side through the normal close-as-failed path instead of being enqueued as a worker command. This breaks the stable pending-abandon loop that kept srv-smart-engine-iz5v7/ibwnt blocked for 2 hours. Stale pending abandons older than 2× the Pan interval also trigger daemon-side execution. `OnAbandonWithSummary` wired in `internal/core/service_starter.go`; tests in `internal/pan` and `internal/core`.
- Pan no longer sees scheduler-blocked candidacy for beads with a live worker row: `Candidates()` now marks every live worker as suppressing (previously only workers that were themselves candidates), and a successful `Spawn` clears the bead's stale spawn-failure record. Root cause of the 2026-08-30 incident where Pan abandoned healthy 2-minute-old workers (srv-smart-engine-v4bpl et al.) over 1.5h-stale spawn-failure records, eating the srv coverage queue.

- Pan judgment dedup: an unchanged candidate set (IDs + states + reasons hashed) is not re-judged for 15 minutes, with a valve that re-judges anyway so permanently stuck beads never go blind. Measured motivation: ~65 MiniMax judgments/hour re-judging identical candidates, 19M fresh input tokens overnight (~7x all bead work). Also: provider-capacity parks now probe llm-router `/capacity` before waking and hold (with growing backoff) while the pool is saturated, killing the wake-denied-repark loop; probe fails open. `LLM_ROUTER_CAPACITY_URL` wired into worker env. *Authored by a Hydra dogfood run.*

- Exception-share gate (`gates.coverage_truth`): close now runs an unfiltered coverage command and compares the true total against the enforced threshold. Gap larger than `max_gap_points` blocks close with a message naming both numbers. Closes the srv-smart-engine M5 loophole where exception patterns hid ~60 statement points (enforced 80%, true total 38.74%). `internal/core/gate_runner.go`, `internal/core/gate_close.go`, `hydra-config.yaml.example`.
- `projects.auto_discover_exclude` config list: multi-user discovery now skips excluded project directory names or paths before running `bv` triage, silencing log spam from uninitialized directories like `demo`.
- Verify-loop budget rule + gate-command telemetry (token economics lever 1): daemon-owned projects now pass their declared gate commands to workers via `HYDRA_GATE_CMDS`; the worker prompt adds a `VERIFY LOOP BUDGET` rule and session telemetry counts gate-command invocations, surfacing them in `[SessionStats]` as `gate_runs={...}`. Fail-open: missing or malformed env data is debug-logged and never blocks completion.
- Per-bead tool-mix telemetry: workers now parse the Pi session jsonl after exit and emit a single `[SessionStats]` line with turn/tool/payload counts plus `graphify`, `hindsight_search`, and `jest_runs` breakdowns. Missing session directories are silently skipped; parse errors are debug-logged and never gate bead completion. *Authored by a Hydra dogfood run.*

- Production-integrity pair from the nipuna campaign incident: (1) the worker DELIVERABLE CONTRACT now covers test/red bead types — RED beads whose purpose is writing failing specs can no longer complete as plan-only empty diffs; (2) verified-close in main-checkout mode (no worktree) now requires COMMITTED changes since the branch diverged from origin as diff evidence — uncommitted edits are revertible and were being reverted after close acceptance (14 false closes, 2026-09-16). Guard fails open when no upstream exists. *Authored by a Hydra dogfood run.*

- Retry cap unenforced: 386/3 attempts + nested error wrap wedged beads until restart. `getRetryEligibleBeads` now drops DAG-ready beads whose fault count has reached `default_retries`, `RecordRetryWithCap` clamps the counter and unwraps nested `RetryExhaustedError` to the root cause, and a single `retry_exhausted` event is emitted when the bead has no live worker or registry row. `TestScheduler_ExhaustedBeadRemovedFromReadyAndEventEmittedOnce`, `TestRetryManager_CountDoesNotExceedMaxRetries`, `TestRetryManager_RecordRetryUnwrapsExhaustedError`.
- Close-path state purge: verified closes now clear the bead's retry-manager records (fault, infra, last failure, validation retries) and spawn-failure residue, so closed beads cannot re-enter Pan candidacy or respawn decisions via stale state (hydra-fis class, 2026-08-27).
- Pan belt-and-braces terminal filter: `Candidates()` re-applies `TerminalBead` to the full candidate list before judgment, catching retry/spawn residue that leaks through per-source filters due to a stale cache or unresolved project path.
- Push verification catches "silent" branch-protection rejections that reported success but left origin/development unchanged (hydra-2l9 and hydra-48a orphaned-work incidents). `PushBranch` and `PushBranchWithToken` now run `git ls-remote` after a successful exit and treat a remote/local SHA mismatch as a failure, logging `[Output] push verification FAILED for <bead>: remote at <sha>, local <sha>`. The failure falls back to the auto-PR path when `output.auto_pr` is enabled.
- Orphan-retry path no longer reopens terminal beads: Charon now re-reads the canonical bead via `GetBead` before reopening a stale `in_progress` listing, skips terminal beads with `[OrphanRetry] bead %s already terminal (%s); skipping reopen`, and reaps the stale worker row. This stops accepted beads like hydra-48a from being re-run and re-accepted (≈6M tokens wasted). `TestRecoverOrphanInProgressBeads_SkipsTerminalBeadAndReapsRow`.
- Resurrection loop: a second close attempt after a successful close+integration saw an empty diff and `rejectClose` REOPENED the already-closed bead (i1g, 5r0, bz5, aga accepted-then-reopened 2026-08-24/25). `rejectClose` is now a no-op on terminal beads and `verifyBeforeClose` skips them at entry. `TestRejectClose_TerminalBeadIsNoOp` / `TestVerifyBeforeClose_TerminalBeadSkips`.
- Pan terminal-bead leak into blocked candidates and repair-action invention spam. `Candidates()` now applies `TerminalBead` to scheduler-level blocked beads before they become candidates, and `BuildPrompt()` lists the exact repair whitelist (`repair-worktree`, `reap-placeholder`, `reset-retries`) so the model cannot invent names. Unknown `repair:<name>` decisions are rejected with a `[Pan]` log and remembered per bead so they are not re-proposed the next tick, stopping the MiniMax rate-limiter burn (hydra-dcy class, 2026-08-25).
- Close verifier no longer rejects an already-integrated chain when the merge base has moved past the work: `verifyBeforeClose` now checks whether the worktree branch is a strict ancestor of main HEAD after a successful integrate-into-chain, subsuming the per-target merge-base fallback. This stops landed work from being re-verified and escalated (dy0 + ig9hw, ~33M tokens, 2026-08-27). `TestVerifyBeforeClose_IntegratedChainBranchAncestorPasses` / `PartialIntegrationLFAFallback`.
- Worktree-identity guard false-positived on submodule gitdirs: `worktreeSourceRepo` parsed the `.git` pointer for `/.git/worktrees/`, which matches the SUPER-project's `.git` first when the repo is a submodule (hydra lives at `/opt/dev-tools/.git/modules/components/hydra`) — every hydra spawn failed "valid git worktree of a different repository" (hydra-x9e: 67 retries, fleet wedge 2026-08-24). `isOurWorktree` now compares `git rev-parse --git-common-dir` on both sides. `TestIsOurWorktree_SubmoduleGitdir` / `TestIsOurWorktree_ForeignRepo`.

- Multi-user same-repo support: the daemon now tracks bead provenance (bead ID → the store path that listed it, rebuilt every DAG build) and consults it before any name-prefix resolution in spawn gates, Pan close/abandon paths, satisfied-close, and BeadCloser. Two users can run `webapp` (or any repo) from their own checkouts without renames or cross-user knowledge — resolution follows the store, not the ID prefix. *Authored by a Hydra dogfood run.*

- New `hydra status` subcommand prints a compact one-command fleet rollup: workers (last 8 registry rows), recent verdicts (last 6 `[CloseEvent]` / `[PanRepair]` lines from the daemon log), per-project board counts plus top 3 ready beads, and queue-health flags (pool utilisation, parked-stale-heartbeat, starved open P1s). Configurable via `status.log_path` (default `/var/log/hydra/hydra-error.log`); missing registry or log degrades to a note instead of failing.
- Auto-PR after protected-branch push rejection: new `output.auto_pr` config (default false). When enabled and a bead integration push is skipped with `protected branch`, Hydra pushes HEAD to `dogfood/<bead-id>`, opens a PR against the protected branch via `gh pr create`, and labels the bead `pr-open` with the PR URL in its description. Dedup guards check the label and `git ls-remote`; all failures are fail-open and logged `[AutoPR] failed for <bead>`. *Authored by a Hydra dogfood run.*
- Charon pool-deadlock detector: when active workers are at max and zero tracked PIDs are alive for two consecutive scans, Charon reaps stale phantom placeholder slots older than 10 minutes, logs `[Charon] pool deadlock: reaping N phantom slots`, and emits a `charon_pool_deadlock` event. Live processes are never touched and the daemon is never restarted (belt-and-braces follow-up to the 2026-08-24 placeholder leak). `TestCharon_PoolDeadlock_*`.
- Pan repair verdict: whitelisted daemon-executed repairs for blocked beads (`repair-worktree`, `reap-placeholder`, `reset-retries`). Pan parses `repair:<name>` decisions, refuses them on live worker rows, enforces one repair per bead per hour, and emits a `[PanRepair]` JSON audit event. Handlers are wired in `internal/core/service_starter.go` and tested in `internal/pan` and `internal/core`. *Authored by a Hydra dogfood run.*
- Pan shepherd visibility into blocked beads: `internal/pan.Candidates()` now unions scheduler-level blocked beads (retry-exhausted and spawn-failed beads with no live worker row) supplied by `BlockedBeads`. The retry manager exposes `LastFailure()` and the worker spawner keeps a per-bead `LastSpawnFailure()` map, both wired in `internal/core/service_starter.go`. Blocked candidates carry a `BLOCKED:` reason prefix and are never duplicated when a live worker row exists. *Authored by a Hydra dogfood run.*
- Deterministic quarantine drain: new `hydra drain-quarantine [--apply]` subcommand and `worktree.quarantine_auto_drain` config flag clean up stale `hydra/quarantine/*` branches without LLM judgment. Deletes branches that are fully merged, touch only identical content, or add only superseded `*.spec.ts` / `*_test.go` files; keeps branches younger than 7 days.

- Second close-integrity wave from the nipuna incident: (1) **epic beads are no longer spawned** — they are containers that auto-close via CloseReadyEpics; the flow-runner epic burned 12.8M tokens producing nothing before closing on the campaign's pre-existing commits; (2) **session-scoped committed evidence** — the scheduler records the branch HEAD at spawn and verified-close in main-checkout mode only counts commits AFTER that point, so a no-work session can no longer pass the diff gate on setup commits (with no declared targets, any commit satisfied it). Empty-worktree ancestry false-positive pinned by regression test. *Authored by a Hydra dogfood run.*

- Pan no longer judges beads that have already reached a terminal state: `Shepherd.Candidates()` accepts an optional `TerminalBead` injection and silently skips workers whose bead is closed/completed, preventing repeated MiniMax calls and log spam for dead beads (e.g., 1xs7z's 118M-token parked row). Wired in `internal/core/service_starter.go` with a 5-minute cache and fail-open on lookup errors.
- `EnsureWorktree` is now idempotent over stale chain worktree paths: a broken symlink, empty directory, or junk path is pruned and recreated; a valid worktree of the same repo is reused and reset to the current base; a stale `hydra/wt-*` branch is reattached and reset. Foreign-repo worktrees fail closed instead of being deleted. Prevents the tre2w "already exists" retry-exhaustion death spiral.
- Worker main-checkout write leak guard: `cmd/worker/main.go` now tells the agent its cwd is the git worktree and forbids writing outside it; `internal/core/bead_closer.go` unions main-checkout changes into the verified-close diff/scope evidence when a worktree is in use, so a worker that leaked writes into the main checkout still passes the diff gate. Each leaked file is logged as `[VerifiedClose] <bead> main-checkout leak: <file>` (incident 2jjz8, 2026-08-24).
- Spawn placeholder leak no longer deadlocks the worker pool: `Spawn` now removes its `{status:"spawning"}` placeholder on any post-registration failure, and Charon's orphan sweep reaps only placeholders older than 10 minutes with no live process, preserving the PR #163 double-spawn guard.
- Directory-shaped bead targets (`Files: cmd/worker/`) could never satisfy the close-path diff gate: `buildTargetSets` treats every target as a file and `hasTargetChanges` exact-matched only, so hydra-2l9's fully-committed worktree was empty-diff rejected 3× (7.5M tokens at risk). `hasTargetChanges` now also path-prefix matches with a `/` boundary. `TestHasTargetChanges_DirectoryTarget`.
- Wrong-repo worktree on unresolved project: scheduler-path beads carry an empty `Project` field; `FindProjectPath` matched nothing, the spawner fell back to the empty field, and `git worktree add` ran in the daemon's cwd — 1xs7z's "srv" chain worktree was a dev-tools checkout (51.6M tokens wandering the wrong repo, 2026-08-23). `FindProjectPath` now derives project from the bead ID prefix; the spawner never substitutes the name-as-path; `EnsureWorktree` refuses empty paths outright.



- Approval-gate/epic/approval beads can no longer spawn from the RETRY queue: that loop bypassed `ShouldSpawnWorker`'s type filter, so parking-gate beads were executed by workers and "completed" — satisfying every dep parked behind them and releasing whole campaigns at once (webapp-c36 executed 2026-09-16; nipuna's campaign ran 5 chain beads in parallel, work discarded by the still-open integrate bug). The three container types are now skipped in the retry loop too. *Authored by a Hydra dogfood run.*

- Charon kills now consume retry budget: killed processes classify as infra exits on the worker-death path (forced-kill = free retry), so budget/stuck kills respawned forever — 1xs7z burned 51.6M tokens across 4+ kill/respawn cycles on 2026-08-23. `recordKillRetry` counts each kill; the spawn gate (`RetryExhaustedError`) now stops the cycle at `default_retries`.



- Dynamic multi-user re-discovery: the scheduler tick re-runs multi-user discovery every 5 minutes, so edited `~/.hydra/config.yaml` files (e.g. pointing a multi project at `~/projects/source-2-pay`) and repos added under a multi dir onboard without a daemon restart. New (name,path) projects append; existing multis pick up new repos in place; removal is conservative (vanished entries list empty rather than risking active work). Also: empty multi projects (zero discovered repos) are now skipped by the bv planner and Charon scans instead of failing non-zero every tick — closes hydra-code-rkps. *Authored by a Hydra dogfood run.*

- Worker prompt now injects deterministic discovery directives as protocol: every bead is told to run `hindsight_search` once at start, and beads without a `Files:` line (discovery-class beads) are required to run `graphify query` before exploring files, with a fallback to `rg`. Session forensics showed workers skipped graphify entirely and rarely used hindsight_search because both were discretionary skills.

- Pan fleet health digest — Pan's eyes: every tick the daemon collects a cheap per-project glance (open/in-progress/dep-blocked counts, live workers, dep-chained live-worker pairs, stranded worktrees, unintegrated wt-branches) and wakes Pan only when a project carries an anomaly flag, via `<project>-campaign-anomaly` pseudo-candidates with a compact fleet snapshot as evidence. Fixes the 2026-09-13→16 blindness where three days of campaign anomalies (gate execution, close loops, stranded worktrees) produced zero candidates because every bead-level provider class is failure-residue only. *Authored by a Hydra dogfood run.*
- New Pan verbs: `repair:park-campaign` freezes a project's campaign behind a reusable approval-gate hold bead (idempotent, reversible, project-scoped rate limit) — the c36 containment, automated; `repair:log-improvement` appends Pan's hydra-improvement proposals to `$HYDRA_HOME/pan-improvements.log` for human review (proposals, never self-modification).

- Pan can now autonomously close already-satisfied beads: a new `close_satisfied` verdict runs daemon-owned scoped gates on the main checkout and, if green, closes the bead as `already-satisfied (Pan)` with label `closed-satisfied`. This breaks the empty-diff reject loop where a worker correctly finds nothing to do but verified-close refuses an empty diff, burning tens of millions of tokens until a human intervenes (1xs7z, round 3).

### Added
- Worker prompt now states an explicit turn budget (~40 turns) with an over-budget exit: commit progress, emit `done:false` with findings. The turn-economy block implied infinite runway; 200-turn grinds cost 20M tokens (kgkc7/soe53). Also mandates piping test output through `tail` — full jest dumps were the per-turn context flood.



### Changed
- Token budget is now a signal, not an axe: crossing `overseer.max_bead_tokens` enqueues a "converge now" steer (rate-limited 1/hr, labelled `token-budget-soft`) and surfaces the worker to Pan as a judgment candidate — it no longer kills. A hard backstop at 2× kills true runaways, stayed by a fresh Pan `continue` verdict. The kill-at-cap design would have killed ux5 (9M) and acx (11.7M) — beads that landed — and did kill cc0d6 minutes after Pan judged it healthy.



### Added
- Pre-spawn probe now primes the worker prompt on RED: the probe already knows why the scoped gate fails, so its parsed summary + raw log path are written into the bead description (marker-delimited, replaced on re-probe) before spawning. Workers were burning 3-5 discovery turns at ~100K context each re-deriving the failure. Green path unchanged (0-token close); faults still fail open.



### Fixed
- Integration no longer quarantines on `.beads/`-only merge conflicts: the beads mirror is br-owned on main, worktree-side writes are redundant, so conflicts confined to `.beads/` auto-resolve `--ours` and the merge proceeds (65wa0's gate-proven suite fix quarantined on a mirror row, 2026-08-21). Real-code conflicts still abort + quarantine.



### Added
- Per-bead token budget (`overseer.max_bead_tokens`, 0 = disabled): Charon kills a *working* worker whose cumulative tokens exceed the ceiling and marks the bead failed so the retry cap escalates, labelled `token-budget-exceeded`. Non-converging grinders were burning 18-25M before the wall-clock timeout noticed (kgkc7 20.5M, soe53 18.2M). Parked workers are never budget-killed — frozen workers burn nothing.



### Fixed
- Phantom validation churn killed at the source: "No Implementation Files" fired purely from missing `target_files` metadata; on a fully-closed chain verified-close already proved the diffs exist. Metadata-only gap is now advisory (Warning, no fix bead) — kgkc7 burned 20.5M tokens wandering the repo on this phantom. Open leaves keep it Critical.



### Fixed
- PRD validation no longer loses `target_files` metadata: `internal/beads/client.go` now enriches beads from `.beads/issues.jsonl` `metadata`/`bead_metadata`, and leaf beads under validation chains have backfilled `target_files` (hydra-acx, hydra-f3s, hydra-l1n).
- Gate-failure labels never applied: `UpdateBeadLabels` passed repeated `-l` flags in one `br label add`, but br's `-l` is singular (exit 2) — every `gate-failed`/`gate-failed-<gate>` label silently dropped since slice 2. Now one `br label add` per label.



### Fixed
- Config hot-reload now applies `projects.list` changes: `ConfigManager.UpdateConfig` diffs old vs new projects, updates `beadDiscovery`, `beadAnalyzer`, `projectResolver`, and `beadFileReader`, and registers chains/indexing for added projects without disturbing in-flight workers. Previously adding a project to `hydra.yaml` required a daemon restart before its beads were discovered (round 2; hydra-ux5).
- `WorkerEnvBuilder.Build` no longer inherits `HYDRA_DAEMON_GATES` from the daemon's environment; the flag is now set only from the project's `daemon_owned` gate config, preventing leaked state into workers and a flaky unit test under `HYDRA_DAEMON_GATES=1`.
- `internal/pi` `EnsureDirectory` and `EnsureFile` now apply their requested permissions with `os.Chmod` so tests pass under a restrictive daemon umask.
- Empty-diff reject loop on committed work: the verified-close diff gate only inspected `git status` (uncommitted). A worker that commits its own fix in the chain worktree leaves a clean tree → rejected as empty-diff → next worker sees the work done, no-ops, rejected again (srv 65wa0: 24.9M tokens, 2026-08-21). The gate now unions uncommitted changes with `git diff <base>...HEAD` (base = the checkout branch the worktree diverged from).



### Fixed
- Double-spawn race: `Spawn` inserts a `{status: spawning}` placeholder before the process reaches the worker tracker; a concurrent spawn for the same bead mistook the placeholder for an untracked orphan, deleted it, and proceeded (j81fp spawned twice in one second, 2026-08-21). A "spawning" placeholder is now treated as proof of life — rejected as duplicate, never deleted.



### Fixed
- Pre-spawn probe never fired: scheduler-path beads carry an empty `Project` field (the project lives in the bead ID prefix — the codebase pattern is `BeadID.Project()`); the probe's config lookup missed and silently skipped. Zero `Prespawn` log lines while 65wa0 burned 20.7M tokens across empty-diff rounds. Probe now derives project via `beadProject()` (explicit field, then ID prefix).



### Fixed
- Worktree commits now exclude `.beads/` — daemon machinery state (status flips, DB mirror) must never merge back into main. Root cause of the srv soe53 breakage: `.gitattributes` union-merge on id-keyed `issues.jsonl` kept both sides' rows → duplicate ids → br refused to read the DB. Union is right for append-only prose (CHANGELOG), wrong for id-keyed JSONL; the srv repo attribute was reverted and the file repaired (last-wins dedup).



### Fixed
- Daemon-gate bypass holes found by the first dogfood run (hydra-5cl): (1) workers no longer self-close beads when `HYDRA_DAEMON_GATES=1` — closure belongs to the daemon after gates pass (worker self-close had skipped the entire gate loop); (2) worktree setup failure under `daemon_owned` now fails the spawn instead of falling back to the shared checkout (hydra-5cl edited the daemon's own live repo). Root cause of the worktree failure itself: hydra is a submodule — `/opt/dev-tools/.git/modules/components/hydra` was root-owned; chowned to hydra.



### Added
- Pre-spawn gate probe: for `daemon_owned` projects, scoped gates (cmd contains `<scope>`) run in the project checkout BEFORE a worker spawns; already-green beads close at zero token cost with full DAG completion semantics (srv 65wa0 burned 7.9M tokens over 3 empty-diff reject rounds because its suite was already green). Probe only considers scoped gates, requires bead target files, and fails open — probe faults spawn normally. Emits `prespawn_closed` event.



### Fixed
- `TestMultiUser_EndToEnd` no longer relies on a fixed 200 ms sleep to assert worker spawns; it polls active workers every 25 ms with a 10 s deadline, so the test passes reliably under load when the live fleet shares the hydra user's GOCACHE/CPU. Removes the gate skip for this test.
- `IntegrateAndRemove` auto-commits a dirty main checkout (`wip: hydra auto-commit ...`) before merging — uncommitted changes the merge touches used to fail every integration ("Your local changes would be overwritten"), so gate-verified work piled up in quarantine while beads closed as completed (srv-smart-engine 2026-08-19: 9 legacy-era files blocked all integrations). Same-hunk content conflicts still quarantine as designed.
- srv-smart-engine repo hygiene (project-side, not hydra code): `.gitattributes` union-merge for append-only files (CHANGELOG.md, docs/untestable-code-log.md) — every bead appends per PRD §3; adjacent-hunk conflicts made every worktree integration collide. Union merge keeps both sides' lines.



### Added
- Gate `<scope>` templating: `GateRequest.Scope` substitutes into gate commands (single-quote escaped); empty scope drops the placeholder token so the full command runs. The close path derives scope from the bead's target files (`Files:` metadata/description), so a bead fixing suite X is gated on suite X — not on every other red suite in the repo. Enables `daemon_owned` on partially-red projects without reject storms.



### Added
- Daemon gates wired into the close path (FR-002 slice 2): when a project sets `gates.daemon_owned: true`, a successful worker completion runs the declared gates daemon-side in the chain worktree BEFORE commit/integrate/close. Gate failure reopens the bead via the existing rejectClose machinery (bounded by `validation.max_rounds`, then human escalation), labels it `gate-failed`, and rewrites the description with the parsed summary + raw log path so the retry worker starts from evidence instead of re-running the suite. No worktree, missing runner, or unknown parser all fail closed. Daemon startup recovers `running` gate artifacts from a crashed predecessor. Workers on daemon-owned projects get `HYDRA_DAEMON_GATES=1` and a VERIFICATION OWNERSHIP prompt note. `BeadsClient` gains `UpdateBeadDescription` (br update --description); all mocks updated. Default off — legacy path untouched.



### Documentation
- PRD-A FR-002 expanded into the full daemon gate-runner design: Class A/B/C separation (daemon code / declared config + parser registry / LLM judgment), artifact store at `$HYDRA_HOME/gates/`, crash-resilience semantics (artifact before LLM, fail-closed close path, per-project lock), config schema, bootstrap contract. Evidence: co3u8 autopsy (25M tokens, ~85% of turns babysitting deterministic commands).



### Added
- Daemon gate runner (PRD-A FR-002, slice 1): `GateRunner` executes declared gates (test/lint/build) daemon-side with per-project serialization, worktree-only execution guard, timeouts, and atomic artifact persistence at `$HYDRA_HOME/gates/<project>/<bead>/` — artifacts written before any LLM involvement, `FinalizeStaleGateArtifacts` recovers `running` artifacts on restart. Parser registry: `exit-code`, `jest-coverage` (coverage % + failing suites; unparseable output fails closed). Config: per-project `gates:` block with `daemon_owned` flag (default false = legacy path). New `hydra gates verify <project>` bootstrap command dry-runs declared gates on the real checkout. Verified live against srv-smart-engine: lint/build pass; test correctly reports 26.97% < 80% with failing suites named.



### Added
- Worker extension resolver now supports git-installed Pi packages (`git/<host>/<org>/<repo>`, matched by repo segment to tolerate org drift/forks) and directory entry points (resolved to `index.ts`/`index.js` per pi convention). This unblocks the Hindsight pi extension (`hindsight` alias → `@walodayeet/hindsight-pi`) for workers — the first recall channel on the daemon path; npm installs still take precedence.

### Added
- Worker prompts now carry TURN ECONOMY guidance (batch independent tool calls in one message, one comprehensive probe over probe sequences, no full-suite re-runs per micro-edit). Motivation: session forensics on srv-smart-engine showed 25M tokens for one REFACTOR bead, 24.8M of it transcript cache-reads across 167 turns; turn count is the linear multiplier of the quadratic tax. Behavioral hedge only — measured model default is 1.00-1.02 tool calls/turn even with harness-level batching instructions.

### Fixed
- Sustained provider-capacity congestion now pauses spawning: exit-7 ("concurrent request limit") failures are tracked as a consecutive streak, and a streak of 6+ spanning >= 30 minutes opens an escalating spawn cooldown (15m -> 30m -> 60m -> 120m cap); any completed worker resets the streak. Blips (fast streaks) still never pause the fleet. Root cause of the co3u8 storm: 219 rejected turns over 3 days, each respawn re-billing the full transcript (~6.1M tokens, zero output).

### Fixed
- Validation fix-bead churn factory: `RunPRDValidation`/`RunTDDValidation` now dedup against open/in-progress beads by round-insensitive title prefix before creating fix or re-validation beads, and dedup within a single round. Previously every validation round spawned fresh duplicates for unresolved gaps — on srv-smart-engine 355 open machinery beads accumulated against 36 real chain beads, burning ~8M tokens per churn worker. Closed fix beads do not block respawn, so the max-rounds retry escalation still works.
- Fleet-wide priority inversion: every scheduler sort site used numeric-descending priority (P4 first) while beads convention is P0 = highest (`Priority::CRITICAL(0) < HIGH(1) < ... < BACKLOG(4)` in beads-rust). Ready queues therefore handed worker slots to the LOWEST-priority beads first — on srv-smart-engine P3 validation churn repeatedly took slots while 12 ready P1 chain beads waited. Flipped all six sites (`scheduler.GetReadyNodes`, `dag_navigator.GetReadyBeads`, `bead_discovery` br/bv paths, `dag` topological sort), made them stable sorts, and added the missing sort to the event-driven immediate-schedule path. Two legacy tests that encoded the inverted convention were corrected.

### Documentation
- Two planning PRDs from the srv-smart-engine campaign analysis: `docs/PRDs/typed-workflow-templates-daemon-gates-20260813.md` (bead-type workflow templates + daemon-owned deterministic gates + verification consolidation) and `docs/PRDs/memory-recall-quality-20260813.md` (recall telemetry, golden probes, bank hygiene). Planning artifacts only — no behavior change; the legacy chain stays default behind config flags.

### Fixed
- Worktree reconcile is now terminal-gated and quarantine-bounded: it only integrates `hydra/wt-*` branches whose bead is closed/completed/failed (open, in_progress, parked-with-stale-heartbeat, and unverifiable beads are skipped — a parked worker's live worktree and an open bead's 1,356-line worktree were previously treated as orphans and merged prematurely), and a terminal bead whose merge conflicts is quarantined to `hydra/quarantine/<key>` instead of retry-failing every sweep forever (observed: `conflicts=23` every 10 minutes for weeks). Nil status predicate fails closed.

### Fixed
- Validation fix beads no longer churn the board: Warning-severity gaps (e.g. "Missing Acceptance Criteria" — fires on any root bead without AC text) are now advisory log lines instead of worker-spawning fix beads, and all spawned fix/re-validation beads are priority-floored at P3 so validation meta-work never queue-jumps fresh PRD beads. On srv-smart-engine these beads inherited P0-P2 parents; ~25% of a day's commits were validation-round churn while the open board grew instead of draining.
- Charon now reaps ghost registry rows at runtime: workers that die without posting a completion marker (SIGKILL, OOM, daemon kill) left `working`/`parked` rows with frozen heartbeats that only the 24h startup sweep cleared — 19 accumulated in one day, poisoning fleet metrics. New `ReapGhosts` store method runs in Charon's scan cycle (10-minute throttle, 1h heartbeat cutoff) and deliberately spares `completed` rows so completion history survives for metrics. Placed before the no-tracked-workers early return — ghosts accumulate exactly when the fleet is idle.
- Worker token accounting now lands for successful completions: `stateReporter.Stop` no longer claims `state=completed` in its final liveness beat — the daemon report endpoint only accepts working/parked/hung and answered 400, silently dropping the token flush. Every exit-0 row recorded `tokens_so_far=0` while failures recorded correctly; terminal state is now left to the completion marker (`MarkCompleted`).

### Changed
- Default `overseer.max_workers` is now 2 (code default was 6, example was 5) — matches the operator-tuned production value; the AIMD governor scales within the ceiling under provider congestion.
### Added
- Worktree preflight gate (fail-early): before spawning, the daemon verifies a Node worktree has resolvable dependencies — node_modules present, no broken dependency symlinks (the `@haulmatic/common → ../../../lib-nestjs` class), lockfile-keyed success sentinel; missing/broken deps trigger a bounded deterministic install (npm ci/pnpm/yarn, 10m cap). An unrecoverable environment aborts the spawn (bead stays open) instead of burning a worker's lifetime on npm install.

### Fixed
- Activity watchdog no longer kills healthy turns when pi's RPC stdout stalls: pi session-file mtime is now a secondary liveness signal (`ActivityMonitor.SetSessionDir`) — a session write refreshes the activity clock. Closes the RPC-stall blindness found in the 3a4 chain walk (kill 75s after the last session-recorded event).

### Fixed
- Worker no longer watchdog-kills healthy pi mid-turn: pi RPC event lines (tool results carrying file reads/jest output, 71KB observed) exceeded bufio's 64KB scanner default, silently killing the stdout scanner and freezing the activity monitor. New `newPiScanner` (8MiB bound) used for both pi pipes, plus fail-loud post-loop scanner error logs. Root cause of chronic zero completions on tool-heavy beads.

### Added
- **`projects.multi_user_discovery` config gate.** The multi-user project scan (`~/projects` across all users) previously ran unconditionally at daemon startup. Set `multi_user_discovery: false` to restrict execution to `projects.list` only. Default stays `true` so existing deployments are unaffected. Operators can now focus the fleet on a single user's project without code changes.

### Fixed
- **Overseer worker timeout was park-blind and killed healthy workers mid-turn.** `WorkerLifecycle.Wait` used a hardcoded 30m wall clock that counted time the worker spent **parked** waiting out provider congestion. Live incident: `hydra-code-9p7` spent ~20m parked, woke at 07:43:58Z, was doing real work (tool calls at 07:58/08:00) when `[Overseer] Worker hydra-code-9p7 timed out, killing process` fired at 30m0.004s. The worker's own `maxTotalRuntime` already excludes parked time; the daemon-side clock now does too, using a ticker loop that accrues only active time and reads `parked` state from the registry. Also honors `config.Overseer.WorkerTimeout` instead of ignoring it.
- **Pan's stale judgments killed healthy young workers.** Two linked holes, one live incident (`srv-smart-engine-pes`, 2026-08-07: self-aborted 2m20s into a healthy turn → exit 1 → bead fault): (a) shepherd commands are queued per *bead* but judged per *worker instance* — a new worker registering for a retried bead drained the `abandon-with-summary` judged against its dead predecessor; `UpsertWorker` now clears pending commands on PID change (new instance = stale judgments void). (b) Pan hung-judged workers younger than its own 5m threshold (tokens legitimately 0 mid-first-turn, heartbeat predating the worker in the registration race); `Candidates` now grants a 2×`HungAfter` young-worker grace.
- **Signal deaths of workers classified as bead faults.** When Charon SIGKILLed a hung worker (or the OOM killer/operator killed one), the daemon recorded `worker exited with code -1` and classified it as a bead fault — 99 such deaths in 2h on 2026-08-07, each spawning fix beads that died the same way. The worker's own code only ever exits deliberately (1/2/3/5/6/7); a negative exit code is always an external kill, so `isInfrastructureExitCode` now treats all negative codes as infrastructure. Operators see hung-kill retries instead of phantom `closed-failed` + fix-bead recursion.
- **Watchdog-forced kills classified as bead faults.** When the activity-timeout or max-runtime watchdog killed Pi, the kill reason lived only in `cmdErr` while `classifyFailure` reads `result.Error` (pi's stderr) — so these provider-side hangs exited `-1`/`1`, recorded as bead faults, closed-failed beads, and spawned fix beads that died the same way (watchdog-found: `hydra-code-x77`). `appendForcedKillReason` now surfaces the kill reason into `result.Error` so the existing timeout markers classify it as exit 3 (infrastructure); `max total runtime` added as a timeout marker. Operators see fewer phantom `closed-failed` beads and no more fix-bead recursion from congested-provider stalls.
- **Epic auto-close no longer counts failed children.** `CloseReadyEpics` treated any `closed` task child as done — including `closed-failed` ones — so phase epics (`srv-smart-engine-757/tml/m5q`) auto-closed over failed work, reporting whole phases complete with no working code. A `closed-failed` child now blocks the epic unless its `Fix: <title>` bead closed without the failed label. The three wrongly-closed epics were reopened live.

### Fixed
- **Final token flush on worker exit.** Workers exit <30s after the last turn and `Stop()` posted only a bare completion marker (bead id + exit code), so parsed token totals never reached the registry — observed on hydra-code-zuo: worker logged `total=616789`, registry showed `tokens_so_far=0`, feeding Pan's false zero-token stuck verdicts. `Stop` now flushes a full final heartbeat (state + tokens) before the completion marker.
- **Final token flush on worker exit.** Workers exit <30s after the last turn and `Stop()` posted only a bare completion marker (bead id + exit code), so parsed token totals never reached the registry — observed on hydra-code-zuo: worker logged `total=616789`, registry showed `tokens_so_far=0`, feeding Pan's false zero-token stuck verdicts. `Stop` now flushes a full final heartbeat (state + tokens) before the completion marker.
- **Pan command-queue dedup.** Pan re-queued the same verdict every tick for dead workers that never consume their queue — observed live: 511 duplicate `abandon-with-summary` commands on one dead bead, MiniMax tokens burned per tick. Before enqueueing, Pan now skips a decision whose kind is already pending for that worker (fail-open on lookup error).

### Fixed
- **Human-gate close-path guard.** `BeadCloser.Close` now refuses to system-close `approval`/`approval-gate` beads — observed live: the daemon closed `srv-smart-engine-56z` with verdict `accepted` after a worker review, bypassing the human gate (the worker-side `br close` guard held; the daemon path did not). Human gates require an explicit human `br close`.
- **Approval-bead gate leak.** Beads with `Type: approval` (emitted by task-breakdown as human approval gates) were worker-executed whenever they were `open` — only `approval-gate` and in-progress `approval` beads were skipped. Observed live: a worker spent a full turn reviewing `srv-smart-engine-56z`. Approval beads are now skipped at all four gate sites (`dag_navigator`, `overseer.shouldSpawnWorker`, `scheduler` ×2) regardless of status, and route to `AgentHuman`.

### Changed
- **Pattern storage path (ops).** Live `/etc/hydra/hydra.yaml` `intelligence.patterns.storage_path` moved from the code checkout (`/opt/dev-tools/components/hydra/.hydra/patterns`, unwritable by the `hydra` user — every close-time verdict pattern failed with `Failed to store verdict pattern`) to `$HYDRA_HOME/.hydra/patterns`.

### Documentation
- **Roadmap: Pan as the self-healing/self-improving loop driver.** Added `docs/roadmap/bug-fix-loop-closure-20260803.md` §14: Pan's current functions (works/doesn't/planned), fleet-health pass, anomaly→bead, decision memory, and escalation cap. Status remains "under consideration (NOT committed)". Derived from the live 2026-08-06 dogfood session (`hydra-code-fku` watchdog).

- **PRD: park canary — coordinated congestion probing.** `docs/PRDs/park-canary-coordination-20260806.md`: when N workers are parked on provider-capacity, the oldest-parked real worker becomes the canary (probes every 30–60s via its own resumed turn — no synthetic pinger), followers extend to ceiling backoff, and a capacity-available signal wakes followers staggered. Fail-open to today's independent backoff. Proposed, not scheduled.

### Documentation
- **Roadmap: closing the bug-fix loop end-to-end.** New `docs/roadmap/bug-fix-loop-closure-20260803.md` — the agreed plan for ~80% automation of the QA→JIRA→fix→deploy→verify cycle: repro-first RED gate, confidence-tiered auto-merge, JIRA adapter, dev-env sentinel (Playwright/OpenAPI/log-fingerprint probes), pattern fast lane, and reuse for untested features. Mermaid workflows, actor/tooling tables, rollout order, industry references, plus two explicitly non-committed candidates: Piolium security-audit integration and Passmark AI browser regression probes. Planning only; nothing scheduled.

### Added
- **Close-time pattern extraction (verdict → pattern store).** When a worker emits a `HYDRA_VERDICT`, the worker now stores patterns locally at bead completion (outcome + files changed, confidence-gated) instead of waiting for settled-state detection — which stays disabled. Wired via `HYDRA_PATTERNS_*` env vars from `intelligence.patterns` config; fail-open. Also fixes `FilePatternStorage` filename collisions (same-second timestamps overwrote sibling patterns). Graphify indexer: a project with no code files (`No code files found - nothing to rebuild`) is now logged as a skip, not an error.
- **Scheduler congestion pause.** When the registry shows 3+ live parked
  workers (heartbeat within 10 min), the scheduler pauses new spawns for 5 min
  so the park-and-resume loop can drain the backlog before the fleet piles onto
  the same saturated provider slots. The gate is fail-open: a missing registry or
  counter error never blocks the scheduler. See `internal/core/scheduler.go` and
  `docs/architecture/shepherd.md`.
- **Named pi session files for park-and-resume across daemon restarts.** Workers now build a deterministic, file-name-safe session id from the bead id, create a directory under `$HYDRA_HOME/sessions`, and pass `--session` and `--session-dir` to pi. The session directory is exported as `PI_CODING_AGENT_SESSION_DIR` and the session id is reported to the daemon registry so a replacement worker can resume the same context after a restart.
- **Registry GC: reap stale worker rows on daemon startup.** `Store.ReapStale`
  removes terminal (`completed`) rows and rows whose last heartbeat is older
  than the cutoff, plus any queued commands for those beads. Wired into
  `internal/core/service_starter.go` with a 24-hour cutoff so Pan and the
  scheduler never act on zombie registry state left by prior runs.
- **Worker registry + Pan, the shepherd (PRD parts 4 & 6).**
  `internal/registry`: daemon-owned SQLite store replacing `workers.json` —
  worker records (state, pi session id, error triple, token progress, park
  fields, nudge count) and a per-worker command queue (`continue`, `steer`,
  `compact-and-continue`, `abandon-with-summary`). Completed workers are
  never resurrected by late upserts. `internal/pan`: fail-open shepherd
  pi-agent with its own provider key (`shepherd.pan` config) that reads the
  registry and queues decisions for the ambiguous tail — parked-overdue,
  hung, and verdict-missing-past-nudge-cap workers. Everything else stays on
  deterministic rules, which run with or without Pan.
- **Worker registry API + worker state reporter (PRD parts 2 & 4).**
  Daemon endpoints `POST /api/v1/workers/report`, `/workers/completed` and
  `GET /api/v1/workers/commands` (consume-on-read). Workers self-register,
  heartbeat state every 30 s, drain shepherd commands, and report their
  terminal state before exit; `HYDRA_API_URL` is passed at spawn and an
  unreachable registry degrades to pre-registry behavior (fail-open).
- **Verdict closure protocol (PRD part 3).** Worker prompts now demand a
  final `HYDRA_VERDICT` JSON block; an rpc turn that ends without a valid
  done/abandon verdict is re-prompted in the same live pi session (cap 3)
  instead of being torn down — pi is no longer killed merely because it
  stopped talking. Hard kills remain reserved for hung processes and are
  system failures, not bead failures.
- **Park-and-resume on provider congestion (PRD part 2, Tier A).** A failed
  rpc turn classified as provider capacity (concurrency limit, engine
  overload, 429 — marker-first because Kimi's error `type` field lies) now
  parks the live pi session: state reported as `parked` with wake time,
  backoff 30 s→5 min with jitter, park cap 60 min (`HYDRA_MAX_PARK_MINUTES`),
  then the same session is re-prompted. Parked workers are exempt from the
  activity timeout, and parked time is excluded from the max-total-runtime
  budget. Zombie-park guard: if pi died while parked the wake write fails
  and the normal failure path takes over.
- **PRD: worker park-and-resume on provider congestion**
  (`docs/PRDs/worker-park-and-resume-20260731.md`). Workers whose session is
  interrupted by provider-capacity errors (403 concurrency, 429, overload)
  should park the live pi rpc session and resume it after backoff instead of
  terminating and losing all context — the root cause of repeated rework seen
  in the 2026-07-31 `hydra-testbed-y05` autopsy (~50 min compute, zero merged
  output). Rewritten after design review: 3-tier error classification
  (auto-park / escalation-park on credential errors / close), worker control
  loop with named session files (parked workers survive daemon restarts),
  verdict-based closure (`HYDRA_VERDICT` required before pi is killed),
  daemon-owned SQLite registry + command queue, a four-knob timeout hierarchy
  with a monotonic lifetime cap, and "Pan" — a pi-agent shepherd with its own
  provider key that handles the ambiguous tail and is fail-open by contract.

### Fixed
- **Worker session flag broke every fresh bead.** The worker passed `--session <id>` to pi, which resumes an EXISTING session and exits 1 with `No session found matching` when it does not exist — i.e. for every fresh bead. First real fleet run after the named-session PR failed 192/192 executions in <1s and the shepherd marked the beads terminal. Now passes `--session-id`, which creates the named session when missing.
- **Worker never reported token usage to the registry.** Pi emits per-message usage on its rpc stream (one execution: 8,009 usage events, 42k tokens in a single message) and the worker parsed the session stats — then only logged them. `SetTokens` was dead code; the registry always showed `tokens_so_far: 0`, which made Pan's zero-token stuck heuristics condemn healthy workers. The turn-end stats are now forwarded via `reportTurnTokens` (absolute: `get_session_stats` is cumulative per named session).
- **Worker session flag broke every fresh bead.** The worker passed `--session <id>` to pi, which resumes an EXISTING session and exits 1 with `No session found matching` when it does not exist — i.e. for every fresh bead. First real fleet run after the named-session PR failed 192/192 executions in <1s and the shepherd marked the beads terminal. Now passes `--session-id`, which creates the named session when missing.
- **Data race in GraphQL event resolver.** `Resolver.Stop()` closed `eventChannel` while `Broadcast*` methods could still select-send on it, and the broadcaster's shutdown path closed subscriber channels while in-flight send goroutines were still using them — `panic: send on closed channel` under `-race` (CI run 30822045466, 10/10 locally). Stop no longer closes `eventChannel` (the broadcaster exits on `ctx.Done` anyway), and subscriber close/send now share a per-subscriber mutex + closed flag.
- **Graphify indexer test flake fixed.** `TestGraphifyIndexer_*` tests that start a background build now wait for it to drain (`waitIdle` polling `inFlight`) before returning; previously teardown intermittently raced the goroutine's `.gitignore` write and failed `t.TempDir` cleanup with "directory not empty" (~20% of runs).
- **Noisy `br close` exit 2 failures when the target directory is not a beads workspace.** The worker and Charon now check for a `.beads` directory before invoking `br close`; if the workspace is missing (testbed beads, cleaned worktrees, or unconfigured project paths), the close is skipped at debug level instead of logging a warning/error.
- **`go test ./...` now passes by default.** Environment-dependent integration tests in `internal/integration` and `internal/pi` are gated behind the `integration` build tag; a stale hard-coded skill count in `TestSkillUpdater_PerformanceTarget` is now dynamic; and the `fmt.Println` vet warning in `docs/collective-unconscious/examples` is fixed.

### Changed
- **Pan defaults to a dedicated MiniMax key.** `hydra-config.yaml.example` and the shepherd config regression test now use `provider: minimax`, `model: MiniMax-M2.7`, and `api_key_name: MINIMAX_API_KEY`.
- **Runtime state moved out of `/opt/hydra` into `/opt/dev-tools/var/hydra`.**
  Code (`components/hydra`), shared config (`config/pi`) and runtime state
  (`var/hydra`) now sit side by side under `/opt/dev-tools`, split by what changes
  them. `$HYDRA_HOME`, the `hydra` user's home, three systemd units, logrotate and
  the shell-script defaults all point at the new path; `/opt/hydra` remains as a
  compatibility symlink and will be removed after a clean run. `var/` is
  gitignored in the monorepo — state must never sit in a git working tree, which
  is how a `.hydra-events/` directory once got committed. See
  `docs/architecture/filesystem-layout.md`.
- **Per-bead worker logs rotate daily with `maxsize 64M`** instead of weekly with
  `rotate 2`, which could not contain a directory growing at ~15 GB/day.
- **Pi config consolidated to one shared directory.** `pi.worker_config_dir` now
  equals `pi.config_dir` (`/opt/dev-tools/config/pi`), so hydra workers and human
  shells resolve the same skills and extensions. The old generated copy at
  `/opt/dev-tools/config/pi-worker` had drifted to 239 npm packages against the
  canonical 242 with nothing detecting it, and both of its stated purposes were
  obsolete. `scripts/setup-pi-worker.sh` is removed. Operators: see
  `docs/architecture/pi-config.md`.
- **Concurrency governor steps down by one instead of halving**, and gains
  clock-driven recovery (`recovery_interval_seconds`, default 300). A single
  one-hour provider outage previously took the limit 4 -> 2 -> 1 and left it at 1
  for 2.5 hours, because recovery required a full outcome window and a low limit
  suppresses outcomes. Exit 7 no longer feeds the governor.

### Added
- **`pi.manage_config`** (default `false`): whether the daemon may WRITE to
  `pi.config_dir` — create structure, install Pi/LSP, refresh skills on a timer.
  Must stay false whenever the directory is shared with human shells, or daemon
  startup reshapes the team's environment.
- **`scripts/verify-pi-config.sh`**: asserts the two config dirs match, the
  directory is a Pi package root, every allowlisted extension resolves, each
  `~/.pi/agent` points at it, and no retired copy is still referenced.

### Documentation
- **Updated `docs/operations/handoff-20260731.md`** to the 2026-08-03 state:
  `development` now at `b47b527`, `go test ./...` and `go vet ./...` green,
  integration tests gated behind the `integration` build tag, and open work
  trimmed to the remaining runtime/dogfood items.
- **Rewrote `docs/operations/handoff-20260731.md`** for the 2026-08-02 state
  of the box: live park-and-resume, registry GC, Charon liveness, timeout
  hierarchy, Pan status, verification commands, and the remaining open work.
- `docs/architecture/shepherd.md` gained a section on the worker registry,
  park-and-resume, the verdict protocol, Pan, liveness-based Charon, and the
  timeout hierarchy with its env knobs (`HYDRA_API_URL`, `HYDRA_MAX_NUDGES`,
  `HYDRA_MAX_PARK_MINUTES`, `HYDRA_WORKER_LIFETIME_MINUTES`).

### Changed
- **Retention stores the HYDRA_VERDICT, not raw run output** (Hindsight LLM
  Sink A). The extraction LLM (MiniMax) used to receive the full turn text —
  tens of KB including thinkingSignature blobs — per run. Retain now sends
  the parsed verdict (~200 tokens: done, ac_met/ac_unmet, files_changed,
  verification) when one exists, and sanitized output (thinkingSignature and
  base64 runs stripped, 4KB tail-biased cap) otherwise. Memory quality
  improves because extraction sees distilled signal; failure evidence is
  preserved (sanitized tail + the A2 cause extractor reads the worker log
  separately and is untouched).

### Added
- **Workers honor shepherd commands at park wake** (PRD Part 3 consumer).
  Commands were polled over the registry API but never drained — a steer or
  abandon from Pan had no effect. The park-wake path now drains: `steer` and
  `continue`/`compact-and-continue` reshape the wake prompt;
  `abandon-with-summary` fails the turn with `abandoned by shepherd: …`
  (deliberately un-parkable) and closes stdin so pi exits.

### Fixed- **Pan example config no longer sets `thinking: high` for the MiniMax provider.** The `pi` CLI rejects `--thinking-level` for the `minimax` provider, causing Pan invocations to fail on startup; the example config and the shepherd load test now omit the field. Live `/etc/hydra/hydra.yaml` updated.

- **`shepherd.pan` config silently parsed to zero values.** `LoadConfig`
  copies `rawConfig` fields one by one; `Shepherd` (and in
  `LoadConfigUncached`, also `Validation`/`ComplianceCycle`) was declared on
  `Config` but never copied, so Pan stayed disabled in production with no
  error. Added the missing copies plus a load-survival regression test.
- **Charon no longer kills live workers on wall-clock time.** The stuck-worker
  sweep used a 15m wall timeout and on 2026-08-01 killed every fleet worker
  at ~15m: workers parked on provider 403s, and one worker while it was
  emitting its closing `HYDRA_VERDICT`. The sweep is now registry-aware
  (PRD parts 4–5): parked workers and workers with a fresh heartbeat (30s
  reporter) are alive by definition and never killed on time alone; only a
  stale heartbeat (>5m) means hung. Workers without a registry row keep the
  legacy wall-clock behavior.
- **Worker lifetime cap replaces the 15m total-runtime kill.** The worker's
  internal max-runtime (15m) could fire mid-verdict on long TDD chains; it
  is now a 2h monotonic lifetime cap (`HYDRA_WORKER_LIFETIME_MINUTES`),
  parked time excluded. The 4m activity timeout stays the real hang
  detector.
- **Charon fleet deadlock: phantom worker records blocked orphan-bead recovery
  forever.** A worker that died without cleanup left a `running` record in
  `workers.json`; `recoverOrphanInProgressBeads` then believed a live worker
  existed and skipped the bead, while the dead-PID sweep that would clear the
  phantom sat behind `getOpenBeads`, which aborted the whole scan whenever any
  one project's `br list` failed (lib-nestjs/demo exit 2). Result on
  2026-07-31: 11 beads stuck `in_progress`, `ReadyNodes:0 BlockedNodes:14`,
  fleet idle for hours. The dead-PID sweep now runs first, before any br
  dependency; `getOpenBeads` skips failing projects instead of aborting; and
  the orphan-kill decision is fail-closed per project — a worker whose project
  cannot be listed is never killed on the strength of a partial open-bead map.
- **`deploy.sh` no longer overwrites live configuration.** `create_config`,
  `create_systemd_service`, `setup_logrotate` and `setup_firewall` wrote canned
  templates on every run. On 2026-07-31 an ordinary deploy replaced the tuned
  `/etc/hydra/hydra.yaml` with a 749-byte template — losing `max_workers`, the
  provider pool, the governor, the shared Pi config, hindsight and the project
  list — and replaced the systemd unit with one missing
  `EnvironmentFile=/etc/hydra/api-keys.env` and the beads binaries on `PATH`. All
  four steps are now no-ops when the target exists, and require
  `HYDRA_PROVISION=1` on a genuinely fresh box.
- **Per-bead worker logs are capped at 32 MB per stream.** `logs/workers/` had
  reached 30 GB across 1,590 files in two days, with single beads writing 853 MB
  of pi stream JSON. A capped log says so in the file, so it cannot be mistaken
  for complete output.
- **Tests no longer write to production state.** `worktreeRoot()` roots worktrees
  under `$HYDRA_HOME`; the daemon exports it and workers inherit it, so a worker
  running this repo's own suite registered every test worktree in live state —
  10,251 dead entries, each named after a Go test function, which Charon walked on
  every orphan sweep. `TestMain` now pins `HYDRA_HOME` to a per-run temp dir.
- **Retry cap had never fired.** Retry state was cleared on successful *spawn*
  rather than completion, so the count could never reach 2. Provider failures
  (429/timeout/overload/quota) now pace retries without consuming the budget;
  only genuine bead faults exhaust it.
- **Daemon log no longer receives pi's JSON stream** (1.56 GB in 4h50m), which
  also made unanchored log greps report failures that never happened.
- **`deploy.sh` builds packages, not single files** — `go build cmd/worker/main.go`
  ignored sibling files and broke the worker build.
- **Nightly testbed matrix counted `"0\n0"`** from `$(grep -c ... || echo 0)`,
  which disabled its own starvation guard, and its daemon-log filter counted
  worker passthrough (reporting 20 gap-to-prd cycles where the daemon logged 0).

### Removed
- **llm-tldr, entirely.** It went unmaintained, returned zero semantic units on
  Go (silently degrading to grep), and its service on :8787 was not running in
  production — so the structural-intelligence layer had been dead for a while
  without anything failing loudly. graphify replaces it.
  Deleted: `internal/intelligence/tldr.go`, `internal/core/tldr_indexer.go`,
  `internal/core/impact_analyzer.go` (its only caller, `analyzeImpactWithTLDR`,
  was already dead code), the hub's client/indexer wiring, the `ToolTLDR`
  refresh path, `TLDRConfig`/`TLDRIndexingConfig`, and the GraphQL tldr
  plumbing. Two consequences worth naming rather than burying:
  - `ChainPatternExtractor` no longer runs a semantic search per chain. That
    call is the source of the recurring `TLDR indexing failed … after 4
    attempts: indexing timed out after 30 seconds` in the daemon log, so this
    also removes a standing 30s-per-chain stall.
  - GraphQL `Bead.impact` now returns nil. The field and its nullable schema are
    kept, so no API contract changes — and nil is what production already
    returned, since the backing service was down. Wiring it to
    `graphify affected` is a follow-up: real reverse traversal, not a rename.
  `ScopeValidator` no longer borrows its file limit from
  `intelligence.tldr.max_files`; that coupled a scope invariant to a tool's
  config and silently disabled the check whenever the tool was disabled. It is
  now the `maxScopeFiles` constant.

### Added
- **Verdicts feed memory (Shepherd D3, dogfood-built + hand-finished).** The
  system's most expensive judgments no longer evaporate: fresh gap
  adjudications (real/false_positive with reasoning) and failed closes with an
  extracted cause are retained as Hindsight memories at judgment time, and the
  adjudicator now sees up to 3 prior verdicts on similar gaps in its prompt.
  Ambiguous verdicts and cause-less failures are never retained (noise, not
  memory); cached fingerprints never re-retain. Fail-open everywhere — a
  Hindsight outage costs memories, never runs. New event: `verdict_retained`.
- **AIMD concurrency governor (Shepherd B1, dogfood-built).** When provider
  health degrades, the fleet's own request pressure raises its exposure to the
  overload it is stalling on (219 activity timeouts vs 4 clean turns on
  2026-07-28). New `ConcurrencyGovernor` applies TCP-style congestion control
  to worker slots: halve effective concurrency when ≥50% of the last 8
  provider-shaped outcomes failed, regrow one slot per quiet 2-minute window,
  floor 1 — never a hard stop. Only exit codes 2/7 and activity timeouts count
  as failures; a bead failing on its own defect (exit 1/5) never shrinks the
  fleet. Config `overseer.concurrency_governor` (default disabled = byte-
  identical behaviour). Built end-to-end by a hydra dogfood run (commit
  authored by the fleet, harvested after gates closed).

### Fixed
- **A concurrency ceiling was being classified as quota exhaustion**, so the
  daemon froze every spawn for 30 minutes over a condition that clears in
  seconds. Kimi returns "You've reached your concurrent request limit" with the
  same HTTP 403 and `access_terminated_error` type as genuine quota
  exhaustion, and only the message distinguishes them — so the quota markers
  matched. On 2026-07-30 that produced 37 fleet-wide pauses in one day and
  starved a dogfood run to roughly a 10% duty cycle while the account's quota
  was never touched; a single sequential probe against the same provider
  succeeded. Concurrency refusals now classify as exit 7, which feeds the AIMD
  governor (thin the fleet) instead of the circuit breaker (freeze it) — the
  response the provider's own message asks for.
- **Fix-bead dedup was structurally dead.** `br list --json` returns
  `dependencies: null` (only `dependency_count`), so the dependency-based
  duplicate check never matched — `fix_deduped` events stayed at zero across
  every run and run D3 created four identical fix beads for one original.
  Dedup now matches by the deterministic fix title ("Fix: <original title>")
  first, dependencies second.
- **Cause extractor no longer harvests stringified source code.** A worker
  implementing a log parser echoed code containing `--- FAIL:` into its log
  and the extractor shipped that as the "cause" (run D3). Failure markers now
  anchor at payload start and lines carrying literal escape sequences
  (`\n`, `\t`, `\"`) are rejected as echoed source.
- `DefaultEventsDir` no longer falls back to a cwd-relative `.hydra-events/`
  when HYDRA_HOME is unset — tests were writing event day-files into the repo
  (one got committed). Unset HYDRA_HOME now disables the central stream.
- **Failed chains quarantine instead of integrating (Shepherd A1).** Chain
  "completion" only ever meant every bead reached a terminal state — closed-as-
  failed counted the same as completed, so failed chains merged into the working
  branch exactly like successful ones (a deliberately red test landed on the
  testbed's branch this way). Failed closes now get a durable `closed-failed`
  label (issues.jsonl carries no close reason; a label survives daemon restarts
  where in-memory state would not — four mid-run restarts this week),
  `ChainHasFailedBeads` reads it at completion time, and failed chains go to
  `hydra/quarantine/<key>` — work preserved for the fix bead to inspect, working
  branch untouched. Emits `chain_quarantined` and `bead_closed` events.
  `BeadFileReader` now parses labels (previously dropped them entirely).
- **Cause-aware, deduplicated fix beads (Shepherd A2).** A fix bead's description
  now carries the original's exit code plus a `What failed last time` block
  extracted from its newest worker log (`ExtractFailureCause`: priority-ordered —
  model failure > failing test > package FAIL > Error — passthrough JSONL
  excluded except the model-failure marker, 2 KB budget). Creation is deduplicated
  via `findOpenFixBeadForOriginal`: one open fix bead per original, closing the
  duplicate-spawn defect (the A2 build run itself produced SEVEN duplicate
  cause-less fix beads for one bead before this landed — the problem demonstrating
  its own fix). Built by dogfood run A2 (FR-001 initially stubbed; a later fix
  bead completed it), harvested from the working tree since both gates correctly
  closed `failed` on the mid-run mess.
- **Structured daemon event stream (Shepherd C1/C2).** Typed JSONL written by
  daemon code about daemon decisions — worker output structurally cannot enter
  it, retiring the passthrough-contamination class (4 incidents). Two tiers:
  central day-files under `HYDRA_HOME/events/`, and per-repo
  `.hydra/events.jsonl` mirrors (gitignored) so each user sees what hydra did to
  their repo. First emitters wired in the closer: `fix_created`, `fix_capped`,
  `fix_deduped`.
- **rpc mode and the `[rtk, caveman, context-mode]` allowlist are now the
  defaults.** A config that says nothing about the protocol gets both. rpc is the
  default because text mode initialises no extension host at all — verified: a
  marker extension loaded with `-e` in text mode is imported but nothing runs and
  `pi --verbose` lists no extensions, while over rpc the same setup produces
  `RTK rewrite: git status --short -> rtk git status --short`. The allowlist is
  the default because Pi's own discovery loads every installed package (48,315
  tokens on one measured turn versus 12,059 for the allowlist), and the extras —
  themes, editor widgets, subagent UI — cannot be used by a headless worker.
  Opt out with `mode: text` or `extensions: []`; both are explicit values, and
  the defaulting distinguishes them from an omitted key. Defaults are applied
  *after* the raw config is copied, because `LoadConfig` assigns
  `config.Agents = raw.Agents` wholesale — anything set in `DefaultConfig` would
  have been silently discarded by any file containing an `agents:` block.
- Pi rpc mode is now actually wired to the worker execution path. `agents.pi.mode`
  previously existed, parsed, and had a tested `RPCEnabled()` accessor while
  changing nothing: beads run in a separate `hydra-worker` process that
  hand-builds its own `pi --offline --no-session` command, so the daemon's mode
  never reached the process that spawns Pi (and `spawnWorkerViaAgent`, the only
  in-daemon caller of the rpc connector, is dead code). `buildPiEnv` now forwards
  `HYDRA_PI_MODE` / `HYDRA_PI_RPC_AUTO_CONFIRM`, and the worker drives the rpc
  protocol: keeps stdin open (UI-ask replies travel back over it — closing it is
  what made earlier attempts hang), reads the verdict from the event stream
  rather than the exit code (the worker kills Pi itself after `agent_end`, since
  Pi waits for another prompt instead of exiting), treats an `agent_end` carrying
  `stopReason: error` as a failure, and logs per-bead token counts from
  `get_session_stats`. Text mode is untouched and remains the default.
  Verified end-to-end on the VPS: `--mode rpc` in the argv, `agent_end` reached,
  file actually written by the model, worker exit 0.
- **Measured, and it does not yet pay off.** Same bead, three configs:
  rpc with the worker config dir → 10,194 tokens / 18.0s / **no extensions
  loaded**; rpc without it → 61,711 tokens / 87.8s / extensions **loaded**
  (`notify=3 setStatus=4 setWidget=1`); text mode → 14.7s. The premise for rpc
  was that rtk/context-mode/caveman cut tokens; loading them cost **6x more
  tokens and ~5x more wall-clock** on this task. Cause of the split: setting
  `HYDRA_PI_CONFIG_DIR` makes the worker export `PI_CODING_AGENT_DIR`, and with
  that set `pi list` reports "No packages installed" — so today a worker can have
  its skill flags **or** its extensions, not both. Do not enable
  `agents.pi.mode: rpc` expecting savings until that is resolved and measured on
  real beads.
- Pi connector can speak pi's rpc JSONL protocol (`agents.pi.mode: rpc`,
  default text). This is Phase 0: the connector answers `extension_ui_request`
  asks — previously ignored entirely, which is why every rpc attempt hung once
  `@aliou/pi-guardrails` asked a `confirm` — denies by default unless
  `rpc_auto_confirm` is set, refreshes the activity deadline on *every* event
  rather than assistant text only, and parses `get_session_stats` so token
  counts become measurable per run. `hydra --rpc-selftest "<prompt>"` exercises
  one real turn end-to-end without the daemon.
  *Authored by a Hydra dogfood run (run G), verified by its compliance gate.*
- `tools/pi-rpc`: standalone client for `pi --mode rpc`, isolated from the
  daemon, proving what rpc mode offers before any hydra change. Verified live:
  pi's extensions **do** initialise in rpc mode and rtk rewrites commands
  (`RTK rewrite: git status --short -> rtk git status --short`) — whereas the
  piped text mode hydra uses initialises none of them; and the session-control
  commands (`get_state`, `get_session_stats` with real token counts,
  `get_last_assistant_text`, `get_available_models`, plus `abort`, `follow_up`,
  `cycle_model`) all work. Includes the protocol lesson that model failures
  hide inside the message (`stopReason: "error"`), so an envelope-only client
  reports an expired key as a passing turn.
- Observer signatures for `orphan-recovery` (threshold 3 — one recovery is
  hygiene, a burst means workers are dying) and `quota-exhausted` (threshold 1),
  so both classes surface within one polling interval.
  *Authored by a Hydra dogfood run.*
- Gap adjudication can now be routed to a stronger model at maximum reasoning
  effort (`compliance_cycle.adjudicator_thinking`, alongside the existing
  `adjudicator_model`). Adjudication is the one high-consequence, low-volume
  judgment in the loop — a wrong verdict drops a real defect or spawns a wasted
  bead chain — so it is where paying for reasoning effort is justified, unlike
  the high-volume build phases. `normalizeThinking` coerces `none`/`off` to
  `low` because disabling thinking silently reroutes K3/K2.7 requests to the
  older K2.6 model.
- `scripts/prd-author.sh`: author a PRD on the VPS with the strong model
  (`k3-256k` at `max` effort by default) via the create-prd skill — the
  equivalent of authoring in Claude Code, which is not installed there.
  User-initiated only; Hydra never calls it.
- `docs/architecture/model-allocation.md`: the evidence-based model allocation
  doctrine — intelligence at the low-volume/irreversible decision points
  (PRD authoring, decomposition, adjudication, diagnosis), cheap models for
  the hundreds of build beads, with the run data showing one-shot rate 1.0 on
  K2.7 across every scored run.
- `tools/prd-lint` + task-breakdown preflight: a deterministic linter
  enforcing the create-prd compiler contract exactly as the task-breakdown
  parser reads it (same-line Target Files, Type/Priority, AC bullets, unique
  IDs, resolvable references, nfr-config commands), wired as a fail-open
  preflight into `cli.mjs` (`--no-lint` escape hatch; missing binary skips).
  One chokepoint covering every PRD author, including the compliance cycle's
  auto-generated gap PRDs. All five current PRDs lint clean; the linter lints
  its own PRD. *Authored by a Hydra dogfood run.*
- `tools/hydra-observer` (self-improvement roadmap Phase 1, deterministic
  core): scans the daemon log for known failure signatures (empty-diff storms,
  stuck workers, panics, blocked users, escalations, timeouts, max-cycles),
  compares hydra-eval metrics against a stored baseline (one-shot-rate drops,
  zero-accept runs), and writes fingerprint-deduplicated ranked backlog
  entries. Observation only — no production mutation. Exit-signal verified:
  replaying the 2026-07-22 empty-diff incident produces one critical backlog
  entry; re-runs produce zero duplicates. *Authored by a Hydra dogfood run.*
- Security scan gate: `tools/security-scan` wraps gitleaks + govulncheck and
  emits analyzer-schema gaps (secret values redacted, fail-open when scanners
  are missing); wired into `hydra-compliance-analyzer` as an always-on
  `security-scan` check, so security findings flow through the adjudicator →
  gap-to-prd → beads pipeline and block PRD approval until fixed.
  *Authored by a Hydra dogfood run.*
- Per-user PAT branch publishing: at each scheduling tick (opt-in via
  `output.push`), Hydra reconciles every project's completed worktree commits
  onto its working branch and pushes it to origin **authenticated with the
  project owner's personal access token** (`PushBranchWithToken` builds a
  one-shot `x-access-token:<pat>@host` URL — never persisted, token redacted
  from logs). Only branches ahead of their remote are pushed. Net effect: by
  the end of a run, every repo's working branch (with the per-bead
  micro-commits, `--no-ff`-preserved) is on the remote for a developer to pull
  and build locally. Fail-open — never affects scheduling.
- `hydra-onboard` now also grants the daemon **write** access (`u:hydra:rwX` +
  default ACL) to every project path declared in a user's `~/.hydra/config.yaml`,
  not just read on `~/.hydra`. Without this the daemon could discover a
  per-user project but not build, commit, or integrate into it (its home-owned
  project dir was not daemon-writable). Verifies a write test per project.
- `hydra-onboard-sweep` now re-onboards **every** user each cycle (onboard is
  idempotent) instead of skipping users whose config is already readable. The
  old skip missed projects added — or project dirs created — after a user's
  first onboard, so their write ACLs were never granted. Now adding a project
  (or cloning its dir later) is picked up within one sweep with no special
  command. Non-account home dirs (fixtures, stray/`-`-prefixed names) are
  skipped cleanly, not reported as failures.
- Publishable & observable output: worker commits now use a readable
  conventional-commit message derived from the bead (`BuildCommitMessage`:
  feat/fix/refactor/test/chore from phase or type, PRD/bead trailers) instead
  of `bead(id): title`; singleton beads optionally **push** their working
  branch after integrate (`output.push`/`output.remote`, fail-open and silent
  on protected/rejected/auth); and every repo gets a curated
  `.hydra/activity.log` (`AppendActivity`, auto-adds `.hydra/` to `.gitignore`).
  Foundation (commit_message/git_push/activity_log/config) built by a Hydra
  dogfood run; close-path wiring + activity-log tests finished by hand after
  the wiring beads failed in the run.
- `.claude/skills/create-prd`: symlinked to the canonical `create-prd` skill in
  `dev-tools/config/pi/skills/create-prd` (pi-agent-config repo), so Claude
  Code and the pi agent share one PRD-authoring skill instead of two drifting
  copies. See pi-agent-config CHANGELOG for the doctrine content.

### Changed
- Refactored `PiConfig.ModelForPhase` to remove the duplicated primary provider/model return path and simplify the override logic. *Authored by a Hydra dogfood run.*
- Further optimized `tools/hydra-eval`: artifact-parsing now uses a single shared `forEachLine` helper, marker/payload extraction is generalized via `payloadAfter`, and `main` parses the daemon log once with `ParseDaemonLog` instead of four separate passes. *Authored by a Hydra dogfood run.*
- Refactored `tools/hydra-eval/parse.go`: artifact-parsing loops are now driven by a shared `scanLines` helper, marker/payload operations use `bytes` instead of `strings` to avoid per-line allocations, and the three public parsers (`ParseCloseEvents`, `ParseBeadsJSONL`, `CountMarker`) share a single scanner setup. *Authored by a Hydra dogfood run.*
- Refactored the verified-close target-less warning: `changedFiles` is now
  computed once for both the diff gate and the scope gate (removing one
  `git status` call), the target-set construction is shared via
  `buildTargetSets`, and the warning logic is extracted into
  `targetlessWarningPaths` for better readability and testability.

### Added
- Multi-user onboarding tooling (`packaging/`): `hydra-onboard <user>` grants
  the daemon durable, minimal access to a user's `~/.hydra` (traverse-only ACL
  on the home dir + read + **default** ACL on `.hydra` so it survives config
  rewrites); `hydra-onboard-sweep` + a systemd timer apply it across all homes
  every 15 min so new users are picked up hands-free. Fixes the recurring
  home-dir-traverse permission gap that silently skipped users (e.g. a real
  user blocked from 2026-07-06 until 2026-07-20).
- Multi-user discovery diagnostics: a config **confirmed present but
  unreadable** (stat succeeds, read fails) is recorded as BLOCKED with an
  actionable remediation (`run: hydra-onboard <user>`) and a startup summary,
  instead of a silent one-line skip; `MultiUserDiscovery.Blocked()` exposes
  the list for status. Homes the daemon cannot even traverse (EACCES on stat)
  are only counted as "unreachable", not blamed on a user — that case is
  indistinguishable from an ordinary non-Hydra user with a locked-down home,
  so per-user remediation there would be a false positive (the root-run
  onboard sweep is the authority that resolves it).
- Per-phase model config: `agents.pi.phase_models` maps TDD phases (`red`, `green`, `refactor`, etc.) to optional provider/model overrides, with case-insensitive lookup, partial (model-only) overrides, and a deep-copied config cache. `PiConfig.ModelForPhase` resolves the effective provider/model for a phase. *Authored by a Hydra dogfood run.*
- `eval/run.sh`: one-command scored dogfood run — clone, seed, approve,
  watch to completion, score with hydra-eval scoped to the run's start
  timestamp.
- Per-phase worker doctrine: RED/GREEN/REFACTOR beads and fix beads now get
  phase-specific working discipline injected into their prompts (fix beads
  follow a strict reproduce→isolate→verify→fix→prove root-cause protocol);
  universal engineering doctrine (evidence-over-narration, smallest-change,
  fail-open vs fail-closed, honest completion) added to AGENTS.md.
- Benchmarks for `tools/hydra-eval` artifact parsers (`ParseCloseEvents`, `ParseBeadsJSONL`, `CountMarker`, `ParseDaemonLog`). *Authored by a Hydra dogfood run.*
- RED-phase unit tests for `tools/hydra-eval` artifact parsers (`ParseCloseEvents`, `ParseBeadsJSONL`, `CountMarker`). *Authored by a Hydra dogfood run.*
- Target-less scope warning on verified close: implementation beads (e.g.
  `fr`, `fix`, `implement`) that declare no Target Files now log the
  non-allowlisted files they changed, while non-implementation beads (e.g.
  `epic`, `approval`, `nfr-*`) remain exempt. Warn-only, never rejects the
  close; allowlisted paths (`CHANGELOG.md`, `docs/**`, `go.mod`, `go.sum`)
  are filtered out. *Authored by a Hydra dogfood run.*
- Target-Files scope gate on verified close: worker changes outside a bead's
  declared Target Files are warned about or rejected
  (`validation.enforce_target_scope: off|warn|block`, default `warn`).
  *Authored by a Hydra dogfood run (PR #51).*
- Worktree integration reliability: singleton beads integrate their worktree
  branch on verified close; an hourly reconciler sweeps orphaned `hydra/wt-*`
  branches back into the working branch before worktree GC.
  *Authored by a Hydra dogfood run.*
- Learning loop: retry prompts carry the previous attempt's failure summary
  (Reflexion, in-process and across respawns via `HYDRA_PREV_FAILURE`), and
  workers deterministically inject the top-3 CASS playbook rules into every
  prompt (`cm context --json`, fail-open).
- Stale-worktree GC: Charon removes per-bead worktrees older than 24h
  (skipping active workers); validated live at 300 → 18.
- Per-bead worker logs under `$HYDRA_HOME/logs/workers/`; only notable lines
  (errors, 401/429, timeouts) surface to the daemon log (PR #50).
- Leveled logging via `internal/logx` (`HYDRA_LOG_LEVEL`), DEBUG/INFO to
  stdout, WARN/ERROR to stderr (PR #50).
- `eval/extract.sh`: extracts a scope-clean patch of a dogfood run's declared
  Target Files from the target clone.

### Fixed
- `failed_closes` in the eval metrics was counting the model's own words. The
  daemon streams worker stderr into its own log, and that payload is raw JSONL
  including `thinking_delta` text, so a substring marker matched whatever the
  model happened to write. On run H the `closed as failed` marker matched 7,884
  lines — **7,739 worker passthrough, 145 genuine `[closeBead]` lines** — and the
  metric was reported as 6,707. Marker counting now skips `[stderr]`/`[stdout]`
  passthrough lines. This affected every marker-derived metric, not just this one.

### Changed
- Default Pi extension allowlist is `[rtk, context-mode]`. context-mode was
  dropped a few hours earlier on the strength of one trivial prompt where it only
  added tokens; that prompt gave it nothing worth sandboxing, so the measurement
  was wrong. Its own stats across 155 worker sessions report **4,401,021 bytes
  sandboxed against 485,148 returned and 1,249,024 tokens saved** — lumpy (median
  3,385 per active session, one session 1,095,981), earning its place by
  occasionally avoiding an enormous context load rather than shaving a steady
  percentage. caveman stays out: at ultra it produced **1,103 output tokens against
  927 without it**, plus ~8,500 input tokens of its own instructions, because it
  compresses assistant prose while exempting code — and a worker's output is
  mostly tool calls and code.
- **Gap-churn: fix beads no longer spawn fix beads.** The recursion guard allowed
  three levels, which is how run H produced titles like
  `Fix: Fix: Fix: NFR-001: The impact path stays cheap and offline` and **158 fix
  beads against 18 real tasks**. A fix bead exists to redo work that failed; if the
  fix itself fails, another copy re-runs identical instructions through the
  mechanism that just failed, burning a worker slot and provider quota per attempt
  while burying the original gap. Capped at one fix bead per original
  (`maxFixNesting = 1`).
  Refusal is now also **labelled** (`fix-recursion-capped`). Previously the refusal
  path returned silently, so the bead closed as failed and its dependents unblocked
  — the exact regression `close_blocked_on_failed` exists to prevent — with no
  record of why no fix bead appeared.
- `compliance_cycle.max_cycles` defaults to **2**, down from 5, in both
  `DefaultConfig` and the runtime fallback. Each cycle re-runs the analyzer,
  re-adjudicates and decomposes surviving gaps into a fresh bead chain, so cost
  multiplies rather than adds. Run H spent 5 gap-to-prd invocations with the later
  cycles re-litigating findings rather than discovering new ones. Two still covers
  analyze → fix → verify.
- **A hung provider request no longer costs a worker ten minutes.** In rpc mode
  the activity timeout is now 4 minutes rather than 10, because the two protocols
  emit at very different densities: text mode streams prose and a long tool run
  can legitimately go quiet, while rpc emits an event for every step
  (`tool_execution_*`, `message_update`, `extension_ui_request`), so silence means
  the request has hung. Measured 2026-07-28: a worker logged "prompt sent over rpc"
  at 07:50:09 and emitted nothing until the timeout fired at 08:00:29, while the
  next attempt received a 429 in five seconds — and Pi's own retry backoff is
  `delayMs: 2000`, so a ten-minute gap is never Pi waiting politely. Waiting it
  out wasted a worker slot; across five slots the run stopped closing beads
  entirely (0 in 45 minutes, against 8 in the preceding text-mode window).
  `HYDRA_WORKER_ACTIVITY_TIMEOUT` still overrides both.
- **A transient provider blip no longer freezes the fleet for fifteen minutes.**
  Kimi returns "The engine is currently overloaded, please try again later" as a
  429 with `type: rate_limit_error`. That was classified as a rate limit (exit 2),
  fed the circuit breaker, and three of them inside ten minutes opened the circuit
  for fifteen — and because `fallback_provider` equals the primary there was
  nothing to fail over to, so the spawn gate paused every worker
  (`skipping spawns for 11m32s`) over an error whose own message says to retry
  shortly. Pi already retries it internally. Capacity errors now classify as
  **exit 7**: retryable infrastructure (so no fix bead is spawned) that never
  touches the provider circuit. Genuine throughput rate limits still classify as
  2, and quota exhaustion still wins with 6.
- The breaker no longer claims a failover it cannot perform. With
  `fallback_provider` equal to the primary it logged "failing over to
  kimi-coding" while every worker was in fact paused; it now says so and points at
  the config.

### Changed
- **Default Pi extension allowlist is `[rtk]`, not all three.** Measured over
  three sequential trials of one prompt: no extensions ~12,271 tokens/turn, rtk
  ~16,360 (+33%), rtk + caveman + context-mode ~26,679 (+117%). context-mode is
  tool-call-shaped by design — one dogfood run logged 1,292 `ctx_*` calls against
  85 rtk rewrites — and every tool call is another request, which raises exposure
  to provider capacity errors under five concurrent workers. rtk is kept because
  it compacts tool *output*: bounded cost, saving that scales with command noise.
  caveman and context-mode are plausible but unmeasured; enable per project with
  `extensions: [rtk, caveman]` once there is data rather than fleet-wide.
- **Workers ran with zero Pi extensions, in both protocol modes.** The worker
  exported `PI_CODING_AGENT_DIR=<config_dir>/agent`, but on the deployed layout
  the package set lives in `<config_dir>` itself
  (`pi-worker/settings.json` + `pi-worker/npm/node_modules`, 239 packages);
  `<config_dir>/agent` holds neither. Verified on the box with the same binary and
  only that variable changed: `<cfg>/agent` -> `pi list` reports "No packages
  installed", `<cfg>` -> 13 packages including `pi-rtk-optimizer@0.9.0`,
  `caveman-milk-pi`, `context-mode`, `@aliou/pi-guardrails`. So no rtk command
  rewriting, no context-mode, no caveman, no guardrails — for every bead Hydra has
  ever run. It also explains why rtk's own savings counter on that host had not
  moved since 06-26 despite workers making nearly all the tool calls.
  `resolvePiAgentDir` now probes for the directory that actually holds the package
  set, preferring `<cfg>/agent` so the developer-machine layout
  (`~/.pi/agent/...`) is unchanged, and falling back to the historical value when
  neither looks like a package root.
- Hindsight recall no longer filters by bead type, which had silently switched
  off the recall half of the learning loop. Hindsight's `types` are memory kinds
  (`experience`, `observation`, `world`); bead types (`compliance`, TDD phases)
  match none of them. The old code sent the bead type under the singular key
  `type`, which the API does not accept, so it was dropped and recall stayed
  unfiltered — correcting the key to `types` in this same branch activated a
  filter that had never actually run. Measured on the live bank: no filter → 12
  results, `types:["compliance"]` → 0. Every worker after the mid-run deploy went
  from `Retrieved 5 relevant memories` to `No relevant memories found`, with no
  error logged anywhere. Caught by comparing pre/post-deploy worker logs, not by
  a test — so there is now a test asserting the request carries no `types` key.
- Quota exhaustion now pauses the fleet instead of hammering the provider.
  Three compounding defects: (1) `RecordRateLimit` only opened a circuit when a
  fallback was configured; (2) an open circuit merely changed provider
  *selection* — `chooseLocked` ends with "nothing better to offer than the
  primary", so it never gated spawning; (3) the deployed config sets
  `fallback_provider` equal to the primary, making failover a no-op by
  construction. Net effect observed 2026-07-27: the fleet spawned 2-3
  workers/minute for hours against a dead quota, each spawn a full prompt build
  plus a rejected call. Quota now classifies as its own exit code (6, still
  infrastructure so no fix beads), opens the circuit on the FIRST occurrence,
  and the scheduler consults `ProviderBreaker.ShouldPause()` before spawning —
  a 30-minute cooldown, because a billing limit clears on cycle rollover, not
  in seconds like a 429.
- Hindsight recall was sending parameters the API does not accept. The client
  posted `limit`, `min_score`, `days_back` and `bank_id`, but the recall
  endpoint's schema (v0.6.2) accepts only `query`, `types`, `budget`,
  `max_tokens`, `trace`, `query_timestamp`, `include`, `tags`, `tags_match`,
  `tag_groups` — so all four were silently dropped. Every recall therefore
  returned the **entire** memory bank (measured: 49 memories, ~80KB) and the
  client kept the first few with **no quality filtering**, while
  `min_score: 0.7` sat in the config implying a filter that never ran. The
  request now uses `max_tokens` (the API's real size control), configurable via
  `intelligence.hindsight.max_tokens` and `HYDRA_HISTORIESIGHT_MAX_TOKENS`,
  default 800. Measured against the live API: **80,517 B → 15,736 B per
  recall**. A test pins the request body so the phantom fields cannot return.
- The intelligence stack told every agent to run a binary that does not exist.
  `AGENTS.md` and the `code-discovery` / `code-search-and-intelligence` skills
  referenced `hydra-llm-tldr` (27 occurrences); the real binary is `llm-tldr`
  (`/usr/local/bin/llm-tldr`, on the daemon user's PATH). `TLDRIndexer` had the
  same wrong name as its fallback when no path is configured — and production
  config sets no path, so the default was always used. Observed in a run G
  worker: "`hydra-llm-tldr search` / `impact` failed with `EACCES` / function not
  found; I relied on `grep`", i.e. the agent silently degraded to a worse tool
  after burning turns on a command that could never work.
- The prd-lint **preflight was missing from the repository**: PR #72 shipped
  `tools/prd-lint` but not the `cli.mjs` wiring that invokes it, so a fresh
  clone or deploy silently had no PRD gate. Cause: the staging command
  addressed the skill through `pi-config/skills/…`, which is a **symlink** to
  `.agents/skills/`, and `git add` refuses paths "beyond a symbolic link" —
  the fatal error was swallowed by a `2>/dev/null`, so the omission looked
  like success. Now committed via the real path, with `preflight.mjs` and its
  test suite.
- `tools/hydra-compliance-analyzer/main_test.go` (the analyzer-wiring test from
  the security-scan PRD) built `security-scan` by package path from the repo
  root, but that tool is its own module — the test panicked with "main module
  does not contain package". It now builds from inside the module directory
  with an absolute output path.
- Orphan bead recovery, three defects in one path: it ran **after** the
  cleanup cycle's `len(trackedWorkers) == 0` early return, so it was
  unreachable in exactly the situation it exists for (a 65-minute dead stall in
  dogfood run D, beads reopened by hand); its documented 5-minute grace was
  read into a log string but never compared against anything; and it
  force-**closed** incomplete work with `--force`, marking undone beads done and
  unblocking dependents — the failure class the verified-close gate exists to
  prevent. Recovery now runs before the early return, skips beads younger than
  the grace (and beads with no `UpdatedAt`, rather than acting blind), and
  **reopens** via `UpdateBeadStatus(StatusOpen)` — the same path
  `rejectClose` uses — so the scheduler re-attempts the work.
  *Authored by a Hydra dogfood run.*
- Quota exhaustion is now classified as **infrastructure**, not a code
  failure. Kimi returns an exhausted billing quota as HTTP 403 with a
  `permission_error`/`access_terminated_error` payload, which the existing
  rate-limit markers (`429`, `rate_limit`) did not match — so each affected
  bead was retried 4x and then spawned a fix bead, and every one of those
  retries was another rejected call against the quota that was already gone.
  The failure consumed the quota it was failing on. `isQuotaError` now drives
  both `shouldRetry` (fail fast) and `classifyFailure` (exit code 2 =
  infrastructure → no fix bead, scheduler backoff). Observed live 2026-07-27:
  36 useless fix beads from one exhausted quota.
- `tools/gap-to-prd` now emits compiler-contract-compliant mini-PRDs:
  same-line `Target Files` (the old bullet list was unparseable by
  task-breakdown, silently producing targetless gap beads), `Type`/`Priority`
  (P0 for critical)/`Test Command`/`Build Command` per FR, deterministic FR
  ordering with correct `Aggregates` references, and a NEEDS-HUMAN-TRIAGE
  section for targetless gaps. Golden-file test embeds the parser's regexes.
  *Authored by a Hydra dogfood run.*
- Verified-close gate now inspects the bead's **worktree**, not the shared
  checkout, when worktree isolation is active. The diff + scope gates read
  `ResolveRepoPath` (the main checkout, which stays clean until a chain
  integrates), so every worktree-isolated bead whose worker wrote to its
  worktree was falsely rejected as `empty-diff` and cycled through retries.
  Root-caused live: worker wrote `activity_log.go` into its worktree, gate
  saw the clean checkout, rejected it 21×. Now uses
  `WorktreeManager.ExistingWorktree` (non-creating) to check the real tree.
- Anti-consultant guard: implementation beads' prompts carry a DELIVERABLE
  CONTRACT (edits must exist on disk; plans are rejected as empty diffs) —
  doctrine-tuned workers had begun honestly delivering plans instead of code.
- Eval metrics accuracy: accepted `[CloseEvent]` lines now carry the real
  validation-attempt count instead of a hardcoded `0` (one-shot-rate was
  degenerate), and `hydra-eval --since <time>` scopes all daemon-log parsing
  to the current run instead of everything since log rotation.
  *Authored by a Hydra dogfood run.*
- Sticky compliance approval: an approved PRD no longer re-runs the analyzer
  every poll until max_cycles (`.hydra/compliance-approved` sentinel,
  restart-durable).
- Hindsight recall: the live recall API ignores `limit`/`min_score` and sends
  no scores — results are now truncated client-side to the server-ranked
  top-k (previously ~65 unranked memories were injected into every worker
  prompt) and the phantom `score: 0.00` display is gone.
- Worker Pi startup: `--offline` flag + jittered startup retries for the
  concurrent provider-list fetch flake; activity timeout lowered 30m → 10m
  (tunable via `HYDRA_WORKER_ACTIVITY_TIMEOUT`) so hung workers recycle
  quickly (PR #49).
- Worker skills are stack-scoped to the project language (a Go project loads
  8 skills instead of 16) (PR #49).
- Worktrees are rooted under `$HYDRA_HOME` (daemon-owned) instead of the
  project's parent directory, which was not writable for per-user projects
  (PR #49).
- Multi-user discovery no longer skips an entire user when their git token is
  unreadable; `br` runs in the repo subdirectory for multi-repo projects; one
  project's `br`/`bv` failure no longer aborts the whole scheduler tick
  (PR #47).

### Documentation
- `docs/architecture/system-loop.md`: process-flow map of Hydra's three
  nested control loops (build / learning / meta) with a loop-closure audit —
  automated vs planned vs human-by-design edges marked, plus the known
  verdict-feedback leak and measured compounding evidence.
- `docs/roadmap/self-improvement-loop.md`: phased roadmap for Hydra's
  self-observation/diagnosis/fix loop (Observer → Proposer → Actor) with
  safety invariants and dogfooding plan.
- Discovery report for FR-001 Phase-Model Config: `docs/discovery/FR-001-phase-model-config.md` documents requirements analysis, derived data model, ambiguity resolutions, and verification results. *Authored by a Hydra dogfood run.*
- Documented `agents.pi.phase_models` in `hydra-config.yaml.example` and `docs/operations/configuration-guide-20260309.md` with per-phase provider/model override examples and circuit-breaker precedence notes. *Authored by a Hydra dogfood run.*
- `docs/hydra-analysis-20260712iv.md`: improvement master plan (phases,
  work orders, eval harness definition).
- Dogfood PRDs: sticky approval, Target-Files scope gate, worktree
  integration reliability.
