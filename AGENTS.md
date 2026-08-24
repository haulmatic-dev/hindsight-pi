# Hydra Agent Workflow

## Intelligence Protocol (Mandatory)

Every implementation, fix, or refactor MUST follow this 5-step workflow. Skipping steps reproduces bugs that were already fixed.

### Step 1: Query CASS (Institutional Memory)

```bash
cm context "<concise description of what you are about to implement>" --json
```

- Parse JSON output. Bullets arrive pre-ranked under `data.relevantBullets`; healthy young rules score around `0.4`, so treat `finalScore >= 0.3` as signal — a `0.7` bar filters out nearly every real rule. Skip `deprecated` bullets.
- Read `content` field for proven patterns and anti-patterns.
- **If an anti-pattern matches your current approach, STOP and follow the documented fix.**

### Step 2: Search the Codebase with the Code Graph

```bash
graphify query "<what you need to implement>" --budget 2000
```

- Use results to find existing code patterns before writing new code.
- `--budget` caps the answer in tokens; keep it tight (2000 is the default).
- The graph lives in `graphify-out/graph.json` and is refreshed by Hydra per
  project, not per bead. If it is missing, fall back to `rg "<pattern>"` — never
  build a graph yourself mid-bead, it costs ~15s and will be stale immediately.

### Step 3: Analyze Impact (For Modifications)

```bash
graphify affected "<symbol or file>" --depth 2
```

- Required when modifying existing files.
- Reverse traversal: shows what depends on the thing you are about to change.
- `graphify god-nodes --top 10` names the architectural hubs — treat a change to
  one as high blast radius.

> Replaces `llm-tldr`, which is gone. It went unmaintained, returned zero
> semantic units on Go (silently falling back to grep), and its service was not
> even running in production. Do not reintroduce `llm-tldr` or `hydra-llm-tldr`
> commands; neither binary exists.

### Step 4: Save New Patterns

After discovering a new anti-pattern, proven fix, or architectural insight:

```bash
cm add "<concise pattern description>" --category <architecture|testing|performance|git|verification>
```

**Save within the same session. Do not defer.** Context is lost across sessions.

For project-specific patterns that belong in durable memory:

```bash
hindsight_retain "<project-specific insight>" --context "<why this matters for hydra>"
```

### Step 5: Verify and Retain

After implementation:
- Run tests: `go test ./...`
- If tests pass and you discovered a pattern, GOTO Step 4.
- If tests fail, fix, then GOTO Step 4.

---

## Category Guide for `cm add`

| Category | Use For |
|----------|---------|
| `architecture` | Design decisions, state machine rules, interface contracts |
| `testing` | Mock gotchas, test setup traps, assertion patterns |
| `performance` | Bottlenecks, resource limits, concurrency issues |
| `git` | Commit conventions, merge workflows, branch hygiene |
| `verification` | Validation logic, config parsing, deployment checks |

---

## Anti-Patterns (DO NOT)

1. **DO NOT** start implementing without querying CASS first.
2. **DO NOT** write new code without querying the code graph for existing patterns.
3. **DO NOT** modify files without running `impact` analysis.
4. **DO NOT** discover a pattern and defer saving it to "later".
5. **DO NOT** save raw execution output or JSON events — save distilled insights only.

---

## Picking Up Development

Read `docs/operations/handoff-20260731.md` first. It records the current state of
the box, what changed most recently, the open work in priority order, and — most
importantly — the measurement traps that produced wrong conclusions repeatedly.
Two you will hit within your first hour:

- **Anchor log greps on the daemon's own prefix.** Worker output is echoed into the
  same file as `[Worker:<bead>] [stderr] {...}`, which also starts with a bracket.
  An unanchored grep reported 473 `infra-exhausted` events that never happened and
  20 gap-to-prd cycles the daemon never logged. Run every count twice, with and
  without `grep -vE '^[0-9/]+ [0-9:]+ \[Worker:'`; if they differ, it is contaminated.
