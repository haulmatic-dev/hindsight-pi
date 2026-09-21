# Pi configuration: one directory, shared with humans

**Status:** current as of 2026-07-31
**Applies to:** the VPS (`178.104.241.196`) and any box running the hydra daemon
**Enforced by:** `scripts/verify-pi-config.sh`

## The rule

There is **one** Pi configuration directory. Hydra workers read it, human shells
read it, and hydra never writes to it.

```yaml
pi:
  config_dir: /opt/dev-tools/config/pi
  worker_config_dir: /opt/dev-tools/config/pi   # same value, deliberately
  manage_config: false                          # read-only
```

Every user reaches the same tree through a symlink:

```
/root/.pi/agent           -> /opt/dev-tools/config/pi
/home/<user>/.pi/agent    -> /opt/dev-tools/config/pi
/opt/dev-tools/var/hydra/.pi/agent      -> /opt/dev-tools/config/pi     # the daemon user
```

So a skill or extension installed once is available to the fleet and to whoever
is typing `pi` in a terminal. That is the entire point.

## What lives where

| Path | What it is | Shared? |
|---|---|---|
| `<cfg>/skills/` | 32 skills — the shared skill library | yes |
| `<cfg>/npm/node_modules/` | installed Pi extensions (13 packages) | yes |
| `<cfg>/settings.json` | provider/model defaults, package list | yes |
| `<cfg>/APPEND_SYSTEM.md` | system-prompt appendix (doctrine, output discipline) | yes |
| `<cfg>/models.json`, `keybindings.json` | shared preferences | yes |
| `<cfg>/sessions/` | per-session state, **namespaced by working directory** | partitioned |
| `<cfg>/agent/auth.json` | credentials — gitignored, never committed | local only |
| `<cfg>/models-store.json`, `trust.json`, `run-history.jsonl` | runtime state — gitignored | local only |

`<cfg>` is a **git repository** (`haulmatic-dev/pi-agent-config`), so shared
content is versioned and runtime state is ignored.

## Extensions are chosen, not discovered

Sharing the config does **not** mean workers load everything a human has
installed. The daemon selects a per-phase allowlist and the worker enforces it:

```
agents.pi.extensions:        [rtk, context-mode]        # default
agents.pi.phase_extensions:  {red: [...], green: [...]} # optional per-phase override
        ↓
worker builds:   --no-extensions  -e <abs>/pi-rtk-optimizer/...  -e <abs>/context-mode/...
```

`--no-extensions` disables discovery from `settings.json`; the explicit `-e`
paths still load. Measured on one identical turn: **48,315 tokens** to load every
installed extension against **12,059** for rtk alone. Themes, editor widgets and
subagent UI are unusable by a headless worker, so they are simply not passed.

Short names map to packages in `cmd/worker/pi_extensions.go` (`rtk` →
`pi-rtk-optimizer`, `caveman` → `@tomooshi/caveman-milk-pi`, …). An unresolvable
name is logged and skipped — a typo costs that extension, not the bead.

`sol-pi` (SoL-Pi pilot) needs no alias — the config may carry the package
name directly. When `sol-pi` is allowlisted and installed, the worker also
writes the conservative pilot config to `<worktree>/.pi/sol-pi.json` and
passes `--approve` for that run (SoL-Pi reads project config only under
project trust; rpc mode never prompts). Details and the A/B protocol:
`docs/operations/sol-pi-pilot.md`.

## `manage_config` — why hydra must not write here

`pi.manage_config` defaults to **false**, meaning the daemon treats the directory
as read-only. When true, startup will:

- create the pi-config directory structure
- install the Pi agent and LSP servers
- write `skills-installed.json`
- run a **skill updater on a timer**, refreshing skills in place

Against a directory hydra owns that is desirable. Against a directory shared with
human shells it means the daemon reshapes the team's environment as a side effect
of starting up, and the skill updater can overwrite a skill someone is editing.

Set `manage_config: true` only for a config directory hydra alone owns (a test
fixture, a dev box with no shared tree).

## History: why there were two directories

Until 2026-07-31 there were two trees, and workers used the second:

```
/opt/dev-tools/config/pi          <- humans (via ~/.pi/agent)
/opt/dev-tools/config/pi-worker   <- the worker fleet, an rsync copy
```

`scripts/setup-pi-worker.sh` generated the copy for two stated reasons, **both of
which had stopped being true**:

1. *"context-mode removed, so each worker starts ~5-10x faster."* The copy still
   listed `npm:context-mode@1.0.169`, so the edit was no longer applying. It is
   also moot: workers pass `--no-extensions`, which ignores the package list
   entirely.
2. *"Sessions are kept separate so concurrent workers don't fight over locks."*
   Pi namespaces `sessions/` by working directory (`--opt-outreach-workflow--`,
   `--home-ajantha--`), and every worker runs in its own git worktree, so session
   paths were already distinct.

Meanwhile the copy had drifted: **239 npm packages against the canonical 242**,
and nothing noticed, because nothing compared them. The only config file that
differed was `settings.json`, on one line — `lastChangelogVersion`, which Pi
rewrites itself. `APPEND_SYSTEM.md` and `auth.json` were byte-identical and the
skills trees were identical.

The copy was retired to `/opt/dev-tools/config/pi-worker.retired-20260731`, and
these dormant leftovers under the daemon home were moved to
`/opt/dev-tools/var/hydra/.retired-pi-config-20260731`:

| Retired | Why it was dead |
|---|---|
| `/opt/dev-tools/var/hydra/.agents/skills/` | 13 skill copies from Jun 22, 6 differing from central (central newer). Unreachable: `discoverLocalSkills` looks in `<parent of config_dir>/.agents/skills`, which does not exist. |
| `/opt/dev-tools/var/hydra/.pi/skills/` | symlinks into the above |
| `/opt/dev-tools/var/hydra/pi-config/`, `/opt/dev-tools/var/hydra/config/`, `/opt/dev-tools/var/hydra/.pi/hydra-config/` | empty |

## Verifying

```bash
scripts/verify-pi-config.sh
```

Checks, in order of what actually breaks:

1. `worker_config_dir == config_dir` — workers and shells agree
2. the directory is a Pi package root — otherwise Pi reports "No packages
   installed" and **every worker runs with zero extensions**
3. every allowlisted extension resolves under `npm/node_modules`
4. each `~/.pi/agent` points at the shared config
5. no retired copy is still referenced by `hydra.yaml`

Check 2 is not hypothetical. When `PI_CODING_AGENT_DIR` pointed one level too
deep, `pi list` reported "No packages installed" and workers ran with **no rtk,
no context-mode, no guardrails** — which is why rtk's savings counter had not
moved since 06-26 while workers were doing nearly all of hydra's tool calls. See
`cmd/worker/pi_agent_dir.go`.

## Known follow-up

A 72-character `kimi-coding` key is present in the history of
`haulmatic-dev/pi-agent-config` (`agent/auth.json`) and has been pushed to
`origin/main`. The repository is **private**, and the working copy has been
emptied to `{}` and the file untracked and ignored. **Rotating the key is the
only fix that revokes exposure**; a history purge is optional after that.
