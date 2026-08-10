# Project access model: how hydra, `br` and humans share a repo

**Status:** problem analysed, fix not yet implemented — 2026-08-10
**Applies to:** every repo listed in `/etc/hydra/hydra.yaml` `projects:`
**Related:** [`filesystem-layout.md`](filesystem-layout.md), [`pi-config.md`](pi-config.md)

## The requirement

Three parties write to the same project repo, and all three must keep working
without manual intervention:

| Party | Runs as | Needs |
|---|---|---|
| the hydra daemon + its workers | `hydra` | read/write the repo, `.git`, and `.beads` |
| the repo's owner (a human) | `ajantha`, `buddhi`, … | the same, plus `br` from their own shell |
| a human's `pi` session | that human | generates beads from a PRD via `br` |

Humans run `br` deliberately — the `create-prd` → task-breakdown flow produces
beads from their own shell. So "only the daemon touches `.beads`" is not an
option, and any design that assumes a single writer is wrong.

**This must survive**: a new user being added, a user creating a new repo, a
`br` upgrade, and someone running `br` under `sudo`. None of those may take the
fleet down for a project — let alone silently.

## What actually broke (2026-08-06 → 2026-08-09)

`srv-smart-engine` had **43 beads, 0 closed**, untouched since 08-06 20:02,
while the dogfood project ran normally. The daemon logged, over three days:

| Symptom | Count |
|---|---|
| `br update --status open` failed | 16,609 |
| `Charon 🔧 Recovering orphan in_progress bead` | 16,620 |
| `br show --json` failed | 14,395 |
| `[Pan] skipping duplicate continue` | 20,293 |
| worker processes started | 1,096 |
| beads closed | **0** |

The cause was a single file:

```
/home/ajantha/projects/srv-smart-engine/.beads/issues.jsonl
  root:hydra  600
```

`issues.jsonl` is `br`'s git-trackable export beside `beads.db`. `br` creates it
**mode 0600**. Mode 600 is survivable only while the owner happens to be the
account that needs it. On 2026-08-06 21:15 someone ran `br` as **root** in that
directory; root's rewrite took ownership, and from that moment the daemon was
locked out of its own bookkeeping while retaining full access to everything
else.

That asymmetry is why it was hard to see:

```
project files       READ  ok
.git                WRITE ok          (ajantha:ajantha 775 + ACL)
.beads/beads.db     WRITE ok          (hydra:hydra 664)
.beads/issues.jsonl READ  DENIED      (root:hydra 600)   ← only this
```

`br list --json` works (it reads SQLite), so the project looks alive. `br show`,
`br update` and `br list --status` all fail. Workers spawn, `pi` writes to the
worktree happily, git branches get created — and no bead state can ever be
recorded. **This is not a "hydra can't reach other users' folders" problem.**
`pi` had zero permission errors across 280 worker logs for this project.

### It is not one project

| Project | `issues.jsonl` | hydra read | `br` | Broken since |
|---|---|---|---|---|
| outreach-workflow | `root:root` **660** | ok | ok | — |
| **srv-file-handler** | `root:beads` **600** | **DENIED** | **FAIL** | 2026-07-03 |
| **srv-smart-engine** | `root:hydra` **600** | **DENIED** | **FAIL** | 2026-08-06 |
| hydra-code (dogfood) | `hydra:beads` 600 | ok | ok | — |

The rule is **mode 600 + owner ≠ hydra ⇒ the project is invisible to the
daemon**. `outreach-workflow` survives only because it is 660 — group bits set.
Dogfood survives only because `hydra` owns the file. Two projects have been dead
for over a month and three days respectively, with no alert.

## Why the existing safeguards did not help

Everything needed for group sharing is **already in place**:

- `hydra-onboard-sweep` runs **every 15 minutes** as root, walks users, and
  idempotently re-grants `setfacl -R -m u:hydra:rwX` plus default ACLs per
  project. Last run: `onboarded=7 skipped=2 failed=0`.
- `umask` is already **0002** for humans and for `hydra`.
- The `beads` group exists with 11 members including `hydra`, `ajantha`,
  `buddhi`.

And yet, minutes after a successful sweep:

```
user:hydra:rwx   #effective:---
mask::---
```

**`chmod 0600` zeroes the ACL mask, which cancels every named ACL entry.** The
sweep grants access; `br`'s file mode revokes it. ACLs are the wrong primary
mechanism here precisely because they do not survive a `chmod`.

### Two sources of truth

