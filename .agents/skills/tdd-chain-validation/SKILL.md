---
name: tdd-chain-validation
description: "Validates complete TDD (Test-Driven Development) chains atomically, ensuring all phases (RED → GREEN → REFACTOR → SYNTAX) are properly implemented before marking a feature as complete."
---

# TDD Chain Validation Skill

## Overview

Validates complete TDD (Test-Driven Development) chains atomically, ensuring all phases (RED → GREEN → REFACTOR → SYNTAX) are properly implemented before marking a feature as complete.

## Purpose

This skill performs comprehensive validation of TDD chains to:
- Verify tests were written comprehensively (RED phase)
- Confirm implementation satisfies tests and acceptance criteria (GREEN phase)
- Ensure code quality improvements without functional changes (REFACTOR phase)
- Validate all quality gates pass (SYNTAX phase)
- Provide detailed feedback on any failures for targeted rework

## When to Use

Use this skill when:
- A TDD chain has reached its end (all phases completed)
- You need to verify atomic completion of a feature
- Quality gates need comprehensive checking
- Preventing "phantom completions" where beads are marked done but work is incomplete

## Input Format

The skill expects a JSON input with the complete TDD chain context:

```json
{
  "chain_id": "feature-xyz",
  "feature_name": "User Authentication",
  "acceptance_criteria": [
    "User can register with email and password",
    "Password must be at least 8 characters"
  ],
  "phases": {
    "red": {
      "bead_id": "abc123",
      "title": "RED: Write tests for User Authentication",
      "description": "...",
      "test_files": [
        {"path": "tests/auth.test.ts", "content": "..."}
      ],
      "status": "completed"
    },
    "green": {
      "bead_id": "def456",
      "title": "GREEN: Implement User Authentication",
      "description": "...",
      "source_files": [
        {"path": "src/auth.ts", "content": "..."}
      ],
      "test_files": [
        {"path": "tests/auth.test.ts", "content": "..."}
      ],
      "status": "completed"
    },
    "refactor": {
      "bead_id": "ghi789",
      "title": "REFACTOR: Optimize User Authentication",
      "description": "...",
      "source_files": [
        {"path": "src/auth.ts", "content": "..."}
      ],
      "changes_description": "Extracted validation logic",
      "status": "completed"
    },
    "syntax": {
      "bead_id": "jkl012",
      "title": "SYNTAX: Check User Authentication",
      "description": "...",
      "lint_results": "No issues found",
      "status": "completed"
    }
  }
}
```

## Validation Criteria

### RED Phase Validation
- [ ] Test files exist and are not empty
- [ ] Tests cover all acceptance criteria
- [ ] Tests use AAA pattern (Arrange-Act-Assert)
- [ ] Tests are comprehensive (edge cases, error cases)
- [ ] No implementation code mixed in test files
- [ ] Test descriptions are clear and meaningful

### GREEN Phase Validation
- [ ] Implementation files exist and are not empty
- [ ] All tests pass (verified by test run)
- [ ] Implementation satisfies acceptance criteria
- [ ] Code is minimal (YAGNI - no premature features)
- [ ] No obvious bugs or issues
- [ ] Error handling implemented where specified

### REFACTOR Phase Validation
- [ ] Code quality metrics improved (if measurable)
- [ ] All tests still pass after refactoring
- [ ] No functional changes introduced
- [ ] Code is more readable/maintainable
- [ ] Design patterns applied appropriately
- [ ] Technical debt reduced or documented

### SYNTAX Phase Validation
- [ ] Linting passes with no errors
- [ ] Code formatting is consistent
- [ ] Type checking passes (if TypeScript)
- [ ] No debug code left (console.log, etc.)
- [ ] Comments are meaningful (not redundant)
- [ ] Code follows project conventions

## Output Format

Return a JSON response with comprehensive validation results:

```json
{
  "chain_valid": false,
  "overall_score": 0.75,
  "phase_results": {
    "red": {
      "passed": true,
      "score": 0.95,
      "issues": [],
      "warnings": ["Could add more edge case tests"]
    },
    "green": {
      "passed": false,
      "score": 0.60,
      "issues": [
        {
          "severity": "critical",
          "message": "Missing error handling for invalid email format",
          "file": "src/auth.ts",
          "line": 15,
          "criterion": "Password must be at least 8 characters"
        },
        {
          "severity": "error",
          "message": "Implementation does not validate password length",
          "file": "src/auth.ts",
          "line": 23
        }
      ],
      "warnings": []
    },
    "refactor": {
      "passed": true,
      "score": 0.90,
      "issues": [],
      "warnings": ["Consider extracting more helper functions"]
    },
    "syntax": {
      "passed": true,
      "score": 1.0,
      "issues": [],
      "warnings": []
    }
  },
  "critical_issues": [
    "GREEN phase failed: Missing error handling for invalid email format"
  ],
  "upstream_failure": "green",
  "reopen_from": "green",
  "summary": "The GREEN phase has critical issues that need to be addressed. The implementation is missing required error handling and does not validate password length as specified in the acceptance criteria.",
  "suggestions": [
    "Add input validation for email format using regex",
    "Implement password length check before processing",
    "Add unit tests for the validation logic"
  ]
}
```

## Failure Handling

When validation fails:
1. Identify the **upstream-most failed phase** (closest to RED)
2. Report **all issues** across all phases (not just first failure)
3. Provide **specific, actionable feedback** for each issue
4. Suggest **reopening from the failed phase** through end of chain
5. Include **acceptance criteria mapping** to show what's not met

## Example Scenarios

### Scenario 1: GREEN Implementation Incomplete
```
RED: ✅ Tests written
GREEN: ❌ Missing validation logic
REFACTOR: ⏭️ Not validated (upstream failure)
SYNTAX: ⏭️ Not validated (upstream failure)

Action: Reopen GREEN, REFACTOR, SYNTAX
Feedback: "Implementation missing password validation (see acceptance criteria #2)"
```

### Scenario 2: Tests Don't Cover Criteria
```
RED: ❌ Tests missing error case coverage
GREEN: ✅ Implementation good
REFACTOR: ✅ Quality improved
SYNTAX: ✅ Clean code

Action: Reopen RED through SYNTAX
Feedback: "Tests must cover: invalid email, short password, missing fields"
```

### Scenario 3: Refactoring Changed Behavior
```
RED: ✅ Tests comprehensive
GREEN: ✅ Implementation correct
REFACTOR: ❌ Changed return type (breaking change)
SYNTAX: ⏭️ Not validated

Action: Reopen REFACTOR and SYNTAX
Feedback: "Refactoring must not change API. Return type changed from User to AuthResult"
```

## Best Practices

1. **Be Thorough**: Check all criteria, not just the obvious ones
2. **Be Specific**: Reference exact files, lines, and criteria
3. **Be Constructive**: Provide clear guidance on how to fix
4. **Be Consistent**: Apply the same standards across all validations
5. **Be Context-Aware**: Consider the specific technology and project conventions

## Integration

This skill is invoked by Hydra's validation system when:
- A TDD chain reaches its end (all phases completed)
- The final bead has no dependents
- Configuration `validation-agent.enabled: true`

The validation runs atomically - the entire chain passes or fails together.
