# Bead Validation System

## Overview

The Bead Validation System is a **hybrid validation framework** that automatically validates bead implementations before closure to prevent "phantom completions" - where beads are marked complete but contain only stub implementations or missing tests.

## Problem Solved

During large-scale implementations (like the Hindsight Integration with 392 beads), agents may:
- Write `panic("not implemented")` stubs instead of actual code
- Skip test creation in RED phase
- Mark beads complete without proper validation
- Fail due to LLM timeouts or rate limiting

**Impact:** Closed beads with incomplete implementations that block future work and create technical debt.

## Solution: End-of-Chain Validation

The system validates beads **only at the end of the TDD chain** (final implementation beads), not during intermediate RED/GREEN/REFACTOR phases.

```
TDD Chain: RED → GREEN → REFACTOR → IMPLEMENTATION → [VALIDATION] → CLOSE
                                                       ↑
                                                 Validates here only
```

## Architecture

### Components

```
┌─────────────────────────────────────────────────────────────┐
│                   Validation Pipeline                       │
├─────────────────────────────────────────────────────────────┤
│ 1. End-of-Chain Detection                                   │
│    └─> Check: No TDD labels (tdd-red, tdd-green, etc.)     │
│    └─> Check: Has target_files metadata                     │
│                                                             │
│ 2. Static Validation (Fast)                                 │
│    ├─> File existence check                                │
│    ├─> Panic stub detection (AST parsing)                  │
│    ├─> Test file existence                                 │
│    ├─> Compilation verification                            │
│    └─> Test execution                                      │
│                                                             │
│ 3. Agent Verification (Deep) [Optional]                     │
│    ├─> Send code to AI agent                               │
│    ├─> Check acceptance criteria                           │
│    └─> Semantic validation                                 │
│                                                             │
│ 4. Closure Decision                                         │
│    └─> Block if any validation fails                       │
└─────────────────────────────────────────────────────────────┘
```

### Code Structure

```
internal/validation/
├── validator.go         # Core orchestrator
├── static_checker.go    # AST-based static analysis
├── agent_verifier.go    # AI agent semantic validation
├── integration.go       # Overseer integration
└── validator_test.go    # Test suite
```

## Configuration

### Default Configuration

```go
validation.ValidationConfig{
    Enabled:                  true,              // Master switch
    StaticChecksEnabled:      true,              // Enable static analysis
    AgentVerificationEnabled: true,              // Enable agent verification
    MinCoverage:              80.0,              // Minimum test coverage %
    Timeout:                  30 * time.Second,  // Validation timeout
    AgentEndpoint:            "http://localhost:8080/verify",
}
```

### Hydra YAML Configuration

Add to your `hydra.yaml`:

```yaml
validation:
  enabled: true
  static_checks: true
  agent_verification: true
  min_coverage: 80.0
  timeout: 30s
  agent_endpoint: "http://localhost:8080/verify"
```

## Validation Rules

### Static Checks (Always Run)

| Check | Description | Severity |
|-------|-------------|----------|
| **File Existence** | Target files must exist | CRITICAL |
| **Panic Stub Detection** | No `panic("not implemented")` allowed | CRITICAL |
| **Test File Existence** | Corresponding `_test.go` must exist | CRITICAL |
| **Compilation** | Code must compile with `go build` | CRITICAL |
| **Test Execution** | All tests must pass | CRITICAL |

### Agent Verification (Conditional)

Triggered for:
- Beads with labels: `architecture`, `security`, `performance`, `consensus`
- Beads with > 3 target files
- Complex implementations

Checks:
- Acceptance criteria satisfaction
- Code quality assessment
- Architecture pattern compliance
- Security best practices

## Usage

### Automatic Validation

Validation runs automatically when closing implementation beads:

```bash
br close hydra-abc123 -r "completed"
```

If validation fails, you'll see:

```
❌ VALIDATION FAILED - Bead NOT closed:

Duration: 2.3s
Static Checks: false

Errors:
  [CRITICAL] Found panic("not implemented") stub: NewServer (server.go:43)
  [CRITICAL] Found panic("not implemented") stub: Start (server.go:49)
  [CRITICAL] Missing test file: internal/hindsight/server_test.go
  [ERROR] Compilation failed: undefined: Server

Action Required:
  1. Implement actual functionality (remove panic stubs)
  2. Create missing test file
  3. Ensure code compiles
  4. Run tests to verify
```

### Manual Validation

You can run validation manually during development:

```go
import "github.com/haulmatic-dev/hydra/internal/validation"

// Create validator
validator := validation.NewValidator(validation.DefaultValidationConfig())

// Run quick static checks
result, err := validator.ValidateQuick(ctx, bead, projectPath)

// Run full validation (static + agent)
result, err := validator.Validate(ctx, bead, projectPath)

// Check results
if !result.Passed {
    fmt.Println(result.FormatResult())
}
```

## Integration Points

