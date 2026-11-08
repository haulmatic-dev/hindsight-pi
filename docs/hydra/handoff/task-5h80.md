# discovery handoff (task-5h80)

## What changed (files + one line each)
- scripts/mirror-health-slo.sh (new): FR-5 SLO checker — exits 0 iff event stream shows 0 `mirror_drift_detected` in last 24h AND ≥1 `mirror_audit_clean` per registered store; reads `$HYDRA_EVENTS_DIR` (default `/opt/dev-tools/var/hydra/events`), parses hydra.yaml `-?[[:space:]]*path:` keys (both list-dash and indented forms), store field falls back through `.fields.store // .store // .fields.store_dir // .store_dir`; `MIRROR_SLO_MAX_AGE_HOURS` window override; exit 2 on infra (missing tools/unreadable dir); read-only, never mutates stores.
- docs/architecture/shepherd.md: new §1b "Mirror health SLO check" — runbook line (after any direct-sqlite repair run the script), SLO semantics, env knobs, fail-closed-on-infra note.
- CHANGELOG.md: two Unreleased entries — Documentation (SLO check + shepherd docs) and Fixed (yaml path parsing bug: sub-key `path:` was invisible to dash-anchored matcher).

## Verification (commands run + pass/fail)
- `bash -n scripts/mirror-health-slo.sh` — SYNTAX-OK. PASS.
- Mock drift: fixture events dir with `mirror_drift_detected` + clean only for projA → `SLO-BREACH: store '.../projB' has no mirror_audit_clean event in the last 24h`, rc=1. PASS (drift/coverage detection works).
- Mock clean: clean events for both stores → `SLO-OK: 0 drift, 24h clean coverage for 2 store(s)`, rc=0. PASS.
- `grep -c "Mirror health SLO check" docs/architecture/shepherd.md` → 1. PASS.
- `git status --short` — 2 modified (CHANGELOG.md, shepherd.md) + script + unrelated untracked discovery doc from another strand.
- hindsight_search at bead start — tool unavailable (skip rule applied). graphify query — graph.json absent; rg fallback per protocol.

## Open edges for the next phase
- Event field names (`fields.store` vs top-level) are a defensive union until FR-2 GREEN pins the actual `mirror_audit_clean` payload — if GREEN uses a different field (e.g. `project`), update `scan_clean_stores` in the same change.
- The script greps `"type":"<event>"` literally — if EventSink ever reformats JSON spacing (non-compact), switch to `select(.type==...)` only.
- `scripts/mirror-health-slo.sh` depends on `jq` and `date -u` — host must keep both (both present on srv-data-harbour).
- FR-5 also names AGENTS.md operating-table documentation for the FR-2 knobs (`mirror_health.enabled`/`audit_interval`) — deferred to FR-2 GREEN because the knobs do not exist in code yet; documenting yaml knobs that cannot parse would mislead operators.