The sweep iterates:

```sh
for cfg in "$HOME_BASE"/*/.hydra/config.yaml    # HOME_BASE=/home
```

It onboards projects listed in each **user's** `~/.hydra/config.yaml`. The
daemon runs projects listed in **`/etc/hydra/hydra.yaml`**. These drift:

- projects outside `/home` (`/opt/outreach-workflow`,
  `/opt/dev-tools/var/hydra/dogfood/hydra-code`) are swept only if some user
  happens to list them
- users without `~/.hydra/config.yaml` are skipped entirely (`skipped=2`)

That is why 15-minute sweeps never repaired `outreach-workflow`.

## The model we want

Ownership must stop being load-bearing. Group membership carries access:

```
<project>/.beads/          <owner>:beads   2775   setgid — children inherit `beads`
<project>/.beads/*         <owner>:beads    664   group-writable
```

Owner may be the human or `hydra` — it does not matter. Every member of `beads`
(the daemon and the humans) can read and write. A stray `sudo br` inherits group
`beads` via setgid and leaves 664 intact, so it is harmless.

This is what the system was clearly designed for. Only `br`'s 0600 prevents it.

## Fix plan

Ordered so that each step is useful alone, and none depends on a dogfood run —
the fleet cannot currently be trusted to build this (see Appendix).

**1. `br` creates group-friendly files.** `issues.jsonl` at **0664**, `.beads/`
at **2775**, honouring umask. `br` is ours
(`components/infra/services/beads-rust`). This is the enabler for humans running
`br` at all: today, a user generating beads from a PRD in a **new** repo creates
a 600 file owned by themselves and locks the daemon out immediately. Requirement
"users can run `br`" and current `br` behaviour are directly incompatible. Ship
with a regression test, so a future `br` upgrade cannot reintroduce it.

**2. Make `/etc/hydra/hydra.yaml` the authority for onboarding.** The sweep
should reconcile the daemon's own project list — wherever those paths live —
rather than per-user configs. Keep the `/home` scan as an addition, not the
source of truth.

**3. The sweep repairs modes, not just ACLs.** Per project: `chgrp -R beads`,
`chmod g+rw`, `chmod g+s` on `.beads`. This is the backstop to step 1 — anything
that writes a 600 file is repaired within 15 minutes instead of rotting for a
month. Group + setgid survives `chmod`; ACLs do not.

**4. The sweep owns group membership.** Ensure every real user is in `beads`,
`pi`, `hydra`. Adding a user then becomes *just adding a user*.
`yasanjith` is currently in **no groups at all** (`yasanjith yasanjith`) despite
being a hydra user with a project — that is requirement 1, and it must not be a
manual step ever again.

**5. Verify, and fail loudly.** The sweep asserts per project, as `hydra`, that
`br show`, `br update` and `br list --status` all succeed, and reports failures.
The daemon treats `EACCES` from `br` as **project-fatal**: log once, mark the
project unhealthy, skip it. One broken file must cost one log line — not 16,609
failed reopens and 1,096 wasted worker spawns.

### Immediate unblock (separate from the plan)

`chown <user>:beads issues.jsonl && chmod 664` on the two dead projects, then
reconcile their wedged `in_progress` beads. This returns 43 + N beads to the
fleet today, but it is a patch: without step 1, the next `br` run re-breaks it.

## How to verify the fix holds

Each of these must pass **without manual intervention**:

1. Add a new user → within 15 minutes they are in `beads`/`pi`/`hydra` and their
   projects are accessible to the daemon.
2. A user runs `br init` in a brand-new repo → the daemon can immediately
   `show`/`update` beads there.
3. Run `sudo br update <bead>` in a project → the daemon still works afterwards.
4. `chmod 600` a project's `issues.jsonl` by hand → the next sweep repairs it.
5. Add a project to `hydra.yaml` outside `/home` → it is onboarded.
6. Upgrade `br` → the regression test fails if 0600 returns.

## Appendix: why not fix this via a dogfood run

The fleet cannot currently do work. Over 2026-08-09 → 08-10:

```
worker spawns        1,567
completions              3      (0.2%)
exit code 6 (quota)  1,478
quota exhausted         42      breaker paused spawns 42 × 30m
Provider cooldown    7,819
```

Every spawn opens a provider session. Hydra exhausted the Kimi quota retrying
work it structurally could not complete — much of it against the very projects
broken by this defect. Fixing this by dogfooding would consume the quota needed
to verify the fix.
