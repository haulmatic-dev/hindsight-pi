---
name: task-breakdown
description: Break down PRDs into actionable tasks with TDD enforcement. Uses deterministic runner for reliable, repeatable task creation with proper dependency management.
---

# Task Breakdown Skill

**⚠️ DETERMINISTIC CLI ONLY**: This skill must be invoked via CLI command, not as a subagent.

## Usage

```bash
node /Users/buddhi/source-code/personal/dev-tools/config/pi/skills/task-breakdown/cli.mjs <prd-path> [--json]
```

## What It Does

Deterministically breaks down PRDs into atomic tasks with:
- Automatic TDD enforcement (RED → GREEN → REFACTOR → SYNTAX → LINT)
- Synchronous task creation with dependencies
- State persistence and resume capability
- Automatic rollback on failure

## Output

Streams JSON events to stdout. Use `--json` for structured output suitable for piping.

## Task Structure Created

For each requirement in the PRD, the following task chain is created:

```
APPROVAL GATE (in_progress, blocks all)
  ↓
Discovery Task (optional, for brownfield)
  ↓  
Implementation Task (T-{n}-{m}: Title)
  ├─ RED: Write tests for {title} (blocked by impl)
  ├─ GREEN: Implement {title} (blocked by red)
  ├─ REFACTOR: Optimize {title} (blocked by green)
  └─ SYNTAX: Check {title} (blocked by refactor)
      ↓
LINT GATE (blocked by all SYNTAX tasks)
```

## Examples

```bash
# Basic usage
node /Users/buddhi/source-code/personal/dev-tools/config/pi/skills/task-breakdown/cli.mjs docs/PRD/feature.md

# JSON output for scripting
node /Users/buddhi/source-code/personal/dev-tools/config/pi/skills/task-breakdown/cli.mjs docs/PRD/feature.md --json

# With custom session ID
node /Users/buddhi/source-code/personal/dev-tools/config/pi/skills/task-breakdown/cli.mjs docs/PRD/feature.md --json --session-id my-feature
```

## Files

- `cli.mjs` - CLI entry point
- `runner.mjs` - 6-step state machine workflow
- `executor.mjs` - Beads CLI integration (creates actual beads)
- `analyzer.mjs` - Deterministic PRD parsing
- `state.mjs` - Resume capability and persistence
- `rollback.mjs` - Error recovery
- `index.mjs` - Module exports