- **`grep -c` exits 1 on no match**, so `$(grep -c x f || echo 0)` yields `"0\n0"`
  and every `[ ... -eq ]` on it dies — falsy, so guards built that way never fire.

## Hydra-Specific Patterns (Quick Reference)

### Shepherd Layer (read before touching close/chain/fix/scheduler code)
- `docs/architecture/system-loop.md` — the three control loops and where your change sits.
- `docs/architecture/shepherd.md` — event stream contract, closed-failed labels + quarantine, fix-bead lifecycle (cause/dedup/cap), AIMD governor, verdict memory.
- Every significant close/fix/chain decision must emit its typed event; the stream is fail-open and must never gate the pipeline.
- The beads store has no close-reason field — durable verdicts are labels (`closed-failed`, `fix-recursion-capped`, `infra-exhausted`).


### Bead IDs
- `NewNumericBeadID(project, repo, number).ID()` returns only the numeric suffix.
- Use `string(NewNumericBeadID(...))` for the full bead ID.

### Dependency Graph
- `BuildFromBeads` must skip edges for already-completed dependencies (`StatusCompleted`, `StatusClosed`).
- Otherwise completed beads continue blocking their dependents.

### Scheduling
- `GetReadyBeadsFromDAG` must:
  1. Filter approval-gates (both `approval-gate` and `approval` types, any status)
  2. Exclude session-completed beads (`o.completed`)
  3. Sort by priority descending

### Interface Changes
- When `BeadsClient` interface changes, update ALL mock implementations:
  - `mockBeadsClient` in `overseer_integration_test.go`
  - `enhancedMockBeadsClient` in `worker_completion_test.go`

### Process Management
- `ps -g` filters by session ID, NOT process group ID.
- To list by PGID: `ps -e -o pid,pgid` and filter by PGID column.

---

## Verification Checklist

Before marking any task complete:

- [ ] CASS queried and results reviewed
- [ ] Code graph queried for existing patterns (`graphify query`)
- [ ] Impact analysis run (if modifying files)
- [ ] Tests pass: `go test ./...`
- [ ] New patterns saved to CASS (`cm add`)
- [ ] Project insights saved to Hindsight (`hindsight_retain`)

---

## End-to-End Testing Setup

When running Hydra end-to-end on a real project, the following configuration MUST be correct. Getting any of these wrong causes silent failures (0 skills loaded, stub workers, malformed beads).

### 0. Operating knobs (live tuning)

| Knob | Where | Proven value | Why |
|---|---|---|---|
| `overseer.max_workers` | yaml | 2 | spawn ceiling; AIMD scales within it; restart to apply |
| `overseer.worker_timeout` | yaml | 45m | active-time cap (parked excluded); 15m = zero completions |
| `HYDRA_WORKER_ACTIVITY_TIMEOUT` | systemd drop-in | 10m | watchdog horizon; provider queue waits >4m killed turns at the default |
| `output.push` | yaml | true | pushes integration branch to origin; off = finished work invisible |
| `worktree.quarantine_auto_drain` | yaml | false | automatic cleanup of stale `hydra/quarantine/*` branches during the worktree reconciler tick; enable only after reviewing the drain rules |

Full table with semantics: `hydra-operator` skill §6 ("Operating knobs").


### 1. Pi Config Directory (CRITICAL)

`hydra.yaml` MUST point `pi.config_dir` to the **centralized Pi config** (not a temp directory):

```yaml
pi:
  config_dir: <dev-tools-checkout>/config/pi
```

Where `<dev-tools-checkout>` is the path to your `dev-tools` repository (e.g., `~/workspace/dev-tools`, `/opt/dev-tools`, etc.).

**For isolated testing** (no centralized config available), use the Hydra repo's local `.pi`:
```yaml
pi:
  config_dir: <hydra-repo>/.pi
```

**WRONG** (creates empty pi-config, 0 skills):
```yaml
pi:
  config_dir: /tmp/hydra-e2e-test/pi-config
```

