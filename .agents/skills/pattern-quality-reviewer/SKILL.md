---
name: pattern-quality-reviewer
description: Review patterns before saving to institutional knowledge (Hindsight). Assess pattern quality, novelty, and generalizability to ensure only valuable learnings are preserved.
---

# Pattern Quality Reviewer

## Overview

**Purpose:** Review extracted patterns before adding them to Hindsight to ensure quality and value.

**When to use:**
- After completing a bead or chain
- Before calling `hindsight retain` to save a pattern
- When you want to verify a pattern is worth preserving

**Key principle:** Not all solutions should be saved. Only generalizable, high-quality patterns deserve a place in institutional memory.

## Quality Criteria

### ✅ Save-Worthy Patterns

Save patterns that meet these criteria:

1. **Novel Solution**
   - Addresses a problem not covered by existing patterns
   - Offers a new approach or technique
   - Example: "Using circuit breaker for external API resilience"

2. **Generalizable**
   - Applies to similar situations, not just this specific case
   - Has broader applicability beyond current context
   - Example: "Token bucket rate limiting for APIs" (not "Rate limiter for /api/users")

3. **Well-Documented**
   - Clear explanation of what problem it solves
   - Includes context and trade-offs
   - Has concrete examples

4. **Proven Success**
   - Successfully implemented and tested
   - No known issues or edge case failures
   - Can be reliably reproduced

5. **Non-Obvious**
   - Not common knowledge or easily discoverable
   - Captures learned wisdom, not basic facts
   - Example: "Handling NaN comparisons in JavaScript" vs "JavaScript has NaN"

### ❌ Don't Save These

**Avoid saving patterns that are:**

- **Too specific:** Only applies to this exact codebase/structure
- **Basic/obvious:** Common knowledge every developer knows
- **Experimental:** Not fully tested or validated
- **Framework-specific:** Tied to version-specific quirks
- **Workarounds:** Hacks that should be fixed properly instead

## Commands

### Review Pattern Quality

Before saving, assess the pattern against quality criteria.

```bash
# Interactive quality assessment (self-reflection)
# Ask yourself:
# 1. Is this novel? (Not already in Hindsight)
# 2. Is it generalizable? (Useful for similar tasks)
# 3. Is it well-documented? (Clear what problem it solves)
# 4. Is it proven? (Tested and working)
# 5. Is it non-obvious? (Not common knowledge)

# If all 5 criteria are met → Save it
# If 3-4 criteria met → Consider saving with notes
# If <3 criteria met → Don't save
```

### Check for Duplicates

Before saving, verify the pattern doesn't already exist.

```bash
# Search Hindsight for similar patterns
hindsight search "your pattern keywords" --limit 5

# Check for exact matches
grep -i "your pattern" ~/.pi/skills/*/SKILL.md 2>/dev/null || true
```

### Calculate Quality Score

Estimate a quality score (0-100) based on the criteria:

```
Novel:        +20 points (if truly new)
Generalizable: +20 points (if broadly applicable)
Documented:   +20 points (if clear and complete)
Proven:       +20 points (if tested and working)
Non-obvious:  +20 points (if learned wisdom)

Score >= 80: Save immediately
Score 60-79:  Save with notes about limitations
Score < 60:   Don't save
```

## Best Practices

### Review Workflow

**After completing work:**

```bash
# Step 1: Reflect on what you learned
# "What was the key insight from this implementation?"

# Step 2: Check quality criteria
# Ask: Is this novel? Generalizable? Documented? Proven? Non-obvious?

# Step 3: Check for duplicates
hindsight search "your insight" --limit 5

# Step 4: Calculate quality score
# Novel (20) + Generalizable (20) + Documented (20) + Proven (20) + Non-obvious (20) = 100

# Step 5: Decide
# Score >= 80 → Save with full documentation
# Score 60-79 → Save with caveats
# Score < 60 → Don't save (but keep mental note)

# Step 6: If saving, use descriptive title and comprehensive description
hindsight retain "Quality pattern: [title] - [description with context, trade-offs, and examples]" --metadata '{"category": "[appropriate-category]"}'
```

### Pattern Documentation Template

When saving a quality pattern, use this format:

```
Title: [Descriptive, actionable title]

Problem: [What problem does this solve?]

Solution: [How does it solve it?]

Context: [When should you use this? When should you NOT?]

Trade-offs: [What are the pros and cons?]

Example: [Concrete code or usage example]

Category: [testing, security, performance, etc.]
```

