# Hydra User Guide

> For developers using a shared Hydra daemon.
>
> 📖 Also see: [Admin Guide](admin-guide.md) — for running the daemon itself.

If your team runs a shared Hydra daemon, you can configure your own projects and git identity without needing admin access.

## Quick Start

Create `~/.hydra/config.yaml`:

```yaml
user:
  name: Alice Smith
  email: alice@example.com
git:
  provider: github
  username: alice-smith
  token_file: ~/.hydra/.github-token
projects:
  - name: my-api
    path: ~/projects/my-api
    type: single
```

Create your token file:

```bash
mkdir -p ~/.hydra
echo "ghp_xxxxxxxxxxxxxxxxxxxx" > ~/.hydra/.github-token
chmod 600 ~/.hydra/.github-token
```

That's it. The next time Hydra's scheduler runs, it will discover your project.

---

## Config Reference

### `user` — Your Identity

```yaml
user:
  name: Alice Smith      # Used for git commits
  email: alice@example.com
```

### `git` — Git Provider Credentials

```yaml
git:
  provider: github       # or gitlab
  username: alice-smith  # GitHub/GitLab username
  token: ghp_xxx         # Option 1: inline (least secure)
  token_env: GITHUB_PAT  # Option 2: read from env var
  token_file: ~/.hydra/.github-token  # Option 3: read from file (recommended)
```

**Token resolution order:** `token_file` → `token_env` → `token`. Only one is required.

**Recommended:** Use `token_file` with `chmod 600`.

### `projects` — Your Projects

```yaml
projects:
  - name: my-api
    path: ~/projects/my-api
    type: single
    prd_path: docs/PRDs/current.md
    branch: main

  - name: my-platform
    path: ~/projects/my-platform
    type: multi
    # repos auto-discovered from subdirectories containing .beads/beads.db
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `name` | ✅ | — | Project identifier |
| `path` | ✅ | — | Absolute or `~/` path |
| `type` | ✅ | — | `single` or `multi` |
| `repos` | ❌ | auto | For `multi`: explicit repo list |
| `prd_path` | ❌ | `docs/PRDs/current.md` | Path to current PRD |
| `branch` | ❌ | `main` | Default branch |

### Single-Repo Project (`type: single`)

One repo with beads in `.beads/`.

### Multi-Repo Project (`type: multi`)

Auto-discovers repos from subdirectories containing `.beads/beads.db`. If you want explicit control:

```yaml
projects:
  - name: my-platform
    path: ~/projects/my-platform
    type: multi
    repos:
      - name: frontend
        path: frontend
      - name: backend
        path: backend
```

---

## How It Works

1. You write a PRD in `docs/PRDs/current.md`
2. Run **task-breakdown** to generate beads
3. Close the **approval gate** bead when ready
4. Hydra (running as a system service) discovers your project from `~/.hydra/config.yaml`
5. Hydra schedules workers — Pi agents execute beads and write code
6. Code is committed in your name using your PAT

---

## FAQ

**Q: Can I use the same repo as someone else?**
A: Yes. Alice and Bob can each have their own copy in `~/projects/repo/`. Hydra tracks them as separate projects.

**Q: What if I don't set a token?**
A: Hydra will skip your projects with a log warning. Workers will still run but won't be able to push.

**Q: Can I use a GitHub App instead of PAT?**
A: Not yet. PAT is the supported method.

**Q: Where do I put the config?**
A: Exactly at `~/.hydra/config.yaml`. Hydra scans `/home/*/.hydra/config.yaml` every startup.

**Q: Do I configure Pi?**
A: No. Pi is shared system infrastructure. The admin configures the Pi installation.
