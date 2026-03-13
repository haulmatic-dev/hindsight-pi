# Pi Agent Skills Reference

**Status:** Draft  
**Last Updated:** 2026-03-12  
**Purpose:** Reference document for Pi agent skills integration with Hydra

## Overview

This document catalogs the curated set of [Pi coding agent](https://pi.dev/) skills integrated with Hydra's worker orchestration system. These skills enhance Pi's capabilities for Go and Angular/TypeScript development workflows.

## Architecture

```
Hydra Service
├── Startup Checks (one-time)
│   ├── Check/Install Pi (npm)
│   ├── Check/Install LSP tools (local - gopls, typescript-language-server)
│   └── Check/Install Skills (npx)
├── Periodic Background Task (daily)
│   └── Update Skills (npx skills add)
└── Worker Spawning
    ├── Set PI_CODING_AGENT_DIR
    └── Pass --skill flags (13 skills)
```

### Directory Structure

```
hydra/
├── pi-config/                      # Self-contained config (Git-tracked)
│   ├── agent/
│   │   ├── AGENTS.md              # Project instructions
│   │   ├── SYSTEM.md              # System prompt
│   │   └── mcp.json               # MCP server configuration
│   └── skills-installed.json      # Tracks installed skill versions
├── scripts/
│   ├── setup.sh                   # One-time setup (Pi + LSP + skills)
│   └── update-skills.sh           # Update all skills
└── internal/
    └── agents/
        └── pi.go                  # Worker spawning with config
```

## Complete Skill Inventory

### 1. LSP Integration & Code Analysis

#### lsp-code-analysis
- **Repository:** [lsp-client/lsp-skill](https://github.com/lsp-client/lsp-skill)
- **Install:** `npx skills add https://github.com/lsp-client/lsp-skill --skill lsp-code-analysis`
- **Weekly Installs:** 113
- **Purpose:** Semantic code navigation and analysis via LSP
- **Key Capabilities:**
  - `lsp definition` - Jump to symbol definitions
  - `lsp reference` - Find all usages
  - `lsp outline` - Get file structure without reading
  - `lsp doc` - Get documentation without navigating
  - `lsp symbol` - Get complete symbol source
  - `lsp search` - Global symbol search
- **Prerequisites:** lsp-cli tool, LSP servers (gopls, typescript)
- **Why Included:** Transforms Pi from text-based to semantic code understanding

**Infrastructure Requirements (Local Installation):**
- lsp-cli installed
- LSP servers running:
  - Go: `ghcr.io/lsp-client/gopls`
  - TypeScript: `ghcr.io/lsp-client/typescript`
- pi-mcp-adapter for MCP connectivity

---

### 2. Go Development

#### golang-pro
- **Repository:** [jeffallan/claude-skills](https://github.com/jeffallan/claude-skills)
- **Install:** `npx skills add https://github.com/jeffallan/claude-skills --skill golang-pro`
- **Weekly Installs:** 4.9K
- **GitHub Stars:** 6.3K
- **Purpose:** Senior Go development patterns
- **Coverage:**
  - Architecture analysis and interface design
  - Concurrent programming (goroutines, channels, context)
  - Performance optimization (pprof, benchmarks)
  - Code quality (golangci-lint)
  - Go 1.21+ features
- **Reference Guides:** concurrency.md, interfaces.md, generics.md, testing.md, project-structure.md
- **MUST DO:** gofmt, context.Context, error wrapping, race detector
- **MUST NOT:** Ignore errors, use panic, create goroutines without lifecycle management

#### golang-testing
- **Repository:** [affaan-m/everything-claude-code](https://github.com/affaan-m/everything-claude-code)
- **Install:** `npx skills add https://github.com/affaan-m/everything-claude-code --skill golang-testing`
- **Weekly Installs:** 1.6K
- **GitHub Stars:** 72.1K (main repo)
- **Purpose:** Comprehensive Go testing patterns with TDD
- **Coverage:**
  - RED-GREEN-REFACTOR cycle
  - Table-driven tests
  - Subtests and parallel tests
  - Test helpers
  - Benchmarks (basic, different sizes, memory allocation)
  - Fuzzing (Go 1.18+)
  - Mocking with interfaces
  - Golden files
  - HTTP handler testing with httptest
- **Coverage Targets:** Critical logic 100%, Public APIs 90%, General code 80%

#### golang-patterns
- **Repository:** [affaan-m/everything-claude-code](https://github.com/affaan-m/everything-claude-code)
- **Install:** `npx skills add https://github.com/affaan-m/everything-claude-code --skill golang-patterns`
- **Weekly Installs:** 1.9K
- **GitHub Stars:** 72.1K (main repo)
- **Purpose:** Idiomatic Go patterns and best practices
- **Key Patterns:**
  - Error wrapping with context
  - Custom error types
  - errors.Is/errors.As
  - Worker pool
  - Context cancellation
  - Graceful shutdown
  - errgroup
- **Interface Design:** Small focused interfaces, define where used, optional behavior
- **Package Organization:** Standard project layout, naming conventions
- **Struct Design:** Functional options pattern, embedding for composition
- **Memory/Performance:** Preallocate slices, sync.Pool, strings.Builder
- **Tooling:** go vet, staticcheck, golangci-lint, goimports

---

### 3. Angular Development

#### angular-component
- **Repository:** [analogjs/angular-skills](https://github.com/analogjs/angular-skills)
- **Install:** `npx skills add https://github.com/analogjs/angular-skills --skill angular-component`
- **Purpose:** Standalone components with modern Angular patterns
- **Target:** Angular v20+
- **Coverage:**
  - Standalone components (no NgModules)
  - Signal inputs/outputs
  - OnPush change detection
  - Host bindings
  - Content projection
  - Lifecycle hooks

#### angular-signals
- **Repository:** [analogjs/angular-skills](https://github.com/analogjs/angular-skills)
- **Install:** `npx skills add https://github.com/analogjs/angular-skills --skill angular-signals`
- **Purpose:** Reactive state management with signals
- **Coverage:**
  - `signal()` - Writable signals
  - `computed()` - Derived state
  - `linkedSignal()` - Linked signals
  - `effect()` - Side effects
  - RxJS interop

#### angular-testing
- **Repository:** [analogjs/angular-skills](https://github.com/analogjs/angular-skills)
- **Install:** `npx skills add https://github.com/analogjs/angular-skills --skill angular-testing`
- **Purpose:** Testing Angular applications
- **Coverage:**
  - TestBed configuration
  - Component harnesses
  - Signal testing patterns
  - OnPush component testing

---

### 4. TypeScript

#### typescript-advanced-types
- **Repository:** [wshobson/agents](https://github.com/wshobson/agents)
- **Install:** `npx skills add https://github.com/wshobson/agents --skill typescript-advanced-types`
- **Weekly Installs:** 12.7K
- **Purpose:** Advanced TypeScript patterns and type manipulation
- **Installs:** 12.7K

---

### 5. Testing

#### vitest
- **Repository:** [antfu/skills](https://github.com/antfu/skills)
- **Install:** `npx skills add https://github.com/antfu/skills --skill vitest`
- **Weekly Installs:** 8.2K
- **Purpose:** Vitest testing framework for TypeScript projects
- **Use Case:** Non-Angular TypeScript testing (complements angular-testing)

---

### 6. Design Implementation

#### implement-design
- **Repository:** [figma/mcp-server-guide](https://github.com/figma/mcp-server-guide)
- **Install:** `npx skills add https://github.com/figma/mcp-server-guide --skill implement-design`
- **Weekly Installs:** 4.3K
- **Purpose:** Pixel-perfect Figma design implementation
- **Prerequisites:**
  - Figma MCP server connected
  - Figma URLs in format: `https://figma.com/design/:fileKey/:fileName?node-id=1-2`
- **Workflow:**
  1. Parse Figma URL for file key and node ID
  2. Fetch design context (layout, typography, colors)
  3. Capture screenshot for visual reference
  4. Download assets (images, icons, SVGs)
  5. Translate to project conventions (Angular)
  6. Achieve 1:1 visual parity
  7. Validate against Figma screenshot
- **Output:** Angular components matching Figma designs

**Figma MCP Setup:**
- Figma MCP server must be running
- Connected via pi-mcp-adapter
- Provides `get_design_context`, `get_screenshot`, asset endpoints

---

### 7. Methodology

#### systematic-debugging
- **Repository:** [obra/superpowers](https://github.com/obra/superpowers)
- **Install:** `npx skills add https://github.com/obra/superpowers --skill systematic-debugging`
- **Weekly Installs:** 27.7K
- **GitHub Stars:** 78K
- **Purpose:** Scientific debugging methodology
- **Core Principle:** NO FIXES WITHOUT ROOT CAUSE INVESTIGATION FIRST
- **4-Phase Methodology:**
  1. **Root Cause Investigation** - Read errors, reproduce, check changes, gather evidence
  2. **Pattern Analysis** - Find working examples, compare, identify differences
  3. **Hypothesis and Testing** - Scientific method, test minimally
  4. **Implementation** - Create test, fix root cause, verify
- **Red Flags:** 'Quick fix', 'Just try changing X', 'Skip the test'
- **Architecture Questioning:** 3+ failed fixes = question architecture
- **Backward Tracing:** For deep call stacks
- **Real-world Impact:** 15-30 min vs 2-3 hours, 95% first-time fix rate

#### test-driven-development
- **Repository:** [obra/superpowers](https://github.com/obra/superpowers)
- **Install:** `npx skills add https://github.com/obra/superpowers --skill test-driven-development`
- **Weekly Installs:** 23.0K
- **Purpose:** TDD workflows
- **Coverage:**
  - RED-GREEN-REFACTOR cycle
  - Writing failing tests first
  - Minimal implementation
  - Refactoring for clean code
  - Test isolation

#### code-review
- **Repository:** [supercent-io/skills-template](https://github.com/supercent-io/skills-template)
- **Install:** `npx skills add https://github.com/supercent-io/skills-template --skill code-review`
- **Weekly Installs:** 10.8K
- **Purpose:** Language-agnostic code review guidelines
- **Coverage:**
  - Code review best practices
  - What to look for (bugs, security, performance, maintainability)
  - How to give constructive feedback
  - Review checklist approach
  - Common anti-patterns to catch
- **Why This Version:** Language-agnostic, works across Go + Angular, simpler than subagent-based alternatives

---

## Installation Summary

### All Skills Installation Script

```bash
#!/bin/bash
# Install all Pi skills for Hydra workers

echo "Installing Go development skills..."
npx skills add https://github.com/jeffallan/claude-skills --skill golang-pro
npx skills add https://github.com/affaan-m/everything-claude-code --skill golang-testing
npx skills add https://github.com/affaan-m/everything-claude-code --skill golang-patterns

echo "Installing Angular skills..."
npx skills add https://github.com/analogjs/angular-skills --skill angular-component
npx skills add https://github.com/analogjs/angular-skills --skill angular-signals
npx skills add https://github.com/analogjs/angular-skills --skill angular-testing

echo "Installing TypeScript skills..."
npx skills add https://github.com/wshobson/agents --skill typescript-advanced-types
npx skills add https://github.com/antfu/skills --skill vitest

echo "Installing design implementation skill..."
npx skills add https://github.com/figma/mcp-server-guide --skill implement-design

echo "Installing methodology skills..."
npx skills add https://github.com/obra/superpowers --skill systematic-debugging
npx skills add https://github.com/obra/superpowers --skill test-driven-development
npx skills add https://github.com/supercent-io/skills-template --skill code-review

echo "Installing LSP code analysis skill..."
npx skills add https://github.com/lsp-client/lsp-skill --skill lsp-code-analysis

echo "All skills installed successfully!"
```

### Infrastructure Requirements (Local Installation)

**Prerequisites:** Go, Node.js/npm, and Python/pip must be installed on the system.

```bash
# 1. Install Pi Agent (if not present)
npm install -g @mariozechner/pi-coding-agent

# 2. Install LSP CLI (Python)
pip install lsp-client

# 3. Install Go LSP Server (gopls)
go install golang.org/x/tools/gopls@latest
# Ensure $GOPATH/bin is in $PATH

# 4. Install TypeScript LSP Server
npm install -g typescript-language-server typescript

# 5. Configure MCP servers in hydra/pi-config/agent/mcp.json
cat > hydra/pi-config/agent/mcp.json << 'EOF'
{
  "mcpServers": {
    "lsp-go": {
      "command": "gopls",
      "args": ["serve"]
    },
    "lsp-typescript": {
      "command": "typescript-language-server",
      "args": ["--stdio"]
    }
  }
}
EOF
```

---

## Hydra Integration

### Startup Checks

Hydra performs automatic checks on startup to ensure Pi ecosystem is ready:

```go
// Hydra initialization flow
func InitializePiEcosystem() {
    // 1. Check/Install Pi Agent
    if !piInstalled() {
        exec.Command("npm", "install", "-g", "@mariozechner/pi-coding-agent")
    }
    
    // 2. Check/Install LSP Tools
    if !lspCliInstalled() {
        exec.Command("pip", "install", "lsp-client")
    }
    if !goplsInstalled() {
        exec.Command("go", "install", "golang.org/x/tools/gopls@latest")
    }
    if !tsLspInstalled() {
        exec.Command("npm", "install", "-g", "typescript-language-server", "typescript")
    }
    
    // 3. Check/Install Skills
    if !skillsInstalled() {
        installAllSkills()
    }
    
    // 4. Start periodic updater
    startPeriodicSkillUpdater(24 * time.Hour)
}
```

### Periodic Skill Updates

Since Hydra runs as a service, a background goroutine updates skills daily:

```go
func startPeriodicSkillUpdater() {
    ticker := time.NewTicker(24 * time.Hour)
    go func() {
        for range ticker.C {
            updateAllSkills()  // Re-installs all 13 skills via npx
        }
    }()
}
```

This ensures skills stay current without manual intervention.

### Worker Spawning

When Hydra spawns a Pi worker:

```go
// Set environment variable for Pi config directory
os.Setenv("PI_CODING_AGENT_DIR", 
    filepath.Join(hydraRoot, "pi-config/agent"))

// Build skill paths
skillsDir := filepath.Join(hydraRoot, "pi-config/skills")

args := []string{
    "--mode", "rpc",
    "--skill", filepath.Join(skillsDir, "golang-pro"),
    "--skill", filepath.Join(skillsDir, "golang-testing"),
    "--skill", filepath.Join(skillsDir, "golang-patterns"),
    "--skill", filepath.Join(skillsDir, "angular-component"),
    "--skill", filepath.Join(skillsDir, "angular-signals"),
    "--skill", filepath.Join(skillsDir, "angular-testing"),
    "--skill", filepath.Join(skillsDir, "typescript-advanced-types"),
    "--skill", filepath.Join(skillsDir, "vitest"),
    "--skill", filepath.Join(skillsDir, "implement-design"),
    "--skill", filepath.Join(skillsDir, "systematic-debugging"),
    "--skill", filepath.Join(skillsDir, "test-driven-development"),
    "--skill", filepath.Join(skillsDir, "code-review"),
    "--skill", filepath.Join(skillsDir, "lsp-code-analysis"),
}
```

### Hydra Configuration

Add to `hydra.yaml`:

```yaml
pi:
  config_dir: "./pi-config"
  skills:
    update_interval: "24h"      # Daily updates
    auto_update: true           # Enable periodic updates
    global: true                # Use global skill set
    list:                       # All 13 skills
      - name: golang-pro
        repo: jeffallan/claude-skills
      - name: golang-testing
        repo: affaan-m/everything-claude-code
      - name: golang-patterns
        repo: affaan-m/everything-claude-code
      - name: angular-component
        repo: analogjs/angular-skills
      - name: angular-signals
        repo: analogjs/angular-skills
      - name: angular-testing
        repo: analogjs/angular-skills
      - name: typescript-advanced-types
        repo: wshobson/agents
      - name: vitest
        repo: antfu/skills
      - name: implement-design
        repo: figma/mcp-server-guide
      - name: systematic-debugging
        repo: obra/superpowers
      - name: test-driven-development
        repo: obra/superpowers
      - name: code-review
        repo: supercent-io/skills-template
      - name: lsp-code-analysis
        repo: lsp-client/lsp-skill
        
  lsp:
    enabled: true
    servers:
      go:
        command: "gopls"
        install_check: "which gopls"
        install_cmd: "go install golang.org/x/tools/gopls@latest"
      typescript:
        command: "typescript-language-server"
        install_check: "which typescript-language-server"
        install_cmd: "npm install -g typescript-language-server typescript"
```

---

## Setup Scripts

### scripts/setup.sh

```bash
#!/bin/bash
set -e

HYDRA_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "=== Hydra Pi Setup ==="

# 1. Check/Install Pi Agent
if ! command -v pi &> /dev/null; then
    echo "Installing Pi..."
    npm install -g @mariozechner/pi-coding-agent
fi

# 2. Check/Install LSP CLI (Python)
if ! command -v lsp &> /dev/null && ! pip show lsp-client &> /dev/null; then
    echo "Installing lsp-cli..."
    pip install lsp-client
fi

# 3. Check/Install Go LSP (gopls)
if ! command -v gopls &> /dev/null; then
    echo "Installing gopls..."
    go install golang.org/x/tools/gopls@latest
    echo "Ensure \$GOPATH/bin is in your PATH"
fi

# 4. Check/Install TypeScript LSP
if ! command -v typescript-language-server &> /dev/null; then
    echo "Installing TypeScript LSP..."
    npm install -g typescript-language-server typescript
fi

# 5. Create config directories
mkdir -p "$HYDRA_ROOT/pi-config/agent"
mkdir -p "$HYDRA_ROOT/pi-config/skills"

# 6. Install skills
echo "Installing skills..."
"$HYDRA_ROOT/scripts/install-skills.sh"

echo "=== Setup Complete ==="
echo "Config directory: $HYDRA_ROOT/pi-config"
echo "Skills directory: $HYDRA_ROOT/pi-config/skills"
```

### scripts/install-skills.sh

```bash
#!/bin/bash
set -e

SKILLS_DIR="$(cd "$(dirname "$0")/../pi-config/skills" && pwd)"
cd "$SKILLS_DIR"

echo "Installing 13 Pi skills..."

# Go Development (3)
npx skills add https://github.com/jeffallan/claude-skills --skill golang-pro
npx skills add https://github.com/affaan-m/everything-claude-code --skill golang-testing
npx skills add https://github.com/affaan-m/everything-claude-code --skill golang-patterns

# Angular (3)
npx skills add https://github.com/analogjs/angular-skills --skill angular-component
npx skills add https://github.com/analogjs/angular-skills --skill angular-signals
npx skills add https://github.com/analogjs/angular-skills --skill angular-testing

# TypeScript & Testing (2)
npx skills add https://github.com/wshobson/agents --skill typescript-advanced-types
npx skills add https://github.com/antfu/skills --skill vitest

# Design (1)
npx skills add https://github.com/figma/mcp-server-guide --skill implement-design

# Methodology (3)
npx skills add https://github.com/obra/superpowers --skill systematic-debugging
npx skills add https://github.com/obra/superpowers --skill test-driven-development
npx skills add https://github.com/supercent-io/skills-template --skill code-review

# LSP Integration (1)
npx skills add https://github.com/lsp-client/lsp-skill --skill lsp-code-analysis

echo "All 13 skills installed successfully!"

# Update tracking file
cat > "$(dirname "$SKILLS_DIR")/skills-installed.json" << EOF
{
  "last_updated": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "skills_count": 13,
  "update_interval": "24h"
}
EOF
```

### scripts/update-skills.sh

```bash
#!/bin/bash
# Update all installed skills to latest versions

HYDRA_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
"$HYDRA_ROOT/scripts/install-skills.sh"
```

---

## Usage Patterns

### Go Development Workflow

1. **Start with golang-pro** for architecture decisions
2. **Use golang-patterns** for idiomatic implementation
3. **Apply systematic-debugging** when issues arise
4. **Use lsp-code-analysis** for semantic navigation
   ```
   lsp outline main.go
   lsp definition main.go --scope MyService.Process
   lsp reference main.go --scope MyService.Process
   ```
5. **Write tests** with golang-testing (table-driven, benchmarks)
6. **Request code review** after implementation

### Angular Development Workflow

1. **Get Figma design** - Parse URL, fetch context with implement-design skill
2. **Create component** using angular-component skill
3. **Implement state** with angular-signals
4. **Write tests** using angular-testing
5. **Validate** against Figma screenshot
6. **Code review** with code-review skill

### Cross-Cutting Concerns

- **Debugging:** systematic-debugging (4-phase methodology)
- **Testing:** test-driven-development (RED-GREEN-REFACTOR)
- **Code Quality:** code-review (checklist-based)
- **Navigation:** lsp-code-analysis (semantic, not text-based)

---

## Skill Compatibility

All skills are compatible with:
- **Pi coding agent** (primary target)
- **Claude Code**
- **Codex CLI**
- **Amp**
- **Droid (Factory)**
- **Other agents supporting standard skill format**

See [pi-skills](https://github.com/badlogic/pi-skills) for format specification.

---

## Maintenance Notes

### Automatic Updates

Hydra automatically updates skills via a background goroutine that runs every 24 hours. Skills are re-installed via `npx skills add`, ensuring the latest versions are always fetched.

### Manual Updates

If you need to force an immediate update:

```bash
# Update all skills manually
./scripts/update-skills.sh

# Or using npx directly
cd pi-config/skills
npx skills add <repo> --skill <name>
```

### Monitoring Skill Usage

- Track which skills are most valuable via worker logs
- Monitor LSP server performance
- Review skill output quality in completed beads
- Check `pi-config/skills-installed.json` for last update timestamp

### Future Additions

Consider adding when needed:
- **angular-ssr** - If server-side rendering becomes required
- **angular-routing** - If complex routing patterns needed
- **angular-http** - If specific HTTP patterns beyond basic usage
- **security-best-practices** - If security-focused development needed (JS-focused)

---

## Related Documents

- [Pi Documentation](https://pi.dev)
- [Skills.sh Registry](https://skills.sh)
- [Pi Skills Compatibility](https://github.com/badlogic/pi-skills)
- [LSP Client Ecosystem](https://github.com/lsp-client)
- [Hydra Agent Connector Architecture](./architecture/agent-connector-20260311.md)

---

## Changelog

### 2026-03-12 - Initial Draft
- Created comprehensive skill inventory
- Documented 13 skills across 7 categories
- Included installation scripts and infrastructure requirements
- Added usage patterns for Go and Angular workflows

### 2026-03-12 - Implementation Update
- Updated architecture for local LSP installation (no Docker)
- Added Hydra integration section with startup checks
- Documented periodic skill updates (daily background task)
- Added complete setup scripts (setup.sh, install-skills.sh, update-skills.sh)
- Updated Hydra configuration examples (hydra.yaml)
- Specified self-contained directory structure (hydra/pi-config/)
- Clarified skill auto-update mechanism via npx

---

**Next Steps:** Convert this document to PRD format for implementation planning.
