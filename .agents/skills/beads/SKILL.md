---
name: beads
description: Task tracking with Beads - a graph-based issue management system. Use when working with tasks, issues, dependencies, project management, or when user mentions beads, br command, bv command, task tracking, or issue management.
---

# Beads Task Tracking System

## Overview

Beads is a graph-based task/issue tracking system that stores issues in `.beads/` and tracks them in git. It provides:

- **Dependency management** - Issues can block other issues
- **Graph analytics** - PageRank, betweenness, critical path detection
- **AI-friendly** - Robot commands for agent integration
- **Git integration** - Issues sync with commits

## Quick Reference

```bash
# Basic workflow
br ready                    # Find available work (no blockers)
br show <id>               # View issue details
br update <id> --status in_progress   # Claim work
br close <id>              # Complete work
br sync                    # Sync with git

# AI Agent Commands (use --robot-* flags)
bv --robot-triage          # Get recommendations (START HERE)
bv --robot-next            # Single top pick
```

## Installation

Beads consists of two tools:

1. **beads (`br`)** - Core issue management
   ```bash
   # Install via pip
   pip install beads
   ```

2. **beads_viewer (`bv`)** - Graph analytics and AI integration
   ```bash
   # Install via pip
   pip install beads-viewer
   ```

## Beads Commands (`br`)

### CRUD Operations

```bash
# Create
br create "Fix authentication bug"
br create "Add user feedback feature" \
  --description "Implement feedback form..." \
  --type feature \
  --priority 1 \
  --labels backend,ui \
  --deps bd-42,bd-45

# Read
br show bd-123
br list --status open
br list --label backend

# Update
br update bd-123 --status in_progress
br update bd-123 --priority 0
br edit bd-123                    # Edit in $EDITOR

# Delete
br delete bd-123 --force
br delete bd-123 --cascade --force   # Remove dependents too
```

### Status Values

- `open` - Available to work on
- `in_progress` - Currently being worked
- `closed` - Completed
- `deferred` - Postponed

### Priority Levels

- `0` (P0) - Critical
- `1` (P1) - High
- `2` (P2) - Medium (default)
- `3` (P3) - Low
- `4` (P4) - Backlog

### Issue Types

- `task` - General work item
- `bug` - Defect to fix
- `feature` - New capability
- `epic` - Large body of work
- `question` - Needs clarification
- `docs` - Documentation

### Dependency Management

```bash
# Add dependency (bd-1 blocks bd-2)
br dep add bd-2 bd-1

# Remove dependency
br dep remove bd-2 bd-1

# List dependencies
br dep list bd-2 dependencies    # What blocks bd-2
br dep list bd-2 dependents      # What bd-2 blocks

# Show dependency tree
br dep tree bd-1

# Detect cycles
br dep cycles
```

### Search and Filter

```bash
# Search
br search "authentication bug"
br search "login" --status open

# Filter
br list --status in_progress
br list --priority-min 0 --priority-max 2
br list --label backend --label urgent
br list --created-after "2025-01-01"
br list --title-contains "bug"

# Sort and limit
br list --sort priority --reverse --limit 10
```

### Labels

```bash
br label add bd-123 backend urgent
br label remove bd-123 urgent
br label list-all
```

### Issue State

```bash
# Defer/Undefer
br defer bd-123
br defer bd-123 --until "next monday"
br undefer bd-123

# Reopen
br reopen bd-123 --reason "Reopened for investigation"

# Duplicate/Supersede
br duplicate bd-456 --of bd-123
br supersede bd-456 bd-789
```

### Comments

```bash
br comments bd-123
br comments add bd-123 "This is blocking release"
```

### Sync and Status

```bash
br sync                    # Commit beads to git
br status                  # Database status
br count --status open     # Count issues
br stale --days 30         # Show stale issues
```

## Beads Viewer Commands (`bv`)

**⚠️ CRITICAL:** Use ONLY `--robot-*` flags for AI agents. Bare `bv` launches interactive TUI.

### Primary Commands

#### bv --robot-triage (START HERE)

Single command returns everything needed:

```json
{
  "quick_ref": {
    "total_issues": 42,
    "open": 15,
    "in_progress": 3,
    "top_3_ready": ["bd-5q", "bd-3x", "bd-7m"]
  },
  "recommendations": [
    {
      "id": "bd-5q",
      "title": "Fix login bug",
      "score": 0.95,
      "reason": "High PageRank, unblocks 5 other issues",
      "unblocks": ["bd-6a", "bd-6b", "bd-6c", "bd-7a", "bd-7b"]
    }
  ],
  "quick_wins": [...],
  "blockers_to_clear": [...],
  "project_health": {
    "status_distribution": {...},
    "graph_metrics": {...}
  },
  "commands": {
    "claim_top": "br update bd-5q --status in_progress"
  }
}
```

#### bv --robot-next

Minimal output - single top pick:

```json
{
  "recommendation": {
    "id": "bd-5q",
    "title": "Fix login bug",
    "claim_command": "br update bd-5q --status in_progress"
  }
}
```

### Planning Commands

```bash
# Get parallel execution tracks
bv --robot-plan

# Output includes:
# - Tracks of work that can happen in parallel
# - Unblocks lists for each track
# - Critical path identification

# Scope to label
bv --robot-plan --label backend

# Pre-filter recipes
bv --recipe actionable --robot-plan      # Only ready items
bv --recipe high-impact --robot-triage   # High PageRank items
```

