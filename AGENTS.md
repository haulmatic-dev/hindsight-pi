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

### Step 2: Search Codebase with TLDR

```bash
hydra-llm-tldr search "<what you need to implement>" . --max 10
```

- Use results to find existing code patterns before writing new code.
- If semantic search returns 0 results, use `rg "<pattern>"` as fallback.

### Step 3: Analyze Impact (For Modifications)

```bash
hydra-llm-tldr impact <file-path> --depth 2
```

- Required when modifying existing files.
- Shows downstream callers affected by your change.

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
2. **DO NOT** write new code without checking TLDR for existing patterns.
3. **DO NOT** modify files without running `impact` analysis.
4. **DO NOT** discover a pattern and defer saving it to "later".
5. **DO NOT** save raw execution output or JSON events — save distilled insights only.

---

## Hydra-Specific Patterns (Quick Reference)

### Bead IDs
- `NewNumericBeadID(project, repo, number).ID()` returns only the numeric suffix.
- Use `string(NewNumericBeadID(...))` for the full bead ID.

### Dependency Graph
- `BuildFromBeads` must skip edges for already-completed dependencies (`StatusCompleted`, `StatusClosed`).
- Otherwise completed beads continue blocking their dependents.

### Scheduling
- `GetReadyBeadsFromDAG` must:
  1. Filter approval-gates
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
- [ ] TLDR searched for existing patterns
- [ ] Impact analysis run (if modifying files)
- [ ] Tests pass: `go test ./...`
- [ ] New patterns saved to CASS (`cm add`)
- [ ] Project insights saved to Hindsight (`hindsight_retain`)

---

## End-to-End Testing Setup

When running Hydra end-to-end on a real project, the following configuration MUST be correct. Getting any of these wrong causes silent failures (0 skills loaded, stub workers, malformed beads).

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
  tldr: { enabled: false }
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
