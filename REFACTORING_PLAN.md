# Configuration Management Refactoring Plan

## Current State Analysis

The `config/intelligence.go` file has 380+ lines with significant duplication:

### Issues Identified

1. **Validate() method duplication**: Each field validates positive duration/values with nearly identical error messages
2. **Timeout validation pattern repeated**: Same pattern for recall, retain, reflect
3. **Check methods share common patterns**: All check methods check if enabled first
4. **Getter methods have fallback logic duplication**: Same nil/disabled check pattern

### Refactoring Strategy

#### 1. Extract Duration Validation Helper
```go
func validatePositiveDuration(d time.Duration, field string) error
func validateTimeout(timeout, limit time.Duration, field string) error
func validatePositiveInt(n int, field string) error
```

#### 2. Extract Common Check Logic
```go
func (c *PerformanceConfig) checkEnabled() bool
```

#### 3. Simplify Validate Method
Use helper functions to reduce repetition in validation logic

#### 4. Consolidate Timeout/Latency Relationships
Use a helper for the common timeout >= latency validation pattern

## Validation Tests Required

Since there are no existing tests in config/ package, tests will be added to ensure:
- Default config passes validation
- Zero/negative values fail validation
- Timeout < latency fails validation
- Disabled config skips validation
- Check methods work correctly with enabled/disabled states
