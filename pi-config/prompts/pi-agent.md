# Task: {{.Title}}

## Description
{{.Description}}

## HARD RULES — read before doing anything

These rules are non-negotiable. The hydra validator checks every target file for stub patterns and will block your bead's closure if any rule is violated.

1. **Do not write stubs.** A stub is any function whose body is empty, returns only zero values (`return nil, nil`, `return 0, nil`, `return "", nil`), or contains a placeholder comment like `// Real implementation...`, `// TODO`, `// FIXME`, `// follow-up bead`, `// stub`. If you cannot fully implement, leave the bead OPEN and report the blocker to the user — do not close a stub as "done".

2. **Do not discard parameters.** If the function signature has parameters, the body must use them. Patterns like `func Foo(_ context.Context, id string) { _ = id; return ..., nil }` are detected as stubs.

3. **Real I/O required when the AC implies it.** If the parent AC says "calls the OpenAI API" / "fetches from GCS" / "writes to MongoDB", the function body MUST include the actual call. A stub that returns nil and skips the call is a regression, not a completion. Read the AC in the description above.

4. **Run the verification command.** If the bead description includes a `test_command` or `build_command`, run it and confirm exit code 0 before reporting success. If the command fails, do not close — fix the failure or leave the bead open.

5. **No premature closure.** The hydra validator inspects every target file with go/ast. Comments cannot hide stubs. `_ = x` patterns cannot hide stubs. Returning `nil, nil` cannot hide stubs. Write real code or stop.

## Instructions
Please complete this task based on the description above.
Do not infer technology stack, file structure, or implementation details from the task title alone.
When finished, ensure all files are saved and the solution is complete.

## Available Skills
You have access to the following skills to help with implementation:

{{range .Skills}}
- **{{.Name}}**: {{.Description}}
{{end}}

**Use these skills as needed based on your judgment.**

{{if .Files}}
## Files to Modify
{{range .Files}}
- {{.}}
{{end}}
{{end}}

{{if .Context}}
## Additional Context
{{.Context}}
{{end}}
