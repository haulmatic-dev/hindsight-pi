# Hydra Agent Prompt Templates

This directory contains customizable prompt templates for Hydra's AI agents.

## Available Templates

| File | Agent Type | Description |
|------|-----------|-------------|
| `pi-agent.md` | Pi Agent | Full-featured template with dynamic skills listing |
| `default.md` | Fallback | Used when agent-specific template not found |

## Template Variables

Use Go template syntax `{{.VariableName}}`:

| Variable | Type | Description | Example |
|----------|------|-------------|---------|
| `{{.Title}}` | string | Bead title | "Implement auth" |
| `{{.Description}}` | string | Bead description | "Add JWT login" |
| `{{.Type}}` | string | Bead type | "implementation" |
| `{{.Skills}}` | array | Available skills | [golang-pro, ...] |
| `{{.Files}}` | array | Target files | [auth.go, ...] |
| `{{.Context}}` | string | Additional context | Pattern matches |
| `{{.Project}}` | string | Project name | "hydra" |
| `{{.BeadID}}` | string | Unique bead ID | "bead-123" |

## Loop Examples

List all skills:
```
{{range .Skills}}
- **{{.Name}}**: {{.Description}}
{{end}}
```

List target files:
```
{{range .Files}}
- {{.}}
{{end}}
```

## Conditional Content

Show section only if files exist:
```
{{if .Files}}
## Files to Modify
{{range .Files}}
- {{.}}
{{end}}
{{end}}
```

## Customization Tips

1. **Restart Required**: Changes take effect on Hydra restart (unless hot_reload: true)
2. **Syntax Check**: Validate your template syntax
3. **Backup**: Keep a backup before major changes
4. **A/B Testing**: Create `pi-agent-variant-a.md` and update config to test

## Troubleshooting

- **Template not found**: Falls back to hardcoded default
- **Syntax error**: Logged to console, uses default template
- **Missing variable**: Renders as `<no value>` - use `{{.Var | default "fallback"}}`