**Why it matters:**
- The worker looks for skills at `{pi.config_dir}/skills/`
- The centralized config has **30+ skills** (`golang-patterns`, `test-driven-development`, `tdd-chain-validation`, etc.)
- The Hydra repo has **27 skills** as symlinks in `.pi/skills/` (fallback only)
- Pointing to a non-existent directory results in `0/16 skills available`
- Code generation still works without skills, but quality is degraded

**Verify centralized config:**
```bash
ls <dev-tools-checkout>/config/pi/skills/
# Should show: angular-component, golang-patterns, test-driven-development, tdd-chain-validation, ...
```

### 1b. Paths: code, config and state are separate

```
/opt/dev-tools/components/hydra/   code (git checkout) — never written at runtime
/opt/dev-tools/config/pi/          shared Pi config (see 1a)
/opt/dev-tools/var/hydra/          runtime state = $HYDRA_HOME = the hydra user's home
/var/log/hydra/                    daemon stdout/stderr, written by systemd
```

Runtime state must NOT live inside the code checkout. It is a git working tree: a
`.hydra-events/` directory was once committed into the repo from a cwd fallback,
and `git clean -fdx` there would delete live worktrees and the run lock. `var/` is
gitignored in the `/opt/dev-tools` monorepo for the same reason.

`/opt/hydra` is a compatibility symlink to `/opt/dev-tools/var/hydra` and will be
removed; do not write new references to it.

**Never let a test use the real `$HYDRA_HOME`.** `TestMain` in `internal/core`
pins it to a temp dir. Without that, tests inherit the daemon's value — workers
running this repo's suite created 10,251 dead worktree entries in live state.

**Never deploy to `command -v hydra`.** systemd runs `$HYDRA_HOME/bin/hydra`;
`/usr/local/bin/hydra` is a different file, and installing there succeeds while
changing nothing. Build packages (`./cmd/hydra`), not single files, then prove it
with `scripts/verify-deploy.sh`.

Full reference: `docs/architecture/filesystem-layout.md`.

### 1a. One Pi config, shared with humans (CRITICAL)

`config_dir` and `worker_config_dir` MUST be the same directory, and hydra MUST
NOT write to it:

```yaml
pi:
  config_dir: /opt/dev-tools/config/pi
  worker_config_dir: /opt/dev-tools/config/pi   # same value, deliberately
  manage_config: false                          # read-only: never write here
```

That directory is also every human's Pi config — each user's `~/.pi/agent` is a
symlink to it — so a skill or extension added once is used by the worker fleet
and by interactive shells alike.

**Never split them again.** They were split until 2026-07-31:
`/opt/dev-tools/config/pi-worker` was a generated `rsync` copy, and it drifted
to 239 npm packages against the canonical 242 with nothing detecting it. Its two
original purposes are both obsolete — the "remove context-mode for faster
startup" edit had stopped applying (the copy still listed
`npm:context-mode@1.0.169`), and workers bypass `settings.json` discovery anyway
via `--no-extensions`; and "keep sessions separate" is unnecessary because Pi
namespaces `sessions/` by working directory, which is already unique per worker
worktree.

**`manage_config: false` is the default and must stay false for a shared
directory.** When true, daemon startup creates the directory structure, installs
Pi and LSP servers, and refreshes skills on a timer — i.e. it edits the team's
environment as a side effect of starting. Set it true only for a config
directory hydra alone owns.

**Extensions are chosen by hydra, not discovered.** The daemon passes a per-phase
allowlist (`agents.pi.extensions`, default `[rtk, context-mode]`, overridable per
phase via `phase_extensions`); the worker turns that into `--no-extensions` plus
explicit `-e <abs path>` resolved from `{config_dir}/npm/node_modules`. This is
why sharing the config does not drag in the interactive extras: loading
everything measured 48,315 tokens against 12,059 for rtk alone, and themes and
editor widgets are unusable by a headless worker.

**Verify before trusting any of the above:**
```bash
scripts/verify-pi-config.sh
# asserts: the two dirs match, it is a Pi package root, every allowlisted
# extension resolves, each ~/.pi/agent points at it, and no retired copy is
# still referenced
```

