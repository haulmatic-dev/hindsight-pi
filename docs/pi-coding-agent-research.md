# Pi Coding Agent Library & Skills System Research Report

## Executive Summary

This report provides comprehensive technical analysis of the **@mariozechner/pi-coding-agent** library, its skills system, LSP integration, and MCP (Model Context Protocol) considerations. Pi is a minimal terminal coding harness that supports Agent Skills (following the agentskills.io standard) and extensions for customization.

---

## 1. Pi Skills System Overview

### 1.1 What Are Pi Skills?

Skills are self-contained capability packages that the Pi agent loads on-demand. They provide specialized workflows, setup instructions, helper scripts, and reference documentation for specific tasks.

**Key Characteristics:**
- Follow the **Agent Skills standard** (agentskills.io)
- Loaded on-demand (progressive disclosure)
- Can include executable scripts and documentation
- Compatible with Claude Code, Codex CLI, and other agents

### 1.2 Skill Directory Structure

```
skill-name/
├── SKILL.md              # Required: metadata + instructions (YAML frontmatter)
├── scripts/              # Optional: executable code
│   └── process.sh
├── references/           # Optional: detailed docs loaded on-demand
│   └── api-reference.md
└── assets/               # Optional: templates, resources
    └── template.json
```

### 1.3 SKILL.md Format Specification

The SKILL.md file must contain YAML frontmatter followed by Markdown content:

```markdown
---
name: skill-name
description: What this skill does and when to use it. Be specific.
license: Apache-2.0              # Optional
compatibility: Requires Node.js  # Optional
metadata:                        # Optional
  author: example-org
  version: "1.0"
allowed-tools: Bash(git:*) Read  # Optional (experimental)
disable-model-invocation: false  # Optional
---

# Skill Name

## Setup
Run once before first use:
```bash
cd /path/to/skill && npm install
```

## Usage
```bash
./scripts/process.sh <input>
```
```

**Frontmatter Fields:**

| Field | Required | Constraints |
|-------|----------|-------------|
| `name` | Yes | 1-64 chars, lowercase a-z, 0-9, hyphens only |
| `description` | Yes | 1-1024 chars, describes what and when |
| `license` | No | License name or file reference |
| `compatibility` | No | Max 500 chars, environment requirements |
| `metadata` | No | Arbitrary key-value mapping |
| `allowed-tools` | No | Space-delimited pre-approved tools |
| `disable-model-invocation` | No | Hide from system prompt if true |

**Name Validation Rules:**
- Must match parent directory name
- Only lowercase letters, numbers, hyphens
- No leading/trailing hyphens
- No consecutive hyphens
- Max 64 characters

### 1.4 Skill Loading Locations

Pi loads skills from multiple locations (in order of precedence):

**Global Locations:**
- `~/.pi/agent/skills/` (primary)
- `~/.agents/skills/` (fallback)

**Project Locations:**
- `.pi/skills/` (project-specific)
- `.agents/skills/` (in cwd and ancestor directories up to git root)

**Package Sources:**
- `skills/` directories in pi packages
- `pi.skills` entries in package.json

**Explicit Sources:**
- Settings: `skills` array in settings.json
- CLI: `--skill <path>` flag (repeatable)

**Discovery Rules:**
- Direct `.md` files in skills directory root
- Recursive `SKILL.md` files under subdirectories

---

## 2. Programmatic Skill Installation

### 2.1 Package Manager Commands

Pi provides built-in package management commands for installing skills and extensions:

```bash
# Install from npm
pi install npm:@scope/pi-package
pi install npm:@scope/pi-package@1.2.3  # pinned version

# Install from git
pi install git:github.com/user/repo
pi install git:github.com/user/repo@v1  # tag or commit
pi install git:git@github.com:user/repo
pi install https://github.com/user/repo
pi install ssh://git@github.com/user/repo

# Install local path
pi install ./local/path

# Project-local installation (adds to .pi/settings.json)
pi install npm:@foo/bar -l

# Remove packages
pi remove npm:@foo/bar
pi remove npm:@foo/bar -l  # project-local

# Update packages
pi update              # update all non-pinned
pi update npm:@foo/bar # update specific

# List installed
pi list

# Configure which resources to enable
pi config
```

### 2.2 Package Structure for Distribution

To create a distributable Pi package, add a `pi` key to `package.json`:

