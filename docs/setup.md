# Hydra Setup

> 📖 Next: [User Guide](guides/user-guide.md) — configure your projects.
>
> 🛠 [Admin Guide](guides/admin-guide.md) — run Hydra as a service.

## PATH Configuration

Add Hydra's bin directory to your PATH **before** other directories:

```bash
# Add to ~/.zshrc or ~/.bashrc
export PATH="/Users/buddhi/source-code/personal/hydra/bin:$PATH"
```

This ensures that Hydra's wrapper scripts (for logging) are found before the actual binaries:
- `cm` → `cm-wrapper.js` (logs then calls real cm)
- `llm-tldr` → `llm-tldr-wrapper.js` (logs then calls real llm-tldr)

## Wrapper Scripts

The wrapper scripts in `bin/`:
1. Log all commands to `/tmp/hydra-logs/`
2. Execute the actual binary
3. Log responses and exit codes

This provides complete visibility into cass_memory and TLDR usage without modifying Pi agent behavior.
