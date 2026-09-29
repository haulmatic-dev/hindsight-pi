# PR Triage Pass (FR-030)

Periodic, Pan-adjacent LLM pass that triages a product repo's open `dogfood/*`
PRs when they overlap. Design principle (operator, 2026-09-29): intelligence
for classification and strategy, mechanical for contracts and safety — this
pass is **advice only**. It never merges and never closes; Tier 2 merge
authority stays human.

## What it does

Per product repo, each tick:

1. Collect open `dogfood/*` PRs (`gh pr list --state open`) and their target
   files (`gh pr view <n> --json files`). A PR whose file list cannot be read
   is skipped (fail-open per PR).
2. Group PRs into connected components of file overlap (union-find over
   shared paths). Groups with fewer than 2 PRs are skipped.
3. Ask the cheap judge model (pi headless, `--no-extensions`, GLM-class) for
   strict JSON: `merge_order`, `duplicates` (PR + duplicate-of + reason),
   `needs_human` (PR + one-line reason), `reasoning`.
4. Post one recommendation comment per PR in the group and add advisory
   labels: `triage:merge-first` on the first PR of the recommended order,
   `triage:duplicate` on flagged duplicates. Label failures fail open.
5. Rate limit: at most one triage comment per PR per 24h, keyed by repo+PR.
   State persists in `$HYDRA_HOME/.hydra/pr-triage-state.json` so restarts do
   not double-post. The rate limit is checked **before** the judge call, so a
   fully rate-limited pile burns zero model tokens.

## Configuration

```yaml
triage:
  enabled: true
  interval_seconds: 3600
  provider: kimi-router      # judge identity, separate from the worker fleet
  model: glm-5.3-flash
  thinking: ""
  api_key_name: MINIMAX_API_KEY
```

Disabled (default) or failing, the daemon runs exactly as before.

## Manual run

`hydra triage` loads the config, runs ONE pass over all configured projects
(multi-repo projects are expanded per repo), and exits. It shares the daemon's
rate-limit state file, so a manual run never double-posts within 24h.

## Failure posture

- `gh pr list` error → project skipped, logged, scan continues.
- Judge error or unparseable output → group skipped, no comment.
- Collection is fail-closed inside the gh runner (non-zero exit is an error,
  not an empty PR set) so a broken `gh` never silently reports "no overlap".
