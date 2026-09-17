# Host setup: bare machine to running Hydra fleet

**Status:** current as of 2026-09-02, verified against `swyftflo-hermes-vm-01`
**Audience:** humans and AI agents provisioning or rebuilding a Hydra host
**Scope:** everything outside the source tree that Hydra needs to run. For
config-file semantics see `hydra-config.yaml.example`; for what each runtime
directory is for see `docs/architecture/filesystem-layout.md`; for the
shared-Pi-config rule see `docs/architecture/pi-config.md`. This document is
the umbrella: follow it top to bottom and nothing important is missed.

> **Keeping this file current is mandatory.** AGENTS.md (Documentation &
> Changelog Discipline, rule 4) requires every change that alters host
> requirements — systemd unit/drop-ins, paths, binaries, OS packages, Pi
> config, `/etc/hydra/*`, resilience tooling, deploy procedure — to update
> this file in the same commit/PR.

Every step lists its **verification** — do not move on until it passes.
Steps marked *(production)* are hard requirements for a real fleet but can be
skipped for a throwaway test box.

---

## 0. What you are building

```
bare host
  └─ hydra daemon (systemd, user `hydra`)
       ├─ spawns worker processes (one per bead)
       │    └─ each worker drives `pi` (the coding agent) against a
       │       per-bead git worktree of a target project
       ├─ tracks work in beads (`br` CLI) per project repo
       ├─ coordinates via Pan (shepherd LLM) + a worker registry (sqlite)
       └─ integrates finished work back to the project's main branch
```

If any layer below is missing, the failure mode is usually **silent**
(workers spawn and die in seconds, beads close-failed for reasons that look
like task difficulty). The verification steps exist because every one of
these has actually happened.

## 1. OS packages

Required: `git`, `node` + `npm` (Node 22+), `go` (1.24+), a Rust toolchain
(for `br`), `sqlite3`, `util-linux` (swap tools).

Production extras: `auditd` (§9), `acl` (`setfacl` for multi-user access).

> **Gotcha (this box):** root shells have a stripped PATH. `/sbin` tools
> (`swapon`, `sysctl`, `auditctl`, `augenrules`) must be invoked by absolute
> path.

**Verify:** `go version`, `node --version`, `cargo --version`,
`sqlite3 --version`, `/sbin/swapon --version` all print.

## 2. Users, groups, permissions

- User `hydra` (system user; home = `$HYDRA_HOME` = `/opt/dev-tools/var/hydra`).
- Group `pi`: members can read the shared Pi config. `hydra` must be in it.
- Group `beads`: workers write bead stores in project repos; the systemd unit
  sets `Group=beads` + `UMask=0007` so files are group-writable.
- Each project-owner's home must be traversable and the project tree
  group-accessible:

```bash
chmod o+x /home/<user>
setfacl -R -m g:pi:rwX /home/<user>/projects
```

**Verify:** `id hydra` shows `pi` and `beads` groups;
`sudo -u hydra ls /home/<user>/projects` succeeds.

## 3. Directory layout

Create the three-way split (code / shared config / runtime state) exactly as
documented in `docs/architecture/filesystem-layout.md`:

```
/opt/dev-tools/components/hydra/   code (git checkout) — never written at runtime
/opt/dev-tools/config/pi/          shared Pi config (git submodule)
/opt/dev-tools/var/hydra/          runtime state = $HYDRA_HOME = hydra's home
/var/log/hydra/                    daemon logs (written by systemd redirection)
```

Runtime state must NOT live inside the code checkout — it is a git working
tree, and cleanup commands there can delete live worktrees and the run lock.

**Verify:** `scripts/verify-deploy.sh` passes its path checks (run after
deploy, §10).

## 4. Code, build, and helper binaries

```bash
cd /opt/dev-tools/components/hydra
git checkout development
go build -o bin/hydra ./cmd/hydra
go build -o bin/hydra-worker ./cmd/worker
```

Build the beads CLI and viewer from the infra monorepo:

```bash
cd /opt/dev-tools/components/infra/services/beads-rust/beads_rust
cargo build --release            # produces target/release/br
cd ../../../beads-viewer/beads_viewer && cargo build --release   # bv
```