**Verify local fallback:**
```bash
ls <hydra-repo>/.pi/skills/
# Should show symlinks to ../../.agents/skills/...
```

### 2. Worker Binary (CRITICAL)

Hydra MUST use the **real worker** (`cmd/worker/main.go`) to test code generation:

```bash
# Build the real worker
go build -o /tmp/hydra-e2e-test/real-worker ./cmd/worker

# Run Hydra with it
HYDRA_WORKER_BINARY=/tmp/hydra-e2e-test/real-worker ./hydra -config hydra.yaml
```

**WRONG** (uses stub, no code generation):
```bash
# Do NOT use a stub worker for E2E code generation tests
HYDRA_WORKER_BINARY=/tmp/hydra-e2e-test/worker-stub ./hydra -config hydra.yaml
```

**Why it matters:**
- The stub just sleeps and exits — it never invokes the Pi agent
- The real worker builds Pi commands, sends prompts via stdin, and captures LLM output
- Only the real worker modifies project files (writes tests, implementations)

### 3. Bead Creation via `br` CLI (CRITICAL)

Beads MUST be created through the `br` CLI — never hand-craft `.beads/issues.jsonl`:

```bash
cd /path/to/project
B1=$(br create "RED: Write tests" --type test --priority 4 --silent)
B2=$(br create "GREEN: Implement" --type implement --priority 4 --silent)
B3=$(br create "REFACTOR: Clean up" --type refactor --priority 2 --silent)

# Link dependencies
br dep add "$B2" "$B1"   # B2 depends on B1
br dep add "$B3" "$B2"   # B3 depends on B2
```

**WRONG** (malformed JSONL, br rejects it):
```bash
# Do NOT hand-craft .beads/issues.jsonl
cat > .beads/issues.jsonl << 'EOF'
{"id":"test-001","type":"test",...}
EOF
```

**Why it matters:**
- `br` validates JSONL against its Rust struct schema on every `list`/`show`
- Missing fields (`created_at`, `created_by`) or wrong types cause `exit status 7`
- Hand-crafted files fail preflight checks: `json_valid: Found N invalid issue record(s)`

### 4. Minimal E2E Test Config

```yaml
overseer:
  max_workers: 2
  bead_discovery_interval: 3s
  default_retries: 1
  worker_timeout: 1m

projects:
  auto_discover: false
  list:
    - name: url-shortener
      path: /tmp/hydra-e2e-test/url-shortener
      type: single

# Disable heavy features for faster startup
intelligence:
  graphify: { enabled: true }
  gptcache: { enabled: false }
  hindsight: { enabled: false }
  patterns: { enabled: false }
  settled_state: { enabled: false }
  collective_unconscious: { enabled: false }

agents:
  enabled: false

# Point to centralized pi config with skills
pi:
  config_dir: <dev-tools-checkout>/config/pi
  skills:
    update_interval: 24h
    auto_update: false

validation:
  enabled: true
  tdd_validation_enabled: true
  prd_validation_enabled: true
  max_rounds: 2
  agent_verification_enabled: false

mcp: { enabled: false }
api: { enabled: false }
```

### 5. Expected E2E Output

When correctly configured, you should see:

```
[PiSetup] ✅ Pi skills ready
[Worker] Added 27 skill flags
🔧 Executing Pi: /bin/pi --no-session --provider kimi-coding --model kimi-for-coding
📁 Working directory: /tmp/hydra-e2e-test/url-shortener
✅ Pi completed successfully
📝 Execution output written to: /tmp/hydra-executions/br-xxx-YYYYMMDD-HHMMSS.json
```

And the project files should be modified:
```bash
ls -la /tmp/hydra-e2e-test/url-shortener/
# main.go        — implementation written by Pi
# main_test.go   — tests written by Pi
```

## Documentation & Changelog Discipline (Mandatory)

Every change that lands — whether authored by a human, Claude, or a Hydra
worker — MUST update the paper trail in the same commit/PR:

