---
name: code-discovery
description: File discovery workflow for beads with "Files: TBD". Uses code-search-and-intelligence skill commands to find relevant files before implementation.
---

# Code Discovery Skill

File discovery workflow for tasks lacking concrete file targets.

## When to Use

**Trigger:** When bead description contains `Files: TBD`, `Files: Unknown`, or no file paths specified

**Purpose:** 
- Discover which files to create or modify
- Understand existing code patterns
- Avoid implementing blindly

## Required Skills

This skill depends on **code-search-and-intelligence**. Load it first:

```bash
@skill_use skill_names=["code-search-and-intelligence"]
```

## Discovery Workflow

### Step 1: Query Institutional Knowledge (ALWAYS FIRST)

Check for existing patterns and anti-patterns:

```bash
hindsight search "<bead title>: <bead description>" --limit 5
```

**Example:**
```bash
hindsight search "Hindsight server lifecycle: Initialize and manage embedded Python server" --limit 5
```

### Step 2: Search Code Semantically

Find existing code related to your task:

```bash
# Search by task description
hydra-llm-tldr search "<what you're implementing>" --limit 10

# Search by functionality keywords
hydra-llm-tldr search "<key functionality>"
```

**Example:**
```bash
hydra-llm-tldr search "server lifecycle management" --limit 10
hydra-llm-tldr search "embedded python process"
```

### Step 3: Analyze Impact (Before Modifying)

If modifying existing files, check what depends on them:

```bash
hydra-llm-tldr impact <file-path> --depth 2
```

**Example:**
```bash
hydra-llm-tldr impact cmd/hydra/main.go --depth 2
```

### Step 4: Document Your Findings

After discovering files, save them to the bead for reference:

```bash
br note add <bead-id> "Files: <list discovered files>"
```

**Example:**
```bash
br note add hydra-1e8 "Files: cmd/hydra/main.go (modify), internal/hindsight/server.go (create)"
```

## Output Format

Document discovered files in your implementation summary:

```markdown
## Files Discovered and Used
- **Existing:** path/to/existing.go (modified)
- **New:** path/to/new.go (created)
- **Confidence:** High/Medium/Low
```

## Best Practices

1. **Always run hindsight search first** - Learn from past patterns
2. **Search multiple queries** - Try different descriptions of the same task
3. **Check 3-5 results** - Don't rely on the first result alone
4. **Save findings** - Document which files you actually used
5. **Be specific** - Use bead title and acceptance criteria in searches

## Related Skills

- **code-search-and-intelligence** - Underlying search functionality
- **pattern-quality-reviewer** - Save discovered patterns after completion
- **lsp-code-analysis** - Navigate code with LSP

## Quick Reference

```bash
# 1. Load required skill
@skill_use skill_names=["code-search-and-intelligence"]

# 2. Query patterns
hindsight search "<task>" --limit 5

# 3. Search code
hydra-llm-tldr search "<query>" --limit 10

# 4. Check impact
hydra-llm-tldr impact <file> --depth 2

# 5. Save findings
br note add <bead-id> "Files: ..."
```
