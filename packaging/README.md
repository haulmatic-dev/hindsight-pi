# Hydra packaging — multi-user onboarding

The Hydra daemon runs as the `hydra` user and discovers per-user projects by
reading each user's `~/.hydra/config.yaml`. To reach that file the daemon must
be able to **traverse** the user's home directory. Home dirs created `0750`
(no `o+x`) block the daemon, and the user's projects are silently skipped.

These tools fix that permission class durably.

## `hydra-onboard <user>`

Grants the daemon the minimum access needed, idempotently (run as root):

- `u:hydra:--x` on the home dir — **traverse only** (the daemon cannot list or
  read the rest of the home).
- `u:hydra:rX` on `~/.hydra` (recursive) — read the current config and token.
- A **default ACL** on `~/.hydra` so files rewritten later (an editor saving
  `config.yaml`, a rotated token) stay daemon-readable. This is why the fix
  survives edits where a plain `chmod`/`chown` would not.

It verifies `hydra` can actually read the config before reporting success.

```
sudo hydra-onboard ajantha
```

## `hydra-onboard-sweep` + timer (hands-off)

`hydra-onboard-sweep` scans `/home/*/.hydra/config.yaml` and onboards any user
the daemon cannot yet read. The systemd timer runs it every 15 minutes, so a
**new user just drops a `~/.hydra/config.yaml` and is picked up within one
interval — no admin action**.

Install (root):

```
cp packaging/systemd/hydra-onboard-sweep.service /etc/systemd/system/
cp packaging/systemd/hydra-onboard-sweep.timer   /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now hydra-onboard-sweep.timer
```

The service `ExecStart` points at `/opt/dev-tools/components/hydra/packaging/hydra-onboard-sweep`;
adjust if Hydra is installed elsewhere.

## Diagnostics

When the daemon starts, multi-user discovery now logs a summary and, for any
user whose config exists but is unreadable, a loud actionable line:

```
[MultiUserDiscovery] BLOCKED ajantha: cannot reach /home/ajantha/.hydra/config.yaml (permission denied) — run: hydra-onboard ajantha
[MultiUserDiscovery] ⚠️  1 user(s) SKIPPED (config unreadable by daemon): ajantha — fix with: hydra-onboard <user> (or wait for the onboard sweep)
```

Environment overrides (for testing): `HYDRA_DAEMON_USER` (default `hydra`),
`HYDRA_HOME_BASE` (default `/home`).