### Anti-Patterns

**Record anti-patterns when:**

- You discovered a wrong approach through trial and error
- You fixed a bug that others might encounter
- You found a "gotcha" that wastes time

```bash
# Save anti-pattern
hindsight retain "DON'T: [what not to do]. Instead: [what to do]. Reason: [why]" --metadata '{"category": "anti-pattern"}'
```

## Categories

Choose the appropriate category for your pattern:

| Category | Use For |
|----------|---------|
| `testing` | Test patterns, mocking strategies, assertions |
| `security` | Auth, encryption, vulnerability prevention |
| `performance` | Optimization, caching, concurrency |
| `debugging` | Troubleshooting techniques, common bugs |
| `architecture` | Design patterns, structure, organization |
| `refactoring` | Code improvement techniques |
| `api-design` | API patterns, REST, GraphQL |
| `database` | Schema design, queries, migrations |

## Examples

### Example 1: High-Quality Pattern

```
Task: Implement retry logic with exponential backoff

Reflection: "Discovered that exponential backoff with jitter prevents thundering herd"

Quality Check:
✅ Novel: Not in existing Hindsight
✅ Generalizable: Applies to any external API calls
✅ Documented: Clear explanation of jitter importance
✅ Proven: Tested with 1000 concurrent requests
✅ Non-obvious: Jitter isn't commonly known

Score: 100/100 → SAVE

Save command:
hindsight retain "Use exponential backoff with jitter for retry logic to prevent thundering herd problems. Base delay: 100ms, multiplier: 2, max delay: 30s, add random jitter 0-100ms." --metadata '{"category": "performance", "tags": ["retry", "backoff", "resilience"]}'
```

### Example 2: Low-Quality Pattern (Don't Save)

```
Task: Fix typo in variable name

Reflection: "Renamed 'userNmae' to 'userName'"

Quality Check:
❌ Novel: Not a pattern, just a typo fix
❌ Generalizable: Specific to this codebase
❌ Non-obvious: Everyone knows to check spelling

Score: 0/100 → DON'T SAVE

Instead: Make a mental note to use IDE spell checking
```

### Example 3: Anti-Pattern (Save)

```
Task: Debug race condition in user cache

Reflection: "Race condition occurred because cache wasn't thread-safe"

Anti-Pattern:
hindsight retain "DON'T: Use plain maps for caches in concurrent code without synchronization. Instead: Use sync.RWMutex or concurrent-safe data structures like sync.Map. Reason: Race conditions cause data corruption and are hard to debug." --metadata '{"category": "debugging", "tags": ["concurrency", "cache", "race-condition"]}'

Quality: High value because it prevents future bugs
```

## Decision Flowchart

```
Completed work
    ↓
What did I learn?
    ↓
Is it a novel insight? ──No──→ Don't save
    ↓ Yes
Is it generalizable? ──No──→ Don't save
    ↓ Yes
Is it well-documented? ──No──→ Document better, then save
    ↓ Yes
Is it proven working? ──No──→ Wait for validation
    ↓ Yes
Is it non-obvious? ──No──→ Don't save (common knowledge)
    ↓ Yes
Check for duplicates
    ↓
Similar pattern exists? ──Yes──→ Don't save (reference existing)
    ↓ No
Calculate quality score
    ↓
Score >= 80? ──Yes──→ SAVE with full documentation
    ↓ No
Score >= 60? ──Yes──→ SAVE with caveats
    ↓ No
Don't save (mental note only)
```

## Related Skills

- **code-search-and-intelligence**: Query existing patterns before saving
- **tldr-code-intelligence**: Find related code to verify pattern
- **systematic-debugging**: Document debugging insights worth saving

## Quick Reference

```bash
# Review checklist (run before saving)
echo "Quality Review Checklist:"
echo "□ Novel (not in Hindsight)?"
echo "□ Generalizable (useful beyond this case)?"
echo "□ Documented (clear problem/solution)?"
echo "□ Proven (tested and working)?"
echo "□ Non-obvious (learned wisdom)?"

# Search for duplicates
hindsight search "your pattern keywords" --limit 5

# Save quality pattern
hindsight retain "[Title]: [Problem]. Solution: [How]. Context: [When]. Trade-offs: [Pros/Cons]. Example: [Code]." --metadata '{"category": "[category]"}'

# Save anti-pattern
hindsight retain "DON'T: [what not to do]. Instead: [what to do]. Reason: [why]." --metadata '{"category": "anti-pattern"}'
```
