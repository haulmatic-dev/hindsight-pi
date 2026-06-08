# Multi-User Hydra: Admin Guide

This guide covers running Hydra as a root/system service that discovers and schedules projects for multiple users.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│  Hydra Daemon (root / service account)                      │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  ServiceStarter.Start()                             │   │
│  │    ├─ ProjectScanner.Discover()  ← hydra.yaml       │   │
│  │    └─ MultiUserDiscovery.Discover() ← /home/*/.hydra │   │
│  │                                                       │   │
│  │  Scheduler.Tick()                                     │   │
│  │    ├─ Build DAG from all beads                        │   │
│  │    └─ Spawn workers for ready beads                   │   │
│  │                                                       │   │
│  │  WorkerSpawner.Spawn()                                │   │
│  │    └─ Resolve user from project path                  │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
   ┌─────────┐         ┌─────────┐           ┌─────────┐
   │ Alice's │         │  Bob's  │           │  Eve's  │
   │ project │         │ project │           │ project │
   └─────────┘         └─────────┘           └─────────┘
```

- **One daemon** runs as root (or a dedicated service user)
- **Many users** each have `~/.hydra/config.yaml`
- **No per-user Hydra process** — the daemon handles scheduling for everyone
- **Pi is shared** — all workers use the same Pi installation and API keys

---

## Installation

### 1. Install Hydra

Follow the standard Hydra installation. The daemon binary should be on the system PATH.

### 2. Ensure Pi is Installed

Pi must be available system-wide:

```bash
which pi  # should return /bin/pi or /usr/local/bin/pi
```

Pi configuration lives in a shared location (e.g. `/opt/pi` with `~/.pi` symlinked). See `docs/pi-setup.md` (if applicable) for Pi installation.

### 3. Ensure `br` CLI is Installed

The beads CLI must be accessible:

```bash
which br  # beads-rust binary
```

---

## Running the Daemon

### Option A: Direct (development / testing)

```bash
sudo hydra -config /etc/hydra/hydra.yaml
```

### Option B: systemd Service

```ini
# /etc/systemd/system/hydra.service
[Unit]
Description=Hydra Multi-User Bead Orchestrator
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/hydra -config /etc/hydra/hydra.yaml
Restart=always
RestartSec=10
User=root
# Required for reading /home/* directories
AmbientCapabilities=CAP_DAC_READ_SEARCH

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable hydra
sudo systemctl start hydra
sudo journalctl -u hydra -f
```

### Option C: Docker (if needed)

Mount `/home` as a read-only volume:

```yaml
# docker-compose.yml
services:
  hydra:
    image: hydra:latest
    volumes:
      - /etc/hydra:/etc/hydra:ro
      - /home:/home:ro
      - /opt/pi:/opt/pi:ro
    environment:
      - HYDRA_HOME=/opt/hydra
    cap_add:
      - DAC_READ_SEARCH
```

---

## System Configuration (`/etc/hydra/hydra.yaml`)

The system config is **sparse** — most configuration comes from users.

```yaml
overseer:
  max_workers: 10
  bead_discovery_interval: 30s
  default_retries: 3
  worker_timeout: 30m

api:
  enabled: true
  port: 8080

# Pi config (shared for all users)
agents:
  pi:
    provider: kimi-coding
    model: kimi-for-coding
    api_key_name: KIMI_API_KEY

# Compliance cycle (optional)
compliance_cycle:
  enabled: true
  max_cycles: 5
  poll_interval: 30s

# No projects section needed — discovery comes from /home/*/.hydra/config.yaml
```

**Do NOT put user projects in `/etc/hydra/hydra.yaml`.** They belong in each user's `~/.hydra/config.yaml`.

---

## User Onboarding

For each new user, send them:

```bash
mkdir -p ~/.hydra
cat > ~/.hydra/config.yaml <<'YAML'
user:
  name: Your Name
  email: your@email.com
git:
  provider: github
  username: your-github-username
  token_file: ~/.hydra/.github-token
projects:
  - name: my-project
    path: ~/projects/my-project
    type: single
YAML

# Create token file
echo "ghp_xxxxxxxxxxxxxxxxxxxx" > ~/.hydra/.github-token
chmod 600 ~/.hydra/.github-token
```

The user then:
1. Writes a PRD in `~/projects/my-project/docs/PRDs/current.md`
2. Runs task-breakdown to generate beads
3. Closes the approval gate when ready
4. Hydra discovers and processes their beads automatically

---

## Security Considerations

### Root reads user directories

Hydra runs as root and reads `/home/*/.hydra/config.yaml`. This requires `CAP_DAC_READ_SEARCH` (or running as root).

**Mitigation:** User token files should have `chmod 600`. The daemon reads them but does not expose them.

### Tokens in memory

User PATs are held in memory while the daemon runs. They are:
- Used to set `user.name`/`user.email` before commits
- Used to inject into remote URLs for push (then restored)
- Never logged in full

### No isolation between users

All workers run under the same Hydra process. There is no container/chroot isolation. If you need hard isolation, run separate Hydra instances per user.

---

## Monitoring

### Check discovered users

```bash
# From Hydra logs
journalctl -u hydra | grep "MultiUserDiscovery"
```

Expected output:
```
[MultiUserDiscovery] Found user: alice (2 projects)
[MultiUserDiscovery] Found user: bob (1 projects)
[MultiUserDiscovery] Total users: 2, total projects: 3
```

### Check worker attribution

```bash
journalctl -u hydra | grep "worker" | head -10
```

Expected output:
```
✅ worker FR-001: bead=FR-001 user=alice project=/home/alice/projects/api
```

---

## Troubleshooting

### "No user config found"

- Verify the file is at exactly `~/.hydra/config.yaml`
- Check that the home directory is readable by root: `sudo ls /home/username/.hydra/`

### "Token not found"

- Check `token_file` path is correct
- Verify permissions: `chmod 600 ~/.hydra/.github-token`

### "No repos found in multi project"

- Ensure subdirectories contain `.beads/beads.db`
- Run `br init` in each repo subdirectory

### Workers don't spawn

- Check `max_workers` limit isn't hit
- Verify approval gates are closed (workers won't start until approval)
- Check DAG has ready nodes: look for `[DAG] ReadyNodes:N` in logs

---

## Migrating from Single-User to Multi-User

1. Move your projects from `/etc/hydra/hydra.yaml` to `~/.hydra/config.yaml`
2. Add your git credentials to `~/.hydra/config.yaml`
3. Restart Hydra daemon
4. Verify discovery in logs

The system-configured projects (in `hydra.yaml`) still work alongside user-discovered projects.
