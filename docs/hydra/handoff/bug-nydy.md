# bug handoff (hydra-nydy)

## What changed (files + one line each)
- internal/core/prefix_guard.go: `guardRepo` no longer renames a store whose existing `issue_prefix` equals the repo basename unless the store's `.beads/config.yaml` really leaves it at br's default — new `storePrefixExplicit` reads the raw config and treats an uncommented `issue_prefix:` line as explicit (preserved, loud log); a missing config file = true default (rename allowed); an unreadable config preserves the namespace (fail-closed against mid-life rename).
- internal/core/prefix_guard_explicit_test.go (new): 5 tests — explicit-prefix-equals-basename preserved, commented default (br style, nipuna webapp store shape) renamed, missing config renamed, unreadable config preserved, `storePrefixExplicit` line-parsing matrix.
- CHANGELOG.md: Fixed entry under Unreleased.
- docs/operations/host-setup.md: bead-prefix onboarding section documents the new default-vs-explicit discriminator.

## Root cause (verified from live evidence)
- `/home/ajantha/projects/srv-jobfile/.beads/issues.jsonl`: 38 beads with prefix `srv-jobfile` (newest 2026-10-04T16:51Z) vs 11 with `ajantha-srv-jobfile` (from 2026-10-05T04:01Z) — mid-store prefix rename.
- `/var/log/hydra/hydra-error.log(.gz)`: `[PrefixGuard] Set issue_prefix=ajantha-srv-jobfile in /home/ajantha/projects/srv-jobfile` fired repeatedly 2026-10-04 16:22/16:38/16:53 — the guard treats `current == repo basename` as br's default and rewrites, and `br config get` flapped with exit 8 (WAL-poisoned store) between successful reads, so the rename re-fired on a store with established beads.
- Downstream split namespace: `fix_created {bead: ajantha-srv-jobfile-7db, original: srv-jobfile-upg}`; `[DAG] Dependency not found: srv-jobfile-4eg -> srv-jobfile-65l`; Pan-filed wedge beads (jm2/vbf/1xd, sources `srv-jobfile-5my`/`-65l`) all carry the renamed prefix.
- Discriminator: br writes defaults as a COMMENTED `# issue_prefix: <name>` line (verified in `/home/nipuna.j/projects/webapp/.beads/config.yaml`) while deliberate values are uncommented (`/home/ajantha/projects/webapp/.beads/config.yaml`). `br config get` resolves both identically, so the old guard could not tell them apart.

## Verification (commands run + pass/fail)
- `go build ./internal/core/` — PASS.
- `go vet ./internal/core/` — PASS (clean).
- `gofmt -l internal/core/prefix_guard.go internal/core/prefix_guard_explicit_test.go` — clean.
- `go test -c ./internal/core/` + binary `-test.run 'TestPrefixGuard|TestStorePrefixExplicit|TestMultiUserDiscovery'` — 22/22 PASS (11 pre-existing discovery/prefix tests + 5 new + matrix), incl. `TestMultiUserDiscovery_AutoDiscoverRepos` (defaults still namespaced) and `TestPrefixGuard_CustomPrefixPreserved`.

## Open edges for the next phase
- LIVE-STORE NOTE (verified 2026-10-06 11:45Z): the prefix restore attempted during this bead was REVERTED by the deployed (pre-fix) daemon — `[PrefixGuard] Set issue_prefix=ajantha-srv-jobfile in /home/ajantha/projects/srv-jobfile` at 11:45:17 exactly overwrote the manual `srv-jobfile` edit. Fighting the running binary is futile; the standing prefix on this store is `ajantha-srv-jobfile` (matches the 11 newest beads). The code fix makes the guard PRESERVE an explicit `ajantha-srv-jobfile` once the new binary deploys — flap ends either way; an operator wanting the legacy namespace can set it after deploy and it will stick.
- The srv-jobfile store remains unusable by br (`Sync conflict: automatic WAL index recovery already failed ... stage live-recovery ... Run 'br doctor migrate-schema recover'` on every command; earlier attempt returned `Schema version mismatch: expected 19, found 11` after recover). Operator must recover/migrate before any bead operation there; guard calls on it fail-open-skip meanwhile.
- The guard still runs every discovery tick and shells `br config get` per repo; on the WAL-poisoned srv-jobfile store that keeps failing (exit 8) — harmless now (fail-open skip), but a `br doctor migrate-schema recover` on that store is advisable separately.
- If a deliberate prefix is later desired to equal a NEW repo's basename, the operator must set it via the project's `issue_prefix` config field (guard respects `configured`).
