# Collective Unconscious - User Guide

**What it is** | **How it works** | **Why it matters**

---

## What is Collective Unconscious?

The Collective Unconscious is Hydra's cross-project pattern learning system. Think of it as an institutional memory that grows smarter with every task completed.

When your agents finish beads successfully, the system automatically:
- **Extracts** reusable patterns from the solution
- **Validates** patterns through quality gates  
- **Stores** approved patterns in a shared knowledge base
- **Matches** relevant patterns to new beads automatically
- **Injects** patterns into agent prompts for context-aware execution

## How It Works

### 1. Pattern Extraction
When a bead completes successfully:
```
Worker Success → Quality Gates → Pattern Abstraction → Storage
```

The system analyzes:
- What files were changed
- How the problem was solved
- Testing approaches used
- Design patterns applied

### 2. Quality Validation
Before patterns enter the knowledge base, they pass through quality gates:

| Gate | Purpose |
|------|---------|
| Static Analysis | Code quality and best practices |
| Test Coverage | Validation thoroughness |
| Security Scan | No vulnerable patterns propagated |
| Documentation | Clear explanations and examples |
| Review Score | Community validation |

Patterns scoring >0.85 are auto-approved. Patterns <0.50 are discarded.

### 3. Pattern Matching
When new beads are created, the system finds relevant patterns:

```
New Bead → Semantic Analysis → Pattern Ranking → Top Matches → Agent Prompt
```

Matching considers:
- **Task similarity** (what you're building)
- **Technology match** (language, framework)
- **Historical success** (how well the pattern worked)
- **Recency** (prefer recent solutions)

### 4. Automatic Injection
Relevant patterns are automatically injected into agent prompts:

```
## Relevant Patterns from Previous Work:
1. Database Migration Best Practices (confidence: 0.95)
   Use transactions, test rollbacks, version control...

## Task:
Create a migration for the new user table...
```

## Benefits

### For Developers
- **Faster Delivery**: Agents start with proven approaches
- **Consistency**: Same patterns used across projects
- **Knowledge Sharing**: Solutions spread organically
- **Quality**: Only validated, high-quality patterns propagate

### For Organizations
- **Institutional Memory**: Knowledge survives team changes
- **Best Practice Enforcement**: Good patterns become defaults
- **Reduced Onboarding**: New projects benefit from existing wisdom
- **Compounding Returns**: Every solved problem makes the system smarter

## Real-World Results

Based on E2E testing with Hydra orchestrator:

| Metric | Result |
|--------|--------|
| Successful Bead Completions | 16 |
| Pattern Extraction Infrastructure | ✅ Operational |
| Quality Gate Validation | ✅ Working |
| Pattern Matching Performance | <100ms (cached) |
| Cross-Project Pattern Sharing | ✅ Enabled |

## Configuration

Enable Collective Unconscious in your `hydra.yaml`:

```yaml
intelligence:
  cass_memory:
    enabled: true
    query_limit: 10
    history_days: 30
  
  tldr:
    enabled: true
    max_files: 50
  
  gptcache:
    enabled: true
```

Patterns are automatically extracted and injected—no manual configuration needed.

## Pattern Review

High-quality patterns (score >0.90) are auto-approved. Others enter a review queue where you can:
- **Approve** good patterns for organization-wide use
- **Reject** low-quality or project-specific patterns
- **Annotate** patterns with additional context

## Privacy & Security

- Patterns never include sensitive data (passwords, API keys)
- Source code is abstracted to show approaches, not implementations
- Cross-project sharing respects access controls
- All patterns pass security scanning before storage

## Monitoring

Track Collective Unconscious effectiveness:

```bash
# View pattern statistics
br list --patterns

# Check pattern matches for a bead
br show <bead-id> --patterns

# Review pending patterns
bv review-patterns
```

## Troubleshooting

**No patterns injected?**
- Ensure `cass_memory` is enabled in config
- Check bead has similar completed beads in the system
- Patterns require 2-minute settlement delay after completion

**Low-quality pattern suggestions?**
- Quality gates may be too strict; adjust thresholds
- More completed beads improve pattern diversity
- Consider manual review for borderline patterns

## Best Practices

1. **Let it Learn**: Don't manually curate—let the system extract organically
2. **Review Periodically**: Check pending patterns weekly
3. **Share Freely**: Patterns help everyone—don't hoard knowledge
4. **Trust but Verify**: Auto-approved patterns are validated; review flagged ones
5. **Measure Impact**: Track time-to-completion improvements over time

## Further Reading

- [Architecture Deep Dive](../architecture/agent-connector-20260311.md)
- [E2E Test Results](./test-results/E2E_TEST_RESULTS_20260311.md)
- [PRD: Technical Specification](./prd/collective-unconscious-20260310-prd.md)
- [Configuration Guide](../../operations/configuration-guide-20260309.md)

---

**The Collective Unconscious transforms individual successes into organizational intelligence. Every bead you complete makes every future agent smarter.**
