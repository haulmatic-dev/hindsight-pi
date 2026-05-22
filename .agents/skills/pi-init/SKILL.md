---
name: pi-init
description: Initialize pi workspace with project configuration, git setup, and tool verification. Use when starting a new project, setting up a repository for pi, or verifying tool installation.
---

# Pi Init Skill

## Purpose

Initialize a project workspace for pi with:
- Project structure setup (`.pi/` directory)
- Git repository initialization (if needed)
- Tool verification and installation guidance
- Optional beads task tracking setup
- Configuration validation

## When to Activate

- User says "initialize this project for pi"
- User says "setup pi workspace"
- User says "pi init" or "initialize pi"
- Starting work in a new repository
- Setting up task tracking (beads)

## Phase 1: Environment Check

Check current environment and tools:

```bash
# Check Node.js and pi
node --version
pi --version 2>/dev/null || echo "pi not installed globally"

# Check git
git --version
git status 2>/dev/null || echo "Not a git repository"

# Check optional tools
which br 2>/dev/null && br --version || echo "beads (br) not installed"
which bv 2>/dev/null && bv --version || echo "beads-viewer (bv) not installed"

# Check formatting tools
which prettier 2>/dev/null && prettier --version || echo "prettier not installed"
which biome 2>/dev/null && biome --version || echo "biome not installed"
```

## Phase 2: Project Structure Setup

### 2.1 Create Pi Directory Structure

```bash
# Create .pi directory structure
mkdir -p .pi/{skills,agents,extensions,prompts,tasks,sessions}

# Create basic .gitignore for pi
cat > .pi/.gitignore << 'EOF'
# Pi sessions (user-specific)
sessions/
*.log

# Temporary files
.tmp/
*.tmp

# Cache
cache/
EOF
```

### 2.2 Create Project Settings

Create `.pi/settings.json`:

```json
{
  "project": {
    "name": "{{project-name}}",
    "initialized": "{{date}}"
  },
  "skills": [],
  "agents": [],
  "extensions": []
}
```

### 2.3 Create README Template

Create `.pi/README.md`:

```markdown
# Pi Configuration

This directory contains project-specific configuration for the pi coding agent.

## Structure

- `skills/` - Project-specific skills
- `agents/` - Project-specific agent definitions
- `extensions/` - Pi extensions
- `prompts/` - Prompt templates
- `tasks/` - Task state files
- `settings.json` - Project configuration

## Getting Started

1. Install pi: `npm install -g @mariozechner/pi-coding-agent`
2. Run pi in this directory
3. Skills and agents in this folder will be auto-discovered

## Documentation

See [Pi Documentation](https://github.com/badlogic/pi-coding-agent) for details.
```

## Phase 3: Git Setup (if needed)

### 3.1 Initialize Git Repository

If not already a git repo:

```bash
# Initialize git
git init

# Create initial .gitignore if it doesn't exist
if [ ! -f .gitignore ]; then
  cat > .gitignore << 'EOF'
# Dependencies
node_modules/
.pnp
.pnp.js

# Build outputs
dist/
build/
*.tsbuildinfo

# Environment
.env
.env.local
.env.*.local

# IDE
.idea/
.vscode/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db

# Logs
*.log
npm-debug.log*

# Testing
coverage/
.nyc_output/
EOF
fi

# Initial commit
git add .
git commit -m "Initial commit: Project setup"
```

### 3.2 Verify Git Configuration

```bash
# Check git user config
git config user.name || echo "⚠️ git user.name not set"
git config user.email || echo "⚠️ git user.email not set"

# Suggest configuration if missing
if [ -z "$(git config user.name)" ] || [ -z "$(git config user.email)" ]; then
  echo "Run:"
  echo "  git config user.name 'Your Name'"
  echo "  git config user.email 'your.email@example.com'"
fi
```

## Phase 4: Optional Tool Setup

### 4.1 Beads Task Tracking (Optional)

If user wants task tracking:

