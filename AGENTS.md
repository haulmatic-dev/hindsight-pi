# Hydra Agent Workflow

## Intelligence Protocol (Mandatory)

Every implementation, fix, or refactor MUST follow this 5-step workflow. Skipping steps reproduces bugs that were already fixed.

### Step 1: Query CASS (Institutional Memory)

```bash
cm context "<concise description of what you are about to implement>" --json
```

- Parse JSON output. Look for bullets with `finalScore >= 0.7`.
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
