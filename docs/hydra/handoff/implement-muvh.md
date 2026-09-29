# implement handoff

## What changed (files + one line each)
- internal/triage/triage.go: PR/Group/Recommendation types, GroupByOverlap union-find (shared-file connected components, <2-PR groups dropped), CommentBody renderer with advice-only wording.
- internal/triage/gh.go: injectable GhRunner (args[0] = program) + DefaultGhRunner (fail-closed inside runner); collection + labels via `gh api` REST (NOT `gh pr list` — GraphQL 401s with fine-grained PATs); CollectOpenDogfoodPRs (dogfood/* filter, mergeable_state, per-PR fail-open), SlugFromRemoteURL, PostComment, AddLabel.
- internal/triage/judge.go: JudgePrompt (strict JSON contract + mergeable_state failure context), ParseRecommendation (fence-tolerant), DefaultPiRunner.
- internal/triage/service.go: Service loop (ticker, Start/Stop), group triage (rate-limit check BEFORE judge call), 24h rate-limit state persisted to a JSON file, DryRun flag (preview without posting), runPi (mirrors pan.RunPi, --no-extensions).
- internal/triage/triage_test.go: 12 tests — grouping (overlap, two groups, singles dropped, empty), collection (dogfood filter, per-PR fail-open), prompt/parse contract, advice-only comment body, RunOnce end-to-end with fake gh + rate-limit across runs, state reload across service instances.
- internal/core/config.go: TriageConfig (yaml `triage`: enabled, interval_seconds, provider, model, thinking, api_key_name) on Config + rawConfig, copied in both Load paths.
- internal/core/overseer.go: triageSvc field; constructed in NewOverseer only when Triage.Enabled, with lazy Projects func (reads o.projects at tick time, expands multi-repos via resolveRepoPath).
- internal/core/service_starter.go: starts/stops triageSvc after epic scanner.
- internal/core/registry_path.go: triageStatePath + exported TriageStatePath (HYDRA_HOME/.hydra/pr-triage-state.json).
- internal/core/repo_path.go: exported ResolveRepoPath mirror.
- cmd/hydra/triage.go + cmd/hydra/main.go: `hydra triage` one-shot subcommand (respects triage.enabled gate, shares daemon rate-limit state).
- docs/architecture/pr-triage.md: design, config, failure posture.
- CHANGELOG.md: Added entry under Unreleased.

## Verification (commands run + pass/fail)
- `go build ./...` — PASS.
- `go vet ./internal/triage/ ./cmd/hydra/ ./internal/core/` — PASS (clean).
- `go test ./internal/triage/ -count=1` — PASS (12 tests, 0.009s).
- `go test ./internal/core/ -count=1` — PASS (ok, 53.7s).
- LIVE VERIFIED 2026-09-29 against haulmatic-dev/srv-telematics (the real pile, 28 open dogfood PRs): token from `$HYDRA_HOME/.git-credentials` works on REST but not GraphQL (hence the gh-api switch). `hydra triage` (temp config, real run) posted recommendation comments to all 28 PRs — one overlap group of 28, merge order #309(5gq) first, 14 dupes flagged, `triage:duplicate` label confirmed applied on #313 via API. 24h rate-limit state now live in `$HYDRA_HOME/.hydra/pr-triage-state.json`, so a re-run within 24h is a no-op.

## Open edges for the next phase
- Daemon must run with a token that can comment: the GraphQL-broken fine-grained PAT in .git-credentials works because collection/labels use REST, but `PostComment` uses `gh pr comment` (GraphQL upstream — it worked this run; if it ever 401s, switch to `gh api repos/{o}/{r}/issues/{n}/comments`).
- 28 PRs formed ONE mega-group (shared CHANGELOG.md etc.); consider grouping cap or weight files by conflict relevance before the next run.
- Triage labels must pre-exist on the repo or label-add fails open (logged only); creating them automatically is deliberately out of scope.
- Rate-limit state grows unbounded (stale keys never pruned); harmless at current scale.