### Analysis Commands

```bash
# Full graph metrics
bv --robot-insights

# Returns:
# - PageRank: Dependency importance
# - Betweenness: Bottlenecks & bridges
# - HITS: Hub/Authority (Epics vs Utilities)
# - Critical Path: Longest dependency chain
# - Eigenvector: Strategic dependencies
# - Cycles: Circular dependencies (CRITICAL to fix)
# - Density: Project coupling health

# Check specific metrics
bv --robot-insights | jq '.Cycles'
bv --robot-insights | jq '.CriticalPath'
```

### Health Commands

```bash
# Label health
bv --robot-label-health

# Returns per-label:
# - health_level: healthy|warning|critical
# - velocity_score: tasks completed per day
# - staleness: average age of open issues
# - blocked_count: issues blocked in this label

# Label flow (cross-label dependencies)
bv --robot-label-flow

# Attention-ranked labels
bv --robot-label-attention
```

### Alert Commands

```bash
# Get alerts
bv --robot-alerts

# Returns:
# - Stale issues (no activity)
# - Blocking cascades
# - Priority mismatches

# Suggestions for improvement
bv --robot-suggest

# Returns:
# - Duplicate detection
# - Missing dependencies
# - Label suggestions
# - Cycle break recommendations
```

### Historical Commands

```bash
# History and correlations
bv --robot-history

# Changes since ref
bv --robot-diff --diff-since HEAD~10

# Burndown
bv --robot-burndown sprint-1

# Forecast
bv --robot-forecast bd-123
bv --robot-forecast all
```

### Export Commands

```bash
# Export graph
bv --robot-graph --graph-format=json
bv --robot-graph --graph-format=dot
bv --robot-graph --graph-format=mermaid

# Export Markdown
bv --export-md report.md
```

## Graph Metrics Reference

| Metric | Purpose | Key Insight |
|--------|---------|-------------|
| **PageRank** | Dependency importance | High = foundational blocker |
| **Betweenness** | Shortest-path traffic | High = bottleneck/bridge |
| **HITS** | Hub/Authority | Hubs=Epics, Authorities=Utilities |
| **Critical Path** | Longest chain | Zero slack = project delays |
| **Eigenvector** | Influence via neighbors | Connected to power players |
| **Cycles** | Circular deps | **CRITICAL**: Must fix |
| **Density** | Edge-to-node ratio | Low=healthy, high=overly coupled |

## jq Quick Reference

```bash
# Quick summary
bv --robot-triage | jq '.quick_ref'

# Top recommendation
bv --robot-triage | jq '.recommendations[0]'

# Check for cycles
bv --robot-insights | jq '.Cycles'

# Critical labels
bv --robot-label-health | jq '.results.labels[] | select(.health_level == "critical")'

# Best unblock target
bv --robot-plan | jq '.plan.summary.highest_impact'
```

## Workflow Patterns

### Daily Standup
```bash
bv --robot-triage | jq '.quick_ref'
bv --robot-alerts
```

### Sprint Planning
```bash
bv --robot-plan
bv --robot-insights | jq '.CriticalPath'
bv --robot-label-health
```

### Session Start
```bash
# Get recommendation
REC=$(bv --robot-next | jq -r '.recommendation.id')

# Claim it
br update $REC --status in_progress

# Show details
br show $REC
```

### Session End
```bash
# Update status
br update $REC --status closed  # or in_progress

# Sync to git
br sync

# Verify
bv --robot-history
```

## Best Practices

1. **Always use `--robot-*` flags** - Never bare `bv` in scripts
2. **Start with `bv --robot-triage`** - Gets everything in one call
3. **Check for cycles** - Run `bv --robot-insights | jq '.Cycles'` regularly
4. **Claim before working** - Update status to `in_progress`
5. **Sync at session end** - Always run `br sync`
6. **Never commit to main** - Create feature branches

## Integration with pi

### Session Protocol

```bash
# On session start
STATUS=$(bv --robot-triage)
echo "Available work: $(echo $STATUS | jq '.quick_ref.open')"
echo "Top pick: $(echo $STATUS | jq -r '.recommendations[0].id')"

# Claim recommended task
TOP_PICK=$(echo $STATUS | jq -r '.recommendations[0].id')
br update $TOP_PICK --status in_progress

# Work on task...

# On session end
br close $TOP_PICK --reason "Completed"
br sync
```

### beads-db File

Issues stored in `.beads/beads.db` (SQLite) and `.beads/issues.jsonl` (append-only log).

**Never edit directly** - always use `br` commands.

## Troubleshooting

### No issues found
```bash
br list --status open    # Verify issues exist
br status               # Check database health
```

### Cycle detected
```bash
br dep cycles           # Show cycle
# Break cycle by removing one dependency
br dep remove <blocked> <blocker>
```

### Sync conflicts
```bash
br sync                 # Auto-resolves most conflicts
git status              # Check for manual resolution needed
```

## Configuration

File: `.beads/config.yaml`

```yaml
# issue_prefix: bd
# default_priority: 2
# default_type: task
```

## Resources

- Beads: https://github.com/steveyegge/beads
- Beads Viewer: https://github.com/Dicklesworthstone/beads_viewer