### 1. Overseer CloseBead

The validation is integrated into `Overseer.CloseBead()`:

```go
func (o *Overseer) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
    if o.validator != nil {
        return validation.CloseBeadWithValidation(
            o.beadsClient,
            o.validator,
            projectPath, repoName, beadID, reason,
        )
    }
    return o.beadsClient.CloseBead(projectPath, repoName, beadID, reason)
}
```

### 2. Beads CLI

The `br close` command automatically triggers validation for end-of-chain beads.

### 3. Worker Completion

When workers complete beads, validation runs before marking the bead closed.

## Performance

### Benchmarks

| Operation | Target Time | Notes |
|-----------|-------------|-------|
| File existence | <10ms | Direct FS check |
| AST parsing | <100ms | Depends on file size |
| Compilation | <2s | Incremental builds |
| Test execution | <10s | Parallel execution |
| **Total static** | **<15s** | For typical bead |
| Agent verification | 5-30s | Depends on complexity |

### Optimizations

1. **Parallel Execution**: Independent checks run in parallel
2. **Caching**: Results cached for unchanged files
3. **Incremental**: Only re-validate changed files
4. **Lazy Loading**: Agent verification skipped for simple beads

## Error Handling

### Validation Errors

When validation fails:
1. Bead status remains unchanged (not closed)
2. Detailed error report provided
3. Specific file/line information included
4. Actionable fix instructions given

### Retry Strategy

If validation fails due to transient issues:
1. Fix the reported issues
2. Re-run `br close`
3. Validation re-executes
4. Bead closes only when all checks pass

## Testing

### Run Tests

```bash
# Run all validation tests
go test ./internal/validation/... -v

# Run specific test
go test ./internal/validation/... -run TestIsEndOfChain -v

# Run with coverage
go test ./internal/validation/... -cover
```

### Test Coverage

The test suite covers:
- End-of-chain detection
- Static validation rules
- Agent verification protocol
- Integration with overseer
- Error message formatting

## Troubleshooting

### Common Issues

**Issue:** Validation takes too long
- **Solution:** Increase timeout in config or disable agent verification for simple beads

**Issue:** Agent verification fails with timeout
- **Solution:** Check agent endpoint is running or disable agent verification

**Issue:** Tests fail but bead should be valid
- **Solution:** Check test coverage threshold or adjust `min_coverage` config

**Issue:** False positives on panic stubs
- **Solution:** Panic stubs are intentional in some cases - refactor to use proper error handling

### Debug Mode

Enable debug logging:

```go
validator := validation.NewValidator(validation.ValidationConfig{
    Enabled: true,
    // ... other config
})

// Check detailed results
result, _ := validator.Validate(ctx, bead, projectPath)
for _, err := range result.Errors {
    log.Printf("[%s] %s (%s:%d)", err.Severity, err.Message, err.File, err.Line)
}
```

## Future Enhancements

### Planned Features

1. **Custom Validation Rules**: Project-specific validation rules
2. **Historical Tracking**: Track validation trends over time
3. **ML-Based Detection**: Use ML to detect incomplete implementations
4. **Integration with cass_memory**: Store validation patterns
5. **Pre-validation Hooks**: Run custom checks before standard validation

### Configuration Extensions

```yaml
validation:
  custom_rules:
    - name: "no_todo_comments"
      pattern: "TODO|FIXME"
      severity: "warning"
    - name: "required_docs"
      files: ["README.md", "API.md"]
      severity: "error"
  
  skip_patterns:
    - "*.generated.go"
    - "vendor/**"
  
  agent:
    model: "gpt-4"
    temperature: 0.3
    max_tokens: 2000
```

## Best Practices

### For Agent Developers

1. **Write tests first** (RED phase) - Don't skip to implementation
2. **Remove all panic stubs** before closing - Use proper error handling
3. **Ensure compilation** - Run `go build` locally before closing
4. **Maintain coverage** - Keep test coverage above 80%
5. **Follow TDD** - Complete each phase before moving to next

### For Project Maintainers

1. **Enable validation** in production environments
2. **Monitor validation metrics** - Track failure rates and patterns
3. **Adjust thresholds** based on project needs
4. **Review agent verification results** - Use for quality improvement
5. **Document exceptions** - Some beads may need validation disabled

## Related Documentation

- [Bead Processing Flow](./bead-processing-flow-20260309.md)
- [Bead Structure Guide](./bead-structure-guide.md)
- [Worker Lifecycle](./worker-lifecycle-20260309.md)
- [Configuration Guide](../operations/configuration-guide-20260309.md)

## References

- **Package**: `github.com/haulmatic-dev/hydra/internal/validation`
- **Key Types**: `Validator`, `ValidationResult`, `ValidationError`
- **Entry Point**: `CloseBeadWithValidation()`
- **Configuration**: `ValidationConfig`

---

*Last updated: 2026-03-20*
*Version: 1.0*
*Status: Production Ready*