**Verify:** `br --version`, `bv --version` (via the unit's PATH, §7).

## 5. The `pi` coding agent

Hydra workers exec `/usr/bin/pi` (a symlink into the global npm package):

```bash
npm install -g @earendil-works/pi-coding-agent
ls -la /usr/bin/pi   # -> ../lib/node_modules/@earendil-works/pi-coding-agent/dist/cli.js
```

**Verify:** `sudo -u hydra pi --version` prints. A missing/dangling pi is the
single most confusing fleet failure (workers exit in seconds with
`fork/exec /usr/bin/pi: no such file or directory`) — see §9 for the watch
that catches whatever replaces it.

> **Never let hydra run `npm install -g` itself.** npm rewrites
> `/usr/lib/node_modules` and the `/usr/bin` symlinks during a global install;
> a worker spawning `/usr/bin/pi` mid-swap dies with ENOENT (every pi-ENOENT
> burst 2026-08-11→09 was hydra's own hourly LSP install — auditd
> `pi_nodemodules` watch). Hydra installs LSP servers with
> `npm install -g --prefix $HYDRA_HOME/npm` (`internal/pi/lsp.go`) and
> resolves their binaries from that prefix first.

## 6. Shared Pi config (one directory, read-only to hydra)

The Pi config is shared between workers and humans and **hydra must never
write to it**:

```yaml
# /etc/hydra/hydra.yaml
pi:
  config_dir: /opt/dev-tools/config/pi
  worker_config_dir: /opt/dev-tools/config/pi   # same value, deliberately
  manage_config: false                          # never let hydra edit it
```

Each human user's `~/.pi/agent` is a symlink to the same directory. Splitting
worker/human configs once drifted a generated copy to 239 npm packages
against the canonical 242 with nothing detecting it — do not reintroduce a
second config.

**Verify:** `scripts/verify-pi-config.sh` — asserts the two dirs match, it is
a Pi package root, every allowlisted extension resolves, each `~/.pi/agent`
points at it, and no retired copy is referenced.

Worker extension allowlist (`agents.pi.extensions` in `/etc/hydra/hydra.yaml`,
2026-09-14): `rtk`, `caveman`, `context-mode`, `hindsight`,
`pi-cache-optimizer`. Workers resolve each name from the shared config — npm
packages under `npm/node_modules/`, git packages under `git/<host>/<org>/` by
repo segment — so installing a new worker extension means `pi install
npm:<pkg>` (or a git package) into the shared config plus one allowlist line;
humans pick it up automatically from the same directory. Note:
`verify-pi-config.sh` only checks the npm path, so git-installed entries
(e.g. `hindsight`) report a false failure there.
`pi-cache-optimizer` (prompt/KV cache hit-rate optimizer, requires Pi 0.82+)
improves provider-side cache reuse for both interactive and worker sessions;
its per-instance stats shards live under `pi-cache-optimizer-stats.d/` in the
shared config.

## 6b. llm-router (provider proxy)

`/opt/llm-router` runs [llm-router](https://github.com/haulmatic-dev/llm-router)
as `llm-router.service` (system user `llm-router`, listens 127.0.0.1:9900,
state in `./data/router.db` under its cwd). It fronts the Kimi coding account
as a single local endpoint so worker traffic gets lane gating, per-account
budgets, affinity pins, and prefix-SHA logging without Hydra knowing account
details. Config `/opt/llm-router/config.json` (accounts/models, no keys);
keys in `/opt/llm-router/llm-router.env` (0600). Hydra workers call it via
the `kimi-router` provider in the shared `models.json` (baseUrl
`http://127.0.0.1:9900/direct`). Pan (MiniMax) stays direct. Two gotchas
cost hours: models.json per-model `baseUrl` SHADOWS the provider-level
`baseUrl`, and a models.json entry cannot reroute a pi built-in provider
(`kimi-coding`) because the built-in stream implementation keeps its own
baseUrl — a fresh provider id (`kimi-router`) is required. Also remove stale
`KIMI_CODE_API_BASE_URL`-style exports: pi ignores them.

## 6c. Log rotation and retention

Two mechanisms cover fleet logs (set 2026-09-15):

- **Hydra file logs** (`/var/log/hydra/*.log`, written via the unit's
  `StandardOutput`/`StandardError` append directives): rotated by
  `/etc/logrotate.d/hydra` — daily or at 100MB, 14 archives, gzip-compressed
  (`delaycompress` keeps the newest archive plain for grep), `copytruncate`
  (no daemon restart needed), **`maxage 365` deletes anything older than a
  year**. Per-bead worker logs under `/opt/dev-tools/var/hydra/logs/workers/`
  age out after 7 days (write-once, rotate-in-place is wrong for them).
  `logrotate.timer` drives the schedule.
- **journald** (everything else, including **llm-router** — its `slog`
  output goes to stderr/journald, not a file): bounded by
  `/etc/systemd/journald.conf.d/99-size-retention.conf` —
  `SystemMaxUse=500M`, `MaxRetentionSec=365day`. journald rotates/vacuums
  itself; no per-service config needed.

Verify: `journalctl --disk-usage`, `logrotate -d /etc/logrotate.d/hydra`,
`ls -la /var/log/hydra/`.

## 7. systemd unit + drop-ins

Install `scripts/hydra.service` per `docs/operations/SERVICE_SETUP.md`
(canonical unit details live there). The parts that bite if wrong:

- `User=hydra`, `WorkingDirectory=/opt/dev-tools/var/hydra`
- `Environment="HYDRA_HOME=/opt/dev-tools/var/hydra"` — workers and tests
  resolve all runtime state from this. **Never let tests or workers inherit a
  real `$HYDRA_HOME`** (see `TestMain` in `internal/core`).
- `Environment="PATH=..."` must include the `br`/`bv` directories and
  `/opt/dev-tools/var/hydra/bin` last.
- `EnvironmentFile=/etc/hydra/api-keys.env` — provider keys, systemd format
  (`KEY=value`, no `export`):
  ```
  MINIMAX_API_KEY=...
  KIMI_API_KEY=...
  HINDSIGHT_API_LLM_API_KEY=...
  HINDSIGHT_API_LLM_BASE_URL=https://api.minimax.io/v1
  HINDSIGHT_API_LLM_MODEL=MiniMax-M2.7
  ```
- `No WatchdogSec` — the binary does not `sd_notify`; enabling the watchdog
  just SIGABRTs it every 60s.
- `LimitNPROC=8192`, no `LimitAS` — cgo libraries need the headroom.

Drop-ins in `/etc/systemd/system/hydra.service.d/`:

```ini
# override.conf — measured provider-queue latency (4m22s, 2026-08-11) killed
# turns at the 4m default; the watchdog horizon must exceed queue waits.
[Service]
Environment="HYDRA_WORKER_ACTIVITY_TIMEOUT=10m"
Environment="HINDSIGHT_ENABLED=true"
Environment="HINDSIGHT_BANK_ID=coding-brain"
```

```ini
# permissions.conf
[Service]
Group=beads
UMask=0007
```

**Verify:** `systemctl daemon-reload && systemctl start hydra`;
`systemctl status hydra` active; `curl -s localhost:8089/health`.

## 8. Live config: `/etc/hydra/hydra.yaml`

The operating knobs table and proven values live in `AGENTS.md` §0 and the
`hydra-operator` skill. The settings that have caused production incidents
when wrong:

| Key | Why it matters |
|---|---|
| `overseer.max_workers` | Must not exceed the provider's real concurrency. A 2nd worker against a 1-lane provider causes 403 lease ping-pong. |
| `overseer.worker_timeout` | Active-time cap (parked excluded). 15m = zero completions on real tasks; 45m proven. |
| `overseer.max_bead_tokens` | Soft cap (15M) + hard backstop (30M). Budget is a signal for Pan, not an axe. The cap is evaluated against **fresh tokens** (`tokens_fresh` = fresh input + output); cache-read and cache-creation tokens count toward `tokens_so_far` for observability only. |
| `output.push` | Off = finished work never reaches origin. When the working branch has permanent branch protection, pair it with `output.pr_merge: true` — direct pushes can never succeed, and the PR route (`dogfood/<id>` branch + `gh pr create` + `gh pr merge --merge --admin`, MERGED-state verified) is the only path that lands work. |
| `pi.config_dir` | Wrong dir = 0 skills loaded, degraded generation (see §6). |
| `projects.list[].path` | Workers run `br`/gates inside these repos; permissions per §2. |
| `projects.multi_user_discovery` | Default `true`. Enables scanning `/home/*/.hydra/config.yaml` for per-user projects. Set to `false` only to run a purely config-listed fleet. |
| `projects.auto_discover_exclude` | Directory names or paths excluded from auto-discovery. Applies to both the explicit `projects.auto_discover` path walk and multi-user `type: multi` + `auto_discover: true` discovery. Use to silence non-Hydra directories (e.g. `demo`) or, operator-side, to keep a project like `webapp` out of discovery until its fix is deployed and verified live. |

### Multi-user project discovery

User projects are declared in `/home/<user>/.hydra/config.yaml`. A multi-repo
project is discovered automatically when the user entry uses `type: multi`
with `auto_discover: true`:

```yaml
projects:
  - name: webapp
    path: ~/projects/webapp
    type: multi
    auto_discover: true
```

Hydra scans `<path>` for subdirectories containing `.beads/beads.db`. For each
discovered repo, PrefixGuard writes `.beads/config.yaml` with
`issue_prefix: <username>-<reponame>` (lowercased and sanitized to
`[a-z0-9-]`) so `br create` bead IDs do not collide across users or across
projects that share a repo name. An explicit `issue_prefix` on the user
project entry overrides the generated value. Existing deliberate custom
prefixes are preserved with a warning; explicit config-listed projects in
`/etc/hydra/hydra.yaml` are not mutated.

**Verify:** `journalctl -u hydra | grep -E "MultiUserDiscovery|PrefixGuard"` shows
discovered users, repos, and any prefix overrides.

**Verify:** the daemon logs `[PiSetup] ✅ Pi skills ready` and workers log
`Added N skill flags` (N>0) on first spawn.

## 9. Host resilience *(production)*

### 9a. Swap — the OOM kill is real

Test runners (jest with 4G heaps) plus the daemon and workers have OOM-killed
the daemon twice at ~19.5G peak on a 22G host. A swapfile converts a SIGKILL
cascade into a slowdown:

```bash
fallocate -l 16G /swapfile && chmod 600 /swapfile
/sbin/mkswap /swapfile && /sbin/swapon /swapfile
echo "/swapfile none swap sw,nofail 0 0" >> /etc/fstab   # nofail: can't break boot
echo "vm.swappiness=10" > /etc/sysctl.d/99-hydra-swap.conf
echo 10 > /proc/sys/vm/swappiness
```

**Verify:** `/sbin/swapon --show` lists `/swapfile`;
`cat /proc/sys/vm/swappiness` = 10.

### 9b. auditd watch on the pi binary *(production)*

`/usr/bin/pi` has been intermittently un-exec-able in bursts since
2026-08-11 (replace-race signature; culprit still unknown at time of
writing). Since PR #196 workers survive this, but the watch names the
culprit:

```bash
apt-get install -y auditd
cat > /etc/audit/rules.d/pi-watch.rules << 'EOF'
-w /usr/bin -p wa -k pi_bin_dir
-w /usr/lib/node_modules -p wa -k pi_nodemodules
EOF
/sbin/augenrules --load
```

**Verify:** `touch /usr/bin/.probe && rm /usr/bin/.probe`, then
`/sbin/ausearch -k pi_bin_dir -ts recent` shows CREATE/DELETE with pid/ppid.

## 10. Deploy and upgrade procedure

The daemon runs `/opt/dev-tools/var/hydra/bin/hydra` — **not** whatever
`command -v hydra` finds. Deploying to the wrong path succeeds while changing
nothing.

```bash
# 1. Check for in-flight workers FIRST (parked/zero-token rows are exempt;
#    verify dead PIDs with kill -0 before trusting the registry)
sqlite3 -json /opt/dev-tools/var/hydra/.hydra/registry.db \
  "SELECT bead_id,state,pid FROM workers WHERE state IN ('working','parked')"

# 2. Build packages (not single files) and swap atomically — a plain cp onto
#    a running binary fails with "Text file busy"
go build -o /tmp/hydra.new ./cmd/hydra && go build -o /tmp/worker.new ./cmd/worker
cp /tmp/hydra.new /opt/dev-tools/var/hydra/bin/.hydra.new
cp /tmp/worker.new /opt/dev-tools/var/hydra/bin/.worker.new
mv -f /opt/dev-tools/var/hydra/bin/.hydra.new /opt/dev-tools/var/hydra/bin/hydra
mv -f /opt/dev-tools/var/hydra/bin/.worker.new /opt/dev-tools/var/hydra/bin/worker

# 3. Restart and prove the deploy
systemctl restart hydra
scripts/verify-deploy.sh
```

## 11. GitHub access (for PR-flow projects)

Workers integrate via worktrees; humans/agents push PRs using a token in
`~/.git-credentials`. The installed `gh` is old — use:

```bash
GH_TOKEN=$(grep -oP 'x-access-token:\K[^@]+' ~/.git-credentials | head -1) \
  gh pr create -R haulmatic-dev/hydra ...
```

## 12. End-to-end smoke

1. `systemctl status hydra` — active, `NRestarts` not climbing.
2. `curl -s localhost:8089/api/v1/stats` — `overseer_running: true`.
3. Create a trivial bead in a test project:
   `cd <project> && sudo -u hydra br create "smoke" --type task --priority 4 --silent`
4. Watch it: `tail -f /var/log/hydra/hydra-error.log` — expect
   `Spawning worker` → `Added N skill flags` (N>0) → `Executing Pi` →
   completion and a close event. A worker that dies in seconds with a
   fork/exec or 0-skill log means §5 or §6 is wrong.

## 13. Operational recall (day-2 commands)

```bash
# Daemon log (anchor greps on the daemon prefix; worker output is echoed as
# "[Worker:<bead>] [stderr] {...}" and contaminates unanchored counts —
# always count twice, with and without: grep -vE '^[0-9/]+ [0-9:]+ \[Worker:')
tail -f /var/log/hydra/hydra-error.log

# Worker forensics (rotated logs are gzipped)
ls -t /opt/dev-tools/var/hydra/sessions/<bead-id>/          # pi session jsonl
zcat /opt/dev-tools/var/hydra/logs/workers/<bead>-<pid>.log.1.gz | less

# Bead state — NEVER run br writes as root, never hand-edit issues.jsonl
cd <project> && sudo -u hydra br list
sudo -u hydra br show <bead-id>

# Worker registry (no CLI; check PID death with kill -0 before trusting rows)
sqlite3 -json /opt/dev-tools/var/hydra/.hydra/registry.db \
  "SELECT bead_id,state,tokens_so_far,pid FROM workers WHERE state IN ('working','parked')"

# pi-binary replace forensics
/sbin/ausearch -k pi_bin_dir -ts recent
/sbin/ausearch -k pi_nodemodules -ts recent

# grep -c exits 1 on no match: $(grep -c x f || echo 0) yields "0\n0" and
# breaks every [ ... -eq ] test on it. Use grep -c x f || true, or if/then.
```

## 14. Known time/skew note

Log timestamps on this host run ahead of UTC. When correlating with external
systems, compare durations, not absolute times.

---

## Related documents

- `docs/operations/SERVICE_SETUP.md` — canonical systemd unit commentary
- `docs/architecture/filesystem-layout.md` — what lives where and why
- `docs/architecture/pi-config.md` — the one-config rule and its failure history
- `docs/architecture/shepherd.md` — event stream, Pan, fix-bead lifecycle
- `AGENTS.md` §0–§5 — operating knobs, E2E test config, dogfood discipline
- Skill `hydra-operator` — fleet operations runbook
