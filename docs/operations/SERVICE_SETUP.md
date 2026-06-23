# Hydra Runtime Service Setup

This document describes the runtime setup required to run Hydra as a
multi-user, multi-project service that survives reboots and crashes.

The hydra binary itself is configured by `hydra-config.yaml`. This file
covers the **OS-level** setup: the systemd unit, environment file,
beads CLI paths, and multi-user symlinks.

## systemd unit

`scripts/hydra.service` is the canonical unit. Install it with:

```bash
sudo cp scripts/hydra.service /etc/systemd/system/hydra.service
sudo systemctl daemon-reload
sudo systemctl enable hydra
sudo systemctl start hydra
```

The unit is opinionated about:

- **`User=hydra`** — must exist (see `add-hydra-user.sh`).
- **`WorkingDirectory=/opt/hydra`** — symlinks to the source tree.
- **`PATH`** — prepends the beads (`br`) and beads-viewer (`bv`) binary
  directories so hydra workers can invoke them.
- **`EnvironmentFile=/etc/hydra/api-keys.env`** — sources the model API
  keys (e.g. `MINIMAX_API_KEY`, `KIMI_API_KEY`) so workers can call LLM
  providers. See below.
- **`LimitNPROC=8192`** — default 4096 is too low for cgo-based libraries
  hydra loads (libtorch via TLDR, gopls for Go LSP, etc).
- **No `LimitAS=`** — the default 2 GB virtual address cap is too low for
  hydra + TLDR + cgo libraries. Removing it lets the process grow as
  needed.
- **No `WatchdogSec=`** — the hydra binary does not implement systemd
  watchdog notifications (`sd_notify(WATCHDOG=1)`), so enabling the
  watchdog would just SIGABRT the process every 60 s. Re-enable when
  the binary supports it.
- **Restart=always** with `RestartSec=10` and `StartLimitBurst=3` —
  survives crashes, but won't loop forever.

## API keys environment file

`/etc/hydra/api-keys.env` is sourced by systemd and merged into the
process environment. Format is systemd EnvironmentFile format
(`KEY=value`, **no `export`**):

```
MINIMAX_API_KEY=sk-cp-...
KIMI_API_KEY=sk-kimi-...
HINDSIGHT_API_LLM_API_KEY=sk-cp-...
HINDSIGHT_API_LLM_BASE_URL=https://api.minimax.io/v1
HINDSIGHT_API_LLM_MODEL=MiniMax-M2.7
```

`scripts/hydra-service.sh` is the legacy bash-style file under
`/etc/profile.d/hydra-api-keys.sh` with `export` lines. The systemd
EnvironmentFile is a plain-key=value version for direct use by
`EnvironmentFile=`. Keep both in sync.

## Binaries

The unit runs `/opt/hydra/bin/hydra`. Create this directory and
symlink the binary so updates to the source tree are picked up
without re-installing:

```bash
sudo mkdir -p /opt/hydra/bin
sudo ln -sf /opt/dev-tools/components/hydra/bin/hydra /opt/hydra/bin/hydra
sudo ln -sf /opt/dev-tools/components/hydra/bin/hydra-worker /opt/hydra/bin/hydra-worker
# The worker spawner looks for a binary named `worker` (not `hydra-worker`).
sudo ln -sf /opt/dev-tools/components/hydra/bin/hydra-worker /opt/hydra/bin/worker
```

## Configuration

`/etc/hydra/hydra.yaml` is the live config. It is a symlink to the
source-tree `hydra-config.yaml`:

```bash
sudo mkdir -p /etc/hydra
sudo ln -sf /opt/dev-tools/components/hydra/hydra-config.yaml /etc/hydra/hydra.yaml
```

## Multi-user setup

`User=hydra` cannot read files in another user's home unless the
directory is traversable. For each project host user (e.g. `buddhi`):

```bash
# Allow `hydra` to traverse /home/<user> and read its projects.
chmod o+x /home/<user>
# Allow the `pi` group (which `hydra` belongs to) to read the project
# files. `hydra` runs as the `hydra` user but is a member of `pi`.
setfacl -R -m g:pi:rwX /home/<user>/projects
```

Alternatively, run `scripts/add-hydra-user.sh` (when checked in) to
onboard a new user with all the right groups and symlinks.

## Beads CLI

Hydra invokes `br` (beads) and `bv` (beads-viewer) via PATH. Install
them to:

```
/opt/dev-tools/components/infra/services/beads-rust/beads_rust/target/release/br
/opt/dev-tools/components/infra/services/beads-viewer/beads_viewer/bv
```

These are prepended to `Environment="PATH="` in the systemd unit. If
you install them elsewhere, update the unit.

## Logs

Hydra stdout/stderr are redirected to:

- `/var/log/hydra/hydra.log` — stdout
- `/var/log/hydra/hydra-error.log` — stderr (worker output, [Charon] logs, etc)

These are append-only. Tail them with `journalctl -u hydra -f` for
live output, or `tail -f /var/log/hydra/hydra-error.log` for worker
diagnostics.

## Health

```bash
systemctl status hydra
curl http://localhost:8089/health
curl http://localhost:8089/api/v1/stats
curl http://localhost:8089/api/v1/workers
```

The `stats` endpoint reports `active_workers`, `max_workers`,
`total_projects`, `uptime_seconds`, and `overseer_running`. The
`workers` endpoint shows live worker PIDs and bead IDs.
