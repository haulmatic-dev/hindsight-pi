# Validation Stub Detection

## Why this document exists

In a 2026-06 audit of the `srv-file-handler` migration, 69 of 121
beads (57%) closed with `close_reason=failed`, including 13 GREEN
beads whose target files contained stub patterns (`return nil, nil`,
placeholder comments, discarded parameters). The system reported
the project "complete" because four independent validation paths all
failed open simultaneously:

| Path | Failure mode | Fix |
|---|---|---|
| `IsEndOfChain` skipped GREEN/RED/REFACTOR beads | TDD phase beads never validated | Any bead with `target_files` is now a validation point |
| `BeadCloser` checked `bead.Type == "refactor"` | Dead check (real beads have `Type="task"`) | Check labels (`tdd-red/green/refactor/syntax`) |
| TDD validator only checked `file.Size() == 0` | 2.5KB stub passes | Added stub_detector with AST-based pattern matching |
| Static checker only matched `panic("not implemented")` | `return nil, nil` slips through | DetectStubs covers empty bodies, discard params, placeholder comments |
| Agent verifier disabled + endpoint never deployed | LLM semantic check off | Config gate; if disabled, static checks must catch everything |
| `close_reason=failed` not a gate | 57% failed closures blocked nothing | `close_blocked_on_failed` (default true) creates fix beads |

This document describes the new behavior. Read it before writing or
auditing a TDD bead.

## What's new

### `IsEndOfChain` contract change

**Before**: Returns true only for beads without TDD phase labels.
GREEN/RED/REFACTOR/SYNTAX beads were exempt.

**After**: Returns true for any bead with `target_files` metadata
that is in a terminal-or-closable status. The TDD phase determines
which checks run, not whether any checks run.

A companion function `TDDPhase(bead)` returns `"red" | "green" |
"refactor" | "syntax" | ""` for callers that want to branch on phase.

### Stub detection beyond panic stubs

`internal/validation/stub_detector.go` parses Go source with
`go/ast` and flags three patterns:

1. **Empty body**: function with a single `return` of zero values
   (`return nil, nil`, `return 0, nil`, `return "", nil`).
2. **Discard parameters**: every non-blank parameter bound to `_`
   via `_ = x` discard statements.
3. **Placeholder comment**: function's doc comment contains
   keywords like "real implementation", "follow-up", "placeholder",
   "todo:", "fixme:", "stub:", "mock implementation", "temporary
   implementation".

Test functions (`Test*`) and underscore-prefixed functions are
ignored — those are conventional names for stubs we don't want to
flag.

### `close_blocked_on_failed` (config)

```yaml
validation:
  close_blocked_on_failed: true   # default
```

When true, a bead that closes with `close_reason=failed` (worker
exited non-zero, retry exhaustion, etc.) triggers `BeadCloser` to
create an auto-fix bead:

- Title: `Fix: <original title>`
- Type: `fix`
- Labels: `auto-fix`, `failed-closure`
- Dependencies: `[original_bead_id]`

The fix bead blocks the original's dependents, so downstream beads
don't unblock until the fix bead closes.

### `block_stub_implementations` (config)

```yaml
validation:
  block_stub_implementations: true   # default
```

Currently informational — the static checker always runs stub
detection. This flag is reserved for future use when an operator
wants to bypass stub detection (e.g., during framework bootstrapping).

## How to verify the fix works

### Unit tests

```bash
go test ./internal/validation/...
```

Key tests:

- `TestIsEndOfChain/GREEN_phase_bead_with_target_files` — confirms
  GREEN beads are now validation points.
- `TestTDDPhase` — confirms the new label-based phase detector.
- `TestDetectStubs_EmptyBody`, `TestDetectStubs_DiscardParams`,
  `TestDetectStubs_PlaceholderComment` — confirm the new detector
  catches each stub pattern.
- `TestCloseBeadWithValidation_BlocksStubImplementation` — the
  regression test that confirms `Activities.GetThing` with
  `return "", nil` and a placeholder comment is now rejected.

### Integration test against a stub-prone requirement

To verify against a real workflow, run a GREEN bead whose target
file deliberately contains a stub, and confirm:

1. The bead is NOT closed.
2. A fix bead is created via `br list --all`.
3. The fix bead blocks the original's dependents.

```bash
# Re-open a previously-closed GREEN bead whose implementation was
# a stub:
br reopen <bead_id>

# Confirm the validator blocks the next close attempt:
br close <bead_id> -r "completed"
# Expected output:
#   VALIDATION FAILED - Bead NOT closed:
#     [CRITICAL] Stub pattern (empty-body) in Activities.GetThing: ...
```

## Why we still need agent verification

The static checks above catch structural stubs. They do NOT catch
semantic gaps where the code compiles, runs, and looks like an
implementation but does the wrong thing. Examples:

- A function that calls OpenAI but ignores the response and returns
  nil.
- A function that writes to the wrong database.
- A function that calls an HTTP endpoint with the wrong payload
  shape.

For semantic validation, `agent_verification_enabled: true` is
required. The agent verifier sends the bead's target files + AC
description to an LLM and asks "does this code satisfy these ACs?".

Two prerequisites for agent verification:

1. `validation.agent_verification_enabled: true` in `hydra.yaml`.
2. An endpoint reachable at `validation.agent_endpoint` (default
   `http://localhost:8080/verify`). In production this is
   `http://localhost:8888/verify` (the hindsight coding-brain
   server).

If either prerequisite is missing, semantic checks do not run, and
only the static checks can catch stubs.

## What this fix does NOT address

- **PRD quality**: this fix detects stubs in the code that gets
  written. It does not catch ambiguous or unmeasurable ACs in the
  PRD that let the agent write "the right kind of code that does the
  wrong thing". PRD quality is a separate concern addressed by
  `prd-quality-audit.md`.
- **Process compliance**: the compliance gate's self-healing loop
  (creating gap beads when contracts aren't met) is still largely
  descriptive text. A follow-up bead should make the loop
  programmatic.
- **Worker timeouts**: a worker that hangs for the full 10-minute
  default still closes as `failed` and now creates a fix bead. That
  is correct behaviour, but the underlying worker timeout should be
  tuned per workload class.

## Migration guide for existing tests

If you have a test that asserts `IsEndOfChain(greenBead) == false`,
update it. The new contract is "any bead with target files is a
validation point". For convenience, `TDDPhase(bead)` returns the
phase string when callers want to branch on it.

If you have a test that asserts `BeadCloser.Close` always closes the
bead regardless of success, add a check for `close_blocked_on_failed`
on/off. With the flag on, a failed close creates a fix bead (visible
via `br list --all`).