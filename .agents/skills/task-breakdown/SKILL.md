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

## Bead Spec Quality (measured, 2026-08-24)

Spec precision is the single biggest token-cost lever: two same-class dogfood
beads under identical machinery cost 0.9M vs 8.9M tokens. The difference: the
cheap one named exact files, function names, and semantics; the expensive one
said "reuse the existing path logic" without naming it. Every task description
this tool emits (or a human writes) MUST:

- Name anchor symbols: exact file paths, existing functions to extend, table
  names, config keys. Never "reuse existing X" without naming X.
- End with a `Files:` line listing exact files (or a single directory) the
  task may touch — this feeds the pre-spawn probe scoping and the close-path
  scope gate.
- State fail-open vs fail-closed expectations for each new check.
- State the verification commands the worker must run (narrow test first,
  then broad).

## Files

- `cli.mjs` - CLI entry point
- `runner.mjs` - 6-step state machine workflow
- `executor.mjs` - Beads CLI integration (creates actual beads)
- `analyzer.mjs` - Deterministic PRD parsing
- `state.mjs` - Resume capability and persistence
- `rollback.mjs` - Error recovery
- `index.mjs` - Module exports