```json
{
  "name": "my-pi-package",
  "keywords": ["pi-package"],
  "pi": {
    "extensions": ["./extensions"],
    "skills": ["./skills"],
    "prompts": ["./prompts"],
    "themes": ["./themes"]
  }
}
```

**Without a `pi` manifest**, Pi auto-discovers from conventional directories:
- `extensions/` - loads `.ts` and `.js` files
- `skills/` - finds `SKILL.md` folders
- `prompts/` - loads `.md` files
- `themes/` - loads `.json` files

### 2.3 SDK-Based Installation (Programmatic)

For programmatic installation within Node.js applications:

```typescript
import { DefaultPackageManager, SettingsManager } from "@mariozechner/pi-coding-agent";

const cwd = process.cwd();
const agentDir = "~/.pi/agent";
const settingsManager = SettingsManager.create(cwd, agentDir);
const packageManager = new DefaultPackageManager({ cwd, agentDir, settingsManager });

// Install package
await packageManager.install("npm:@foo/bar", { local: false });

// Add to settings
packageManager.addSourceToSettings("npm:@foo/bar", { local: false });

// Remove package
await packageManager.remove("npm:@foo/bar", { local: false });
packageManager.removeSourceFromSettings("npm:@foo/bar", { local: false });

// Update packages
await packageManager.update(); // all
await packageManager.update("npm:@foo/bar"); // specific
```

---

## 3. Passing Skills to Pi Agent Workers

### 3.1 CLI Flags for Skill Loading

```bash
# Load specific skill (repeatable)
pi --skill /path/to/skill

# Disable skill discovery, load only explicit skills
pi --no-skills --skill /path/to/skill1 --skill /path/to/skill2

# Combined with extensions
pi --skill /path/to/skill -e /path/to/extension.ts
```

### 3.2 Settings Configuration

Add skills to `~/.pi/agent/settings.json` or `.pi/settings.json`:

```json
{
  "skills": [
    "/absolute/path/to/skill",
    "~/skills/my-skill",
    "./project-skills"
  ],
  "packages": [
    "npm:pi-skills",
    {
      "source": "npm:my-package",
      "skills": ["skill1", "skill2"],
      "extensions": [],
      "prompts": [],
      "themes": []
    }
  ]
}
```

### 3.3 SDK-Based Skill Loading

```typescript
import {
  createAgentSession,
  DefaultResourceLoader,
  type Skill,
} from "@mariozechner/pi-coding-agent";

// Define custom skill
const customSkill: Skill = {
  name: "my-skill",
  description: "Custom instructions",
  filePath: "/path/to/SKILL.md",
  baseDir: "/path/to",
  source: "custom",
};

// Create resource loader with skill override
const loader = new DefaultResourceLoader({
  skillsOverride: (current) => ({
    skills: [...current.skills, customSkill],
    diagnostics: current.diagnostics,
  }),
});
await loader.reload();

// Create session with custom loader
const { session } = await createAgentSession({ resourceLoader: loader });
```

### 3.4 Additional Skill Paths

```typescript
const loader = new DefaultResourceLoader({
  additionalSkillPaths: [
    "/path/to/skill1",
    "/path/to/skill2",
  ],
});
await loader.reload();
```

---

## 4. Environment Variables

### 4.1 Primary Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `PI_CODING_AGENT_DIR` | Override config directory | `~/.pi/agent` |
| `PI_PACKAGE_DIR` | Override package directory | (none) |
| `PI_SKIP_VERSION_CHECK` | Skip version check at startup | `false` |
| `PI_CACHE_RETENTION` | Set to `long` for extended prompt cache | (none) |
| `VISUAL`, `EDITOR` | External editor for Ctrl+G | (system default) |
| `PI_OFFLINE` | Enable offline mode | `false` |

Hydra sets `PI_CACHE_RETENTION=long` on every worker spawn (daemon env
builder + `cmd/worker` launch site, 2026-09-06), so pi-ai marks cache blocks
`cache_control {type:ephemeral, ttl:"1h"}` instead of the default 5m TTL.
Whether the coding endpoint honors the ttl marker for cache expiry is
measured by `scripts/cache-ttl-probe.sh` (warms a ~26K-token prefix with the
ttl:1h marker, probes `cache_read` at +35min and +65min, prints HIT/MISS).
Prior TTL measurements only covered `[300s, 1800s)` with default markers; if
the probe shows MISS at 35min the env var stays set (harmless — providers
ignore markers they do not support) but cache retention beyond 5m must not be
relied on. The probe needs ~65 minutes and must run outside a worker turn
(`systemd-run`/`nohup`).

