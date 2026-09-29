# implement handoff

## What changed (files + one line each)
- internal/triage/triage.go: PR/Group/Recommendation types, GroupByOverlap union-find (shared-file connected components, <2-PR groups dropped), CommentBody renderer with advice-only wording.
- internal/triage/gh.go: injectable GhRunner + DefaultGhRunner (fail-closed inside runner), CollectOpenDogfoodPRs (filters dogfood/* heads, per-PR fail-open file collection), PostComment, AddLabel.
- internal/triage/judge.go: JudgePrompt (strict JSON contract), ParseRecommendation (fence-tolerant), DefaultPiRunner.
- internal/triage/service.go: Service loop (ticker, Start/Stop), group triage (rate-limit check BEFORE judge call), 24h rate-limit state persisted to a JSON file, runPi (mirrors pan.RunPi, --no-extensions).
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
- Live verification on srv-telematics PR pile: NOT DONE — no valid GitHub token in the worker environment (`gh auth status` → "not logged into any GitHub hosts"; the stored app-install credential returns `HTTP 401: Bad credentials`), and no local dogfood/* branches exist to exercise grouping with real data. `hydra triage` + daemon wiring are ready for an operator run with a valid token.

## Open edges for the next phase
- Live run needs gh auth (operator token or fresh app install token): `hydra triage` against /etc/hydra/hydra.yaml after adding `triage:` config; expect pmgh/5gq-class overlap groups.
- Judge prompt does not yet include "failure context" (CI/merge-conflict state) — bead text mentions it; mergeable field could be added to the collection JSON cheaply.
- Triage labels must pre-exist on the repo or label-add fails open (logged only); creating them automatically is deliberately out of scope.
- Rate-limit state grows unbounded (stale keys never pruned); harmless at current scale.
