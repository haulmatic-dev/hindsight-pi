# bug handoff (bug-jfnm)

## What changed (files + one line each)
- internal/core/beads_perms_watchdog.go (new): `BeadsPermsWatchdog` — per-interval stat probe of every managed project's `.beads` store (single + multi-repo via `resolveRepoPath`); conforming store = stats only; drift repaired via `onboardBeadsPerms` (discovery onboarding); repair-impossible (daemon locked out) → typed `beads_store_locked` event (Reason `owner_locked`, remedy `packaging/hydra-normalize-beads <repo>` / `beads-fixer.service` in Detail), throttled once/10m/store; fail-open; inert when `beadsGroupGID < 0` or knob 0; repair seam injectable (`watchdogRepair`).
- internal/core/beads_perms_watchdog_test.go (new): 6 tests — repaired-no-event, locked-out emits typed throttled event (TTL expiry re-fire pinned), conforming store untouched (repair must-not-call), disabled/missing-store no-op, multi-repo per-dir dispatch.
- internal/core/config.go: `overseer.beads_perms_watchdog` duration knob, default 1m.
- internal/core/overseer.go + service_starter.go: watchdog field + Start/Stop wiring (same pattern as integrationSweep).
- packaging/install-br-shim.sh (new): re-shadows the packaging/br shim onto every PATH dir preceding the beads-rust release dir (root-run); exits 1 naming any still-raw target.
- hydra-config.yaml.example, docs/operations/host-setup.md (§8 watchdog + PATH-shadow tripwire), docs/architecture/shepherd.md (event row), CHANGELOG.md.

## Verification (commands run + pass/fail)
- A1 REPRO (raw br 0.2.10, /tmp scratch store, `br init` + `setfacl u:hydra:rw g:pi:rw`): after `br create`/`br update --remove-label x` → issues.jsonl `600`, ALL ACL entries wiped (getfacl grep empty). Defect EXISTS on current base. VERIFIED.
- A3: no pre-existing watchdog (rg; only discovery-time `onboardBeadsPerms`, silent fail-open). Shim (`packaging/br`, a3f14b22) exists but cannot act on owner PATH — regression root: `/opt/dev-tools/config/pi/bin/br` is the raw ELF (Oct 7), shadowing the shim on owner-session PATH; daemon unit PATH (`/opt/dev-tools/bin` first) is still shimmed.
- B1 post-fix: watchdog `checkStore` run against the repro store (600, ACL wiped) → repaired to `-rw-rw----` (0660), PASS (temp in-package test, removed after run).
- `go test ./internal/core/ -run TestWatchdog -count=1` — PASS (6/6).
- `go build ./...` — PASS. `go vet ./internal/core/` — PASS. `gofmt` on new files — clean (config.go/service_starter.go gofmt drift pre-existing on base, untouched).
- Commit verified via `git show --stat HEAD` (10 files, +503).

## Open edges for the next phase
- Option (a) upstream (br preserving ACLs on write) is in beads-rust, a separate repo — not implementable here; the shipped mechanism is (b) watchdog + shim re-shadowing. `beads-fixer.timer` (root, 60s) already covers the owner-locked case mechanically.
- OPERATOR ACTION (cannot run as uid 999): `sudo /opt/dev-tools/components/hydra/packaging/install-br-shim.sh` to re-shadow `/opt/dev-tools/config/pi/bin/br` (currently raw ELF, 17085800 B) — until then owner-session br writes can still re-lock stores.
- `hydra-onboard-sweep` service doc says it grants ~/.hydra ACLs; it does not repair .beads store contents on a 1m cadence — beads-fixer does, but with mode `2775` + `g:pi` only (not `user:hydra`, not `2770`). Harmless today (hydra ∈ pi) but diverges from the documented convention.
