# Filesystem layout on the VPS

**Status:** current as of 2026-07-31
**Verified by:** `scripts/verify-deploy.sh`, `scripts/verify-pi-config.sh`

## The layout

Everything hydra owns lives under `/opt/dev-tools`, split by **what changes it**:

```
/opt/dev-tools/
├── components/hydra/        CODE — a git checkout. Never written to at runtime.
│   ├── bin/                 build outputs
│   ├── scripts/             deploy, verify, run-lock
│   └── eval/                dogfood + nightly matrix harnesses
├── config/pi/               SHARED CONFIG — a git submodule (pi-agent-config).
│                            Read by workers AND by humans via ~/.pi/agent.
│                            See docs/architecture/pi-config.md
└── var/hydra/               RUNTIME STATE — the hydra user's home ($HYDRA_HOME).
    ├── bin/                 the binaries systemd actually executes
    ├── logs/workers/        one log per bead, capped at 32 MB per stream
    ├── events/              structured daemon event stream (day files)
    ├── shepherd/            nightly testbed verdict
    ├── observer/backlog/    anomalies the observer wrote
    ├── .hydra-worktrees/    per-bead git worktrees
    ├── run.lock             "a run owns the daemon" advisory lock
    └── .pi/agent -> /opt/dev-tools/config/pi
```

`/var/log/hydra/` stays outside this tree — it holds the daemon's own stdout and
stderr, written by systemd (`StandardOutput=append:`), and is rotated by
`/etc/logrotate.d/hydra`.

## Why state is not inside the code checkout

`components/hydra` is a git working tree. Runtime state inside it means:

- state shows up as untracked files, so `git status` stops being readable — and
  on 2026-07-31 a `.hydra-events/` directory was accidentally **committed** into
  the repo because `DefaultEventsDir()` fell back to the current directory
- `git clean -fdx` deletes live worktrees, logs and the run lock
- a checkout switch can move files the daemon has open

State also must not sit in the code checkout's *parent* unguarded: `/opt/dev-tools`
is itself a git repo, so `var/` is listed in its `.gitignore`. If you add another
state directory, ignore it there too.

## `$HYDRA_HOME`

One variable determines the whole state tree. It is set in three places that must
agree:

| Where | Value |
|---|---|
| `hydra.service` | `Environment="HYDRA_HOME=/opt/dev-tools/var/hydra"` |
| the `hydra` user's home | `getent passwd hydra` → `/opt/dev-tools/var/hydra` |
| shell script defaults | `HYDRA_HOME="${HYDRA_HOME:-/opt/dev-tools/var/hydra}"` |

Consumers: `worktreeRoot()`, `DefaultEventsDir()`, `workersLogDir()`,
`run-lock.sh`, `testbed-gate.sh`, `testbed-matrix.sh`, `verify-deploy.sh`.

**In tests, `HYDRA_HOME` is forced to a temp directory** by `TestMain` in
`internal/core`. Without that, tests inherit the production value: the daemon
exports `HYDRA_HOME=/opt/dev-tools/var/hydra`, workers inherit it, and when a
worker ran this repo's own test suite every test that created a worktree
registered it in live state. That produced **10,251 dead worktree entries** by
2026-07-31, each named after a Go test function, which Charon then walked on
every orphan sweep.

## Binary paths — the trap

systemd runs `$HYDRA_HOME/bin/hydra`. That is **not** what `command -v hydra`
resolves to (`/usr/local/bin/hydra`), and a deploy that installs to the wrong one
succeeds while changing nothing — a full day of deploys was inert on 2026-07-30
for exactly this reason. Two rules:

1. `deploy.sh` builds to `$HYDRA_HOME/bin/`, and builds **packages** (`./cmd/hydra`),
   never single files — `go build cmd/worker/main.go` compiles `main.go` alone as
   `command-line-arguments` and silently ignores its siblings.
2. `scripts/verify-deploy.sh` asserts the *live* `/proc/<pid>/exe`, the binary's
   freshness, and a marker string from the change. Run it after every deploy;
   "the deploy printed success" is not evidence.

`/opt/hydra` remains as a **compatibility symlink** to `/opt/dev-tools/var/hydra`
during the transition, so anything still holding the old path keeps working
instead of failing silently. Remove it once a full run is clean.

## Disk discipline

State grows without bound unless something stops it. On 2026-07-31 `/opt/hydra`
was **51 GB**:

| What | Was | Cause | Fix |
|---|---|---|---|
| `logs/workers/` | 30 GB | 1,590 per-bead logs, single beads to 853 MB of pi stream JSON, `weekly` rotation against ~15 GB/day | 32 MB cap per stream in `streamWorkerOutput`, rotation now `daily` + `maxsize 64M` |
| `.cache/go-build` | 15 GB | Go build cache under the daemon home | regenerable — clear freely |
| `go/pkg/mod` | 4.9 GB | Go module cache | regenerable — clear freely |
| `.hydra-worktrees/` | 10,255 entries | tests writing to production `HYDRA_HOME` | `TestMain` isolation |
| `.npm/_cacache` | 736 MB | npm cache | regenerable — clear freely |

After cleanup: **1.3 GB**, disk 68% → 56%.

The three caches are safe to delete at any time; they rebuild on next use. The
worker logs are the forensic record `fix_cause.go` parses, so prefer aging them
out over deleting the recent ones.