1. **`CHANGELOG.md`** — add an entry under `## [Unreleased]` in the matching
   section (`Added` / `Fixed` / `Changed` / `Documentation`). One or two
   lines: what changed and why it matters to an operator. Hydra-authored
   changes note it: `*Authored by a Hydra dogfood run.*`
2. **Affected docs** — if the change alters behavior described anywhere in
   `docs/` or this file (config knobs, workflows, log formats, commands),
   update that description in the same change. A config knob without a doc
   line does not exist.
3. **PRD-driven work** — a bead's changelog entry is part of DONE. The
   Target-Files scope gate allowlists `CHANGELOG.md` and `docs/**` so these
   updates are never counted as off-scope.

Rule of thumb: if an operator reading only `CHANGELOG.md` since the last
release would be surprised by the system's behavior, the entry is missing or
too vague.

## Engineering Doctrine (Mandatory)

Universal rules for every agent working in this repo — human, Claude, or Hydra worker. Phase-specific versions are injected into worker prompts automatically (`phaseGuidance` in `cmd/worker/main.go`).

1. **Evidence over narration.** Never claim a command succeeded without running it in this task and quoting its actual output. Label claims **VERIFIED** (you ran it, output shown) or **ASSUMED** (you did not). An ASSUMED "tests pass" is a defect.
2. **Smallest change that wins.** Touch only files your task requires. Unrelated bug or improvement spotted? Do NOT fix it inline — record it (`cm add "..." --category ...`) and move on. Drive-by edits invalidate your diff.
3. **Fail-open vs fail-closed.** Checks that might be broken themselves (network, optional tooling, telemetry) fail **open**: log and continue. Product gates (tests, build, acceptance criteria, scope) fail **closed**: block. Never invert these.
4. **Exact errors.** Quote error messages verbatim, never paraphrased — the exact string is the next agent's search key.
5. **Honest completion.** Anything not done or not verified is reported as **NOT DONE** / **NOT VERIFIED** in the final summary. A false "done" costs ten times a true "blocked".
6. **Root cause, not symptom.** Silencing an error, skipping a test, or widening a timeout is a symptom fix; it is acceptable only when evidence shows it IS the root cause.

---

## Change Hygiene (mechanical, not aspirational)

Every rule here exists because the mistake happened and cost real time. Install
the guards once:

```bash
./scripts/install-hooks.sh
```

### Branch from the base branch explicitly

```bash
git fetch github --quiet
git checkout -b <name> github/development
```

Not `git checkout -b <name>` from wherever you happen to be standing. PR #87 was
branched off another PR's branch, carried that PR's commits, and had its approval
dismissed because the same change appeared in two open PRs. The work was correct;
the starting point was not. The pre-push hook warns when a branch looks stacked.

### Never hide `git add` errors, and verify what landed

```bash
git add <explicit paths>          # never with 2>/dev/null
git show --stat HEAD              # confirm the files you intended are there
```

Two separate incidents: a `git add` naming a path that did not exist yet made git
abort the *entire* invocation, producing a commit with only a changelog entry that
broke the next deploy; and a `git add` on a path behind a symlink failed with its
error swallowed by `2>/dev/null`, so a PR shipped half its content. `git add` is
all-or-nothing — one bad pathspec discards the whole staging operation.

### Edit code with tools that fail loudly

A silent `str.replace` / `perl -pi -e` that matches nothing leaves the file
untouched and reports success. That is how #85's description and changelog came to
claim a `provider_breaker.go` change the commit did not contain — the replacement
used two tabs against a file indented with one.

Prefer an editor that errors when its anchor is missing. If you must use a script,
`grep` for the result afterwards and treat a zero count as failure.

### Verify the effect, not the command

The pattern behind all of the above is asserting an action succeeded because it
exited 0. Check the effect: `git show --stat` after committing, `grep -c` after a
scripted edit, a build after a refactor.

The same applies to measurement. State the denominator and the window — "0 hits in
the last 3 minutes" was reported as "extensions are not firing" when the true
figure was 1,170 hits across 26 logs; only one log fell inside that window.