### 4.2 API Key Environment Variables

Pi automatically reads API keys from environment variables:

- `ANTHROPIC_API_KEY`
- `OPENAI_API_KEY`
- `GEMINI_API_KEY`
- `AZURE_OPENAI_API_KEY`
- `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`
- `MISTRAL_API_KEY`
- `GROQ_API_KEY`
- `CEREBRAS_API_KEY`
- `XAI_API_KEY`
- `OPENROUTER_API_KEY`
- `HUGGINGFACE_API_KEY`
- And others (see provider documentation)

### 4.3 Usage Example

```bash
# Set config directory
export PI_CODING_AGENT_DIR=/custom/pi/config

# Set API key
export ANTHROPIC_API_KEY=sk-ant-...

# Run pi
pi

# Or inline
PI_CODING_AGENT_DIR=/custom/path pi
```

---

## 5. MCP (Model Context Protocol) Integration

### 5.1 Pi's Philosophy on MCP

**Pi does not natively support MCP.** According to the documentation:

> **No MCP.** Build CLI tools with READMEs (see Skills), or build an extension that adds MCP support. [Why?](https://mariozechner.at/posts/2025-11-02-what-if-you-dont-need-mcp/)

Pi takes an intentionally minimal approach, preferring:
1. **Skills** for capability extension (following agentskills.io)
2. **Extensions** for deep integration needs

### 5.2 MCP Integration via Extensions

MCP support can be added through custom extensions:

```typescript
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

export default function mcpExtension(pi: ExtensionAPI) {
  // Register MCP client
  const mcpClient = new MCPClient({ /* config */ });
  
  // Register MCP tools
  pi.registerTool({
    name: "mcp_call",
    description: "Call MCP server",
    parameters: Type.Object({
      server: Type.String(),
      method: Type.String(),
      params: Type.Optional(Type.Record(Type.String(), Type.Unknown())),
    }),
    async execute(toolCallId, params, signal, onUpdate, ctx) {
      const result = await mcpClient.call(params.server, params.method, params.params);
      return {
        content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
        details: { result },
      };
    },
  });
}
```

### 5.3 MCP vs Skills Comparison

| Aspect | MCP | Pi Skills |
|--------|-----|-----------|
| **Protocol** | JSON-RPC based | Markdown + scripts |
| **Discovery** | Dynamic tool discovery | Static file-based |
| **Transport** | stdio, HTTP, WebSocket | File system |
| **State** | Stateful connections | Stateless |
| **Complexity** | Higher | Lower |
| **Portability** | Protocol-dependent | File-based, portable |

### 5.4 Recommendation

For Pi integration:
1. **Use Skills** for most capabilities (simpler, more portable)
2. **Use Extensions** when you need:
   - Stateful connections
   - Custom UI components
   - Direct MCP protocol support
   - Complex lifecycle management

---

## 6. Skills.sh Registry Format

### 6.1 Understanding Skills.sh

While Pi doesn't use a specific `skills.sh` registry file, the Agent Skills ecosystem supports registry mechanisms. Based on the Agent Skills specification and Pi's implementation:

### 6.2 Skill Registry Structure

A skill registry is typically a collection of skills in a directory or repository:

```
skills-registry/
├── skill1/
│   └── SKILL.md
├── skill2/
│   └── SKILL.md
└── README.md
```

### 6.3 Programmatic Skill Discovery

```typescript
import { DefaultResourceLoader } from "@mariozechner/pi-coding-agent";

const loader = new DefaultResourceLoader({
  cwd: process.cwd(),
  agentDir: "~/.pi/agent",
});
await loader.reload();

// Get all discovered skills
const skills = loader.getSkills();

// Skill object structure:
// {
//   name: string;
//   description: string;
//   filePath: string;
//   baseDir: string;
//   source: "user" | "project" | "package";
// }
```

### 6.4 Skill Validation

Use the skills-ref library to validate skills:

```bash
# Install validation tool
npm install -g @agentskills/skills-ref

# Validate skill
skills-ref validate ./my-skill
```

---

## 7. LSP Skill Specifics

### 7.1 Available LSP Packages

Two main LSP packages for Pi:

1. **@yofriadi/pi-lsp** - Extension-based LSP integration
2. **@oh-my-pi/lsp** - LSP tool for Pi (Rust-based)

### 7.2 @yofriadi/pi-lsp Extension

**Installation:**
```bash
pi install git:github.com/yofriadi/pi-extensions@lsp-v<version>
```

**Package Structure:**
```
packages/lsp/
├── src/
│   ├── index.ts           # Extension entrypoint
│   ├── client/
│   │   ├── runtime.ts     # LSP client lifecycle
│   │   └── registry.ts    # Multi-server orchestration
│   ├── config/
│   │   └── resolver.ts    # Server config resolution
│   ├── tools/
│   │   └── lsp-tool.ts    # LSP tool actions
│   └── hooks/
│       └── writethrough.ts # Format/diagnostics hooks
```

**Configuration:**

LSP servers are configured via `~/.pi/agent/lsp.json|yaml|yml` or `<cwd>/.pi/lsp.json|yaml|yml`:

```json
{
  "serverCommand": ["typescript-language-server", "--stdio"],
  "server": "typescript-language-server",
  "args": ["--stdio"],
  "servers": {
    "typescript": {
      "command": ["typescript-language-server", "--stdio"],
      "fileTypes": [".ts", ".tsx", ".js", ".jsx"]
    },
    "rust": {
      "command": ["rust-analyzer"],
      "fileTypes": [".rs"]
    }
  }
}
```

**Features:**
- Multi-server registry with file-type routing
- Workspace fallback selection
- Full LSP tool actions:
  - `diagnostics` - Get diagnostics
  - `definition` - Go to definition
  - `references` - Find references
  - `hover` - Hover information
  - `symbols` - Document/workspace symbols
  - `rename` - Symbol renaming
  - `status` - Server status
  - `reload` - Reload server
- Write-through hooks:
  - Format-on-write
  - Diagnostics-on-write

**Usage:**
```bash
# After loading extension
/lsp-status              # Show LSP status

# Use lsp tool
lsp action:definition path:/path/to/file.ts line:10 character:5
lsp action:hover path:/path/to/file.ts line:10 character:5
lsp action:symbols path:/path/to/file.ts
lsp action:diagnostics
```

### 7.3 Extension Implementation Details

```typescript
// src/index.ts
export default function lspExtension(pi: ExtensionAPI): void {
  const runtime = createLspRuntimeRegistry();
  const configResolver = createLspConfigResolver();
  const toolRouter = createLspToolRouter(runtime, {
    getResolvedConfig: () => configResolver.resolve(),
  });
  const writeThroughHooks = createWriteThroughHooks(runtime);

  toolRouter.register(pi);
  writeThroughHooks.register(pi);

  pi.on("session_start", async (_event, ctx) => {
    const config = configResolver.resolve();
    await runtime.start(config);
    const status = runtime.getStatus();
    if (status.state === "error") {
      ctx.ui.notify(`LSP startup failed: ${status.reason}`, "warning");
    }
  });

  pi.on("session_shutdown", async () => {
    await runtime.stop();
  });

  pi.registerCommand("lsp-status", {
    description: "Show LSP health information",
    handler: async (_args, ctx) => {
      const status = runtime.getStatus();
      // Display status...
    },
  });
}
```

### 7.4 LSP Client/Server Interaction

The LSP extension manages:

1. **Server Lifecycle**
   - Spawns LSP subprocess (Bun `spawn` or Node `child_process.spawn`)
   - JSON-RPC initialize/shutdown
   - Automatic restart on failure

2. **Request Routing**
   - File-type based routing to appropriate server
   - Workspace-scoped request fallback

3. **Diagnostics Cache**
   - Caches diagnostics for quick access
   - Updates on file changes

---

## 8. Integration Requirements Summary

### 8.1 System Requirements

- **Node.js**: 18+ (for running Pi)
- **Package Manager**: npm, yarn, or pnpm
- **Git**: For installing skills from git repositories

### 8.2 Installation Flow

```bash
# 1. Install Pi globally
npm install -g @mariozechner/pi-coding-agent

# 2. Configure API keys
export ANTHROPIC_API_KEY=sk-ant-...

# 3. Install skills package
pi install npm:@yofriadi/pi-lsp

# Or install from git
pi install git:github.com/badlogic/pi-skills

# 4. Configure skills (optional)
echo '{
  "packages": ["npm:@yofriadi/pi-lsp"]
}' > ~/.pi/agent/settings.json
```

### 8.3 SDK Integration

```typescript
import {
  AuthStorage,
  createAgentSession,
  DefaultResourceLoader,
  ModelRegistry,
  SessionManager,
  SettingsManager,
} from "@mariozechner/pi-coding-agent";

async function main() {
  // Setup
  const authStorage = AuthStorage.create();
  const modelRegistry = new ModelRegistry(authStorage);
  const settingsManager = SettingsManager.create();
  
  // Resource loading with skills
  const loader = new DefaultResourceLoader({
    additionalSkillPaths: ["./custom-skills"],
  });
  await loader.reload();
  
  // Create session
  const { session } = await createAgentSession({
    sessionManager: SessionManager.inMemory(),
    authStorage,
    modelRegistry,
    resourceLoader: loader,
  });
  
  // Use session
  await session.prompt("Use the LSP skill to find definitions in src/index.ts");
}
```

### 8.4 Configuration Files

**~/.pi/agent/settings.json:**
```json
{
  "defaultProvider": "anthropic",
  "defaultModel": "claude-sonnet-4-20250514",
  "packages": [
    "npm:@yofriadi/pi-lsp",
    {
      "source": "git:github.com/badlogic/pi-skills",
      "skills": ["brave-search", "vscode"]
    }
  ],
  "skills": [
    "~/custom-skills/my-skill"
  ]
}
```

**~/.pi/agent/lsp.json:**
```json
{
  "servers": {
    "typescript": {
      "command": ["typescript-language-server", "--stdio"],
      "fileTypes": [".ts", ".tsx"]
    }
  }
}
```

---

## 9. Security Considerations

### 9.1 Skill Security

> **Warning:** Skills can instruct the model to perform any action and may include executable code. Review skill content before use.

**Best Practices:**
1. Review SKILL.md before installation
2. Check scripts/ directory for malicious code
3. Use trusted sources (official npm packages, verified git repos)
4. Pin versions: `npm:@foo/bar@1.2.3`

### 9.2 Extension Security

> **Warning:** Extensions run with your full system permissions and can execute arbitrary code.

**Best Practices:**
1. Only install extensions from trusted sources
2. Review extension source code
3. Use `--extension` flag for one-time testing
4. Prefer skills for simple capabilities

### 9.3 MCP Security

If implementing MCP via extension:
1. Validate MCP server identity
2. Sanitize all inputs/outputs
3. Use secure transport (WSS, HTTPS)
4. Implement proper authentication

---

## 10. References

### Official Documentation

- Pi Coding Agent: https://github.com/badlogic/pi-mono/tree/main/packages/coding-agent
- Agent Skills Spec: https://agentskills.io/specification
- Pi Skills Repo: https://github.com/badlogic/pi-skills

### NPM Packages

- `@mariozechner/pi-coding-agent` - Core package
- `@yofriadi/pi-lsp` - LSP extension
- `@oh-my-pi/lsp` - LSP tool

### Related Projects

- Claude Code Skills: https://github.com/anthropics/skills
- Agent Skills Spec: https://github.com/agentskills/agentskills

---

## 11. Quick Reference Card

### Environment Variables
```bash
PI_CODING_AGENT_DIR    # Config directory (~/.pi/agent)
PI_PACKAGE_DIR         # Package directory override
PI_SKIP_VERSION_CHECK  # Skip version check
PI_CACHE_RETENTION     # long = extended cache
ANTHROPIC_API_KEY      # API key example
```

### CLI Commands
```bash
pi install <source>    # Install package
pi remove <source>     # Remove package
pi update              # Update packages
pi list                # List packages
pi config              # Enable/disable resources
pi --skill <path>      # Load skill
pi --extension <path>  # Load extension
```

### Skill Structure
```
skill-name/
├── SKILL.md      # Required (YAML frontmatter + Markdown)
├── scripts/      # Optional executables
├── references/   # Optional docs
└── assets/       # Optional resources
```

### SKILL.md Template
```markdown
---
name: skill-name
description: What this skill does and when to use it
---

# Skill Name

## Setup
```bash
npm install
```

## Usage
Instructions here...
```

---

*Research completed: March 2026*
*Pi Version: 0.57.1*
