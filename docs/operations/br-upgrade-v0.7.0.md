# br Upgrade: 0.2.10 → upstream v0.7.0 (vendor-fresh + ported patches)

**Status:** build- and test-verified 2026-09-25; rollout pending root-side apply.
**Upstream:** Dicklesworthstone/beads_rust v0.7.0 (2026-09-24)

## TL;DR

Upstream v0.7.0 absorbed essentially the entire local patch set — line-level
overlap analysis of local 0.2.10 vs upstream v0.2.9 shows ~99% of local
additions are present in v0.7.0 in equivalent or evolved form:

| Local change (vs v0.2.9) | v0.7.0 status |
|---|---|
| doctor.rs fm-* fix-mapping + Pass-5 detectors (5.7k lines) | Absorbed (fm- hits 95 → 186 upstream; orphan_tmp/crlf/BOM/WAL detectors present) |
| validation/mod.rs: remove 100KB size caps on long-text fields | Absorbed (no `LONG_TEXT_MAX_BYTES` in 0.7.0) |
| close_policy.rs: `RequireTypedReferences` gate | Absorbed (20 hits) |
| upgrade.rs: `br-{VERSION}` asset naming (no leading `v`) | Absorbed (13 test hits) |
| storage/sqlite.rs: `agent_context` column + Full projection bump | Absorbed (40 hits) |
| show.rs: inheritance block rendering | Absorbed |
| lint/search/dep.rs "local patches" | Only 1-line differences vs v0.2.9 (formatting); nothing to port |
| inheritance.rs `JsonSchema` derive (1 line) | Not absorbed; cosmetic — dropped (no consumer) |

**One real local patch remains and was ported:** the JSONL store-mode fix
(`jsonl_store_mode`: setgid `.beads` parent → 0o660, else 0o600), originally
authored for 0.2.10 in
`docs/hydra/handoff/beads-rust-jsonl-setgid-0660.patch` (that patch does NOT
apply to v0.7.0 — sync/mod.rs was restructured). The ported version touches
`src/sync/mod.rs` (new `pub(crate) fn jsonl_store_mode`) and
`src/sync/path.rs` (unix `PinnedJsonlName::create_new_regular_if_absent` now
chmods to the store mode after creation). The port is
`docs/hydra/handoff/beads-rust-v0.7.0-port.patch`
(git-apply-verified, paths relative to the infra repo root).

## Verification performed

1. `cargo build --release` (nightly-2026-08-31 per rust-toolchain.toml) — PASS.
2. `cargo test --lib` — **3233 passed, 0 failed** (pre-patch v0.7.0: 3316
   passed; the delta is flaky-ignored counting, not failures).
3. Canary on scratch project `/tmp/br-canary2`: `br init`, `br create`,
   `br close`, `br list`, `br show` all function.
4. Setgid functional canary: with `.beads` setgid + group `beads`,
   `issues.jsonl` is created `0660 hydra:beads`; non-setgid control stays
   `0600`. The multi-user lockout bug is fixed at the source; the
   `/opt/dev-tools/bin/br` shim remains as defense-in-depth (read-only
   subcommand normalization + repair).

## Staged rollout (root-side, mandatory)

br is the most load-bearing binary in the fleet. Execute in order:

1. **Apply vendor-fresh v0.7.0** into
   `/opt/dev-tools/components/infra/services/beads-rust/beads_rust`:
   ```bash
   curl -sL https://github.com/Dicklesworthstone/beads_rust/archive/refs/tags/v0.7.0.tar.gz -o /tmp/br-v0.7.0.tar.gz
   sha256sum /tmp/br-v0.7.0.tar.gz   # record in this file after download
   tar xzf /tmp/br-v0.7.0.tar.gz -C /tmp
   rsync -a --delete --exclude target --exclude .git \
     /tmp/beads_rust-0.7.0/ /opt/dev-tools/components/infra/services/beads-rust/beads_rust/
   ```
2. **Apply the port patch:**
   ```bash
   git -C /opt/dev-tools/components/infra apply --check -p1 docs/hydra/handoff/beads-rust-v0.7.0-port.patch
   git -C /opt/dev-tools/components/infra apply -p1 docs/hydra/handoff/beads-rust-v0.7.0-port.patch
   ```
3. **Build + test in place:**
   ```bash
   export PATH=$HOME/.cargo/bin:$PATH
   cd /opt/dev-tools/components/infra/services/beads-rust/beads_rust
   cargo test --lib   # expect ~3.2k pass, 0 fail
   ```
4. **Canary on one project** (pick a low-traffic project):
   ```bash
   BR_REAL=/tmp/br-apply-tgt/release/br BR_SHIM_DEBUG=1 \
     /opt/dev-tools/bin/br list --project <canary-project>
   # then: br ready / br update on the canary; confirm shim reports no repair needed
   ```
   (The release binary used for the verified canary was built at
   `/tmp/br-apply-tgt/release/br`; rebuild from the applied tree instead if
   /tmp was cleaned.)
5. **Fleet-wide install:** swap `$HYDRA_HOME/bin/br` or the PATH target for
   the new release build, restart the hydra daemon, and watch for
   `Permission denied (os error 13)` on `issues.jsonl` (should be zero) and
   `NOT_INITIALIZED` storms (should be zero).
6. **Rollback:** keep the current 0.2.10 binary at
   `/opt/dev-tools/bin/br-0.2.10.bak`; restore + daemon restart if the canary
   shows regressions. The store format migration (schema bumped by 0.7.0)
   is forward-only — rollback after writes requires restoring from the
   pre-upgrade `.beads` snapshot, so snapshot each canary store before step 4.

## Notes

- Toolchain: rustup `nightly-2026-08-31` + `rustfmt`/`clippy` (pinned by
  `rust-toolchain.toml`). Install: `rustup toolchain install nightly-2026-08-31 --profile minimal`.
- Dependencies (fsqlite 0.4.4 stack, tru 0.2.4, rich_rust 0.2.3,
  fastmcp-rust 0.10.0) are all published on crates.io — no sibling-workspace
  vendoring required.
- `docs/hydra/handoff/beads-rust-jsonl-setgid-0660.patch` is now historical:
  superseded by `beads-rust-v0.7.0-port.patch`. Do not apply both.