```bash
# Check if beads is installed
if ! command -v br &> /dev/null; then
  echo "📦 Installing beads..."
  cargo install --git https://github.com/Dicklesworthstone/beads_rust.git
fi

# Initialize beads
br init

# Create initial config if needed
if [ ! -f .beads/config.yaml ]; then
  mkdir -p .beads
  cat > .beads/config.yaml << 'EOF'
# Beads Project Configuration
issue_prefix: bd
default_priority: 2
default_type: task
EOF
fi

echo "✓ Beads initialized"
echo "  Quick commands:"
echo "    br ready       # Show available tasks"
echo "    br create 'Task description'"
echo "    bv --robot-triage  # AI recommendations"
```

### 4.2 Formatting Tools

Check and suggest formatting tools:

```bash
# Check for formatting configuration
if [ -f package.json ]; then
  # Node.js project
  if [ ! -f .prettierrc ] && [ ! -f .prettierrc.json ] && [ ! -f .prettierrc.js ]; then
    echo "💡 Suggestion: Add Prettier configuration"
    echo "  npm install --save-dev prettier"
    echo "  echo '{}' > .prettierrc"
  fi
  
  if [ ! -f biome.json ] && [ ! -f biome.jsonc ]; then
    echo "💡 Suggestion: Consider Biome (faster alternative to ESLint+Prettier)"
    echo "  npm install --save-dev @biomejs/biome"
    echo "  npx @biomejs/biome init"
  fi
fi

# Python project
if [ -f requirements.txt ] || [ -f pyproject.toml ] || [ -f setup.py ]; then
  echo "💡 Python formatting tools:"
  echo "  pip install black ruff"
  echo "  black --init-pyproject  # Add to pyproject.toml"
fi
```

## Phase 5: Summary Report

Provide user with initialization summary:

```
═══════════════════════════════════════════════════════════
              Pi Workspace Initialized ✓
═══════════════════════════════════════════════════════════

📁 Project Structure
  ✓ .pi/ directory created
  ✓ Settings file: .pi/settings.json
  ✓ Git ignore: .pi/.gitignore

🔧 Git Repository
  ✓ Git initialized
  ✓ Initial .gitignore created
  ✓ First commit ready

🛠️  Tool Status
  Node.js: v20.x.x ✓
  Git: 2.x.x ✓
  
  Optional Tools:
  ├── beads (br): {{installed ? '✓' : '○ not installed'}}
  ├── beads-viewer (bv): {{installed ? '✓' : '○ not installed'}}
  ├── prettier: {{installed ? '✓' : '○ not installed'}}
  └── biome: {{installed ? '✓' : '○ not installed'}}

📚 Next Steps
  1. Start pi: pi
  2. Create a PRD: "Create a PRD for [feature]"
  3. Break down tasks: "Break down docs/PRDs/..."

═══════════════════════════════════════════════════════════
```

## Installation Commands Reference

If tools are missing, provide these install commands:

### Required
```bash
# Pi (already assumed to be installed)
npm install -g @mariozechner/pi-coding-agent
```

### Optional but Recommended
```bash
# Beads (task tracking)
cargo install --git https://github.com/Dicklesworthstone/beads_rust.git
curl -fsSL "https://raw.githubusercontent.com/Dicklesworthstone/beads_viewer/main/install.sh" | bash

# Formatting (Node.js projects)
npm install -g prettier
npm install -g @biomejs/biome

# Formatting (Python projects)
pip install black ruff
```

## Idempotent Operation

This skill is safe to run multiple times:
- Won't overwrite existing `.pi/settings.json`
- Won't reinitialize git if already initialized
- Won't duplicate beads initialization
- Updates tool status each run

## Error Handling

| Error | Action |
|-------|--------|
| No Node.js | Error - pi requires Node.js |
| Git not installed | Error - version control required |
| Beads install fails | Warning - optional feature |
| Permission denied | Suggest sudo or user install |

## Integration with Other Skills

After initialization:
- `create-prd` will save PRDs to `docs/PRDs/`
- `task-breakdown` will save task state to `.pi/tasks/`
- `beads` skill will work if beads is installed
