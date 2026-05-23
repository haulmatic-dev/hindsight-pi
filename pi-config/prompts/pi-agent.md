# Task: {{.Title}}

## Description
{{.Description}}

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
