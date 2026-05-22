---
name: hydra-worktree-setup
description: Automatically set up dependencies in Hydra DRYAD worktrees. Detects project type and runs appropriate package manager install. Supports Node.js (pnpm/npm/yarn), Rust (cargo), Go (go mod), Python (pip/poetry), Java (maven/gradle), Ruby (bundler), PHP (composer).
---

# Hydra Worktree Setup

## Purpose

Automatically detect project type and install dependencies in a Hydra DRYAD worktree. This runs once per worktree before workers begin processing beads.

## When to Activate

- `HYDRA_WORKTREE_PATH` environment variable is set
- Dependencies are missing (e.g., no `node_modules`)
- Project manifest files exist (e.g., `package.json`)

## Setup Process

### Step 1: Detect Project Type

The skill checks for common project files to determine the language/framework:

```bash
cd "$HYDRA_WORKTREE_PATH"

if [ -f "package.json" ]; then
    PROJECT_TYPE="nodejs"
elif [ -f "Cargo.toml" ]; then
    PROJECT_TYPE="rust"
elif [ -f "go.mod" ]; then
    PROJECT_TYPE="go"
elif [ -f "pyproject.toml" ] || [ -f "requirements.txt" ]; then
    PROJECT_TYPE="python"
elif [ -f "pom.xml" ]; then
    PROJECT_TYPE="java-maven"
elif [ -f "build.gradle" ] || [ -f "build.gradle.kts" ]; then
    PROJECT_TYPE="java-gradle"
elif [ -f "Gemfile" ]; then
    PROJECT_TYPE="ruby"
elif [ -f "composer.json" ]; then
    PROJECT_TYPE="php"
else
    PROJECT_TYPE="unknown"
fi
```

### Step 2: Run Setup by Project Type

#### Node.js Projects

**Detect Package Manager:**
```bash
if [ -f "pnpm-lock.yaml" ]; then
    PACKAGE_MANAGER="pnpm"
elif [ -f "yarn.lock" ]; then
    PACKAGE_MANAGER="yarn"
else
    PACKAGE_MANAGER="npm"
fi
```

**pnpm (Recommended):**
```bash
# Check if pnpm is available
if command -v pnpm &> /dev/null; then
    echo "Using pnpm with global virtual store"
    pnpm install
else
    echo "pnpm not found, falling back to npm"
    npm install
fi
```

**npm:**
```bash
npm install
```

**yarn:**
```bash
yarn install
```

#### Rust Projects

```bash
cargo build
```

#### Go Projects

```bash
go mod download
```

#### Python Projects

```bash
if [ -f "poetry.lock" ] && command -v poetry &> /dev/null; then
    poetry install
elif [ -f "Pipfile" ] && command -v pipenv &> /dev/null; then
    pipenv install
elif [ -f "pyproject.toml" ]; then
    pip install -e .
else
    pip install -r requirements.txt
fi
```

#### Java (Maven)

```bash
mvn compile
```

#### Java (Gradle)

```bash
./gradlew build
```

#### Ruby Projects

```bash
bundle install
```

#### PHP Projects

```bash
composer install
```

### Step 3: Verify Setup

```bash
# Node.js: Verify node_modules exists and has content
if [ "$PROJECT_TYPE" = "nodejs" ]; then
    if [ ! -d "node_modules" ]; then
        echo "ERROR: node_modules directory not created"
        exit 1
    fi
    
    PACKAGE_COUNT=$(find node_modules -maxdepth 1 -type d | wc -l)
    if [ "$PACKAGE_COUNT" -lt 2 ]; then
        echo "ERROR: node_modules appears empty"
        exit 1
    fi
    
    echo "✓ Installed $((PACKAGE_COUNT - 1)) packages in node_modules"
fi

# Rust: Verify target directory
if [ "$PROJECT_TYPE" = "rust" ]; then
    if [ ! -d "target" ]; then
        echo "ERROR: target directory not created"
        exit 1
    fi
    echo "✓ Rust dependencies built"
fi

# Go: Verify go.sum was created/updated
if [ "$PROJECT_TYPE" = "go" ]; then
    if [ ! -f "go.sum" ] && [ -f "go.mod" ]; then
        echo "WARNING: go.sum not created"
    fi
    echo "✓ Go modules downloaded"
fi

# Python: Verify virtual environment or packages
if [ "$PROJECT_TYPE" = "python" ]; then
    if [ -d ".venv" ] || [ -d "venv" ]; then
        echo "✓ Python virtual environment created"
    else
        echo "✓ Python packages installed"
    fi
fi
```

## Output Format

### Success

```
WORKTREE_SETUP_RESULT=success
PROJECT_TYPE=nodejs
PACKAGE_MANAGER=pnpm
PACKAGES_INSTALLED=42
DURATION_SECONDS=5
WORKTREE_ID=<hydra-worktree-id>
```

### Failure

```
WORKTREE_SETUP_RESULT=failed
PROJECT_TYPE=nodejs
PACKAGE_MANAGER=pnpm
ERROR_MESSAGE="pnpm: command not found"
WORKTREE_ID=<hydra-worktree-id>
```

### Skipped (No Setup Needed)

```
WORKTREE_SETUP_RESULT=skipped
REASON="No dependency manifest found"
WORKTREE_ID=<hydra-worktree-id>
```

## Environment Variables

The skill reads these environment variables:

| Variable | Description |
|----------|-------------|
| `HYDRA_WORKTREE_PATH` | Absolute path to the worktree directory |
| `HYDRA_WORKTREE_ID` | Unique identifier for the worktree |
| `HYDRA_FEATURE_NAME` | Name of the feature being worked on |
| `HYDRA_CHAIN_ID` | ID of the TDD chain |
| `HYDRA_PREFERRED_PACKAGE_MANAGER` | Optional: pnpm, npm, yarn |

## Error Handling

| Scenario | Action |
|----------|--------|
| Package manager not found | Try fallback (e.g., pnpm → npm), fail if none available |
| Install command fails | Exit with error code, include stderr in output |
| Verification fails | Exit with error, indicating what check failed |
| No project manifest | Return "skipped" status |
| Partial install | Re-run full install (idempotent) |

## Best Practices

1. **Idempotent**: Running setup multiple times should be safe
2. **Fast Detection**: Check for existing dependencies before running install
3. **Clear Output**: Always report what was done and any errors
4. **Graceful Fallbacks**: If preferred tool unavailable, try alternatives
5. **Verification**: Always verify the result before reporting success

## Examples

### Node.js with pnpm

```bash
$ cd /project/.hydra/dryad/feature/auth/simple-calc-prd-new
$ HYDRA_WORKTREE_PATH=$(pwd) pi --skill hydra-worktree-setup

WORKTREE_SETUP_RESULT=success
PROJECT_TYPE=nodejs
PACKAGE_MANAGER=pnpm
PACKAGES_INSTALLED=47
DURATION_SECONDS=3
WORKTREE_ID=simple-calc-prd-new
```

### Rust Project

```bash
WORKTREE_SETUP_RESULT=success
PROJECT_TYPE=rust
PACKAGE_MANAGER=cargo
PACKAGES_INSTALLED=156
DURATION_SECONDS=12
WORKTREE_ID=rust-cli-parser
```

### No Setup Needed

```bash
WORKTREE_SETUP_RESULT=skipped
REASON="No dependency manifest found (no package.json, Cargo.toml, etc.)"
WORKTREE_ID=documentation-update
```
