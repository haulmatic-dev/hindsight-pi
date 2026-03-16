# Product Requirements Document: Pi Agent Skills Integration

## 1. Executive Summary

### Overview
This PRD specifies the integration of 13 curated Pi coding agent skills into Hydra's worker orchestration system. The skills enhance Pi's capabilities for Go and Angular/TypeScript development workflows through semantic code understanding, best practices enforcement, and systematic methodologies.

### Business Value
- **Enhanced Code Quality**: Access to 13 specialized skills covering Go patterns, Angular signals, TDD, and systematic debugging
- **Developer Productivity**: Automated skill updates ensure workers always use latest best practices
- **Consistent Standards**: Standardized skill sets across all Hydra workers
- **Reduced Setup Time**: Automatic installation and configuration of Pi ecosystem (agent, LSP, skills)

### Success Metrics
- 100% of Pi workers launch with all 13 skills loaded
- Zero manual intervention required for skill updates (daily automatic updates)
- <30 second skill installation time per worker
- 99.9% skill availability during worker execution

## 2. User Stories

### US-001: Automatic Pi Ecosystem Setup
**As a** DevOps engineer, **I want** Hydra to automatically install and configure Pi agent, LSP tools, and skills, **so that** I don't need manual setup on each deployment.

**Acceptance Criteria:**
- Hydra checks Pi installation on startup and auto-installs via npm if missing
- LSP CLI and language servers (gopls, typescript-language-server) auto-install if absent
- All 13 skills auto-install on first run via `npx skills add`
- Setup logs clearly indicate success/failure of each component

### US-002: Daily Skill Updates
**As a** system administrator, **I want** skills to update automatically every 24 hours, **so that** workers always use the latest skill versions without manual intervention.

**Acceptance Criteria:**
- Background goroutine runs skill update check every 24 hours
- Update process uses `npx skills add` to fetch latest versions
- Failed updates log errors but don't block worker spawning
- Update timestamp tracked in `pi-config/skills-installed.json`

### US-003: Skill-Aware Worker Spawning
**As a** Hydra operator, **I want** Pi workers to spawn with all 13 skills pre-loaded, **so that** each bead execution has access to the complete skill set.

**Acceptance Criteria:**
- Worker spawning sets `PI_CODING_AGENT_DIR` environment variable
- All 13 skills passed via `--skill` flags to Pi
- Skills loaded from `pi-config/skills/` directory
- Worker fails fast if skill directory is missing or empty

### US-004: LSP Integration
**As a** developer using Hydra, **I want** Pi workers to have access to Language Server Protocol for semantic code navigation, **so that** code analysis is accurate and context-aware.

**Acceptance Criteria:**
- LSP servers (gopls, typescript-language-server) auto-start when needed
- Pi can execute `lsp definition`, `lsp reference`, `lsp outline` commands
- LSP configuration read from `pi-config/agent/mcp.json`
- LSP skill (`lsp-code-analysis`) installed and functional

### US-005: Configuration Management
**As a** platform engineer, **I want** to configure Pi skills through `hydra.yaml`, **so that** I can customize behavior without code changes.

**Acceptance Criteria:**
- `hydra.yaml` supports `pi.config_dir`, `pi.skills.update_interval`, `pi.skills.auto_update`
- Configuration changes take effect on Hydra restart
- Sensible defaults provided for all settings
- Configuration validation prevents invalid settings

## 3. Functional Requirements

### FR-001a: FR-001: Pi Agent Installation Check (Part 1/2)
Hydra must verify Pi agent installation on startup and auto-install if missing.

**Acceptance Criteria:**
- Check for `pi` command in PATH
- If missing, run `npm install -g @mariozechner/pi-coding-agent`
- Verify installation success before proceeding

### FR-001b: FR-001: Pi Agent Installation Check (Part 2/2)

**Acceptance Criteria:**
- Log installation status clearly

### FR-002a: FR-002: LSP Infrastructure Setup (Part 1/2)
Hydra must ensure LSP CLI and language servers are installed and available.

**Acceptance Criteria:**
- Check for `lsp` command (Python lsp-client package)
- Check for `gopls` (Go language server)
- Check for `typescript-language-server` (TypeScript language server)

### FR-002b: FR-002: LSP Infrastructure Setup (Part 2/2)

**Acceptance Criteria:**
- Auto-install missing components with appropriate package managers
- Verify each component is functional before marking ready

### FR-003a: FR-003: Skill Installation on Startup (Part 1/2)
Hydra must install all 13 skills on first run if not already present.

**Acceptance Criteria:**
- Check `pi-config/skills-installed.json` for installation status
- If skills missing, execute installation script
- Install all 13 skills via `npx skills add <repo> --skill <name>`

### FR-003b: FR-003: Skill Installation on Startup (Part 2/2)

**Acceptance Criteria:**
- Verify each skill installed successfully
- Update tracking file with timestamp and skill count

### FR-004a: FR-004: Daily Skill Update Background Task (Part 1/2)
Hydra must run periodic skill updates via background goroutine.

**Acceptance Criteria:**
- Ticker runs every 24 hours (configurable)
- Re-installs all skills using `npx skills add`
- Handles partial failures (some skills may update, others fail)

### FR-004b: FR-004: Daily Skill Update Background Task (Part 2/2)

**Acceptance Criteria:**
- Logs update results with skill names and status
- Updates tracking file with new timestamp

### FR-005a: FR-005: Skill Directory Structure Creation (Part 1/2)
Hydra must create and maintain the pi-config directory structure.

**Acceptance Criteria:**
- Create `pi-config/agent/` directory on startup
- Create `pi-config/skills/` directory on startup
- Create `pi-config/agent/mcp.json` with LSP server configuration

### FR-005b: FR-005: Skill Directory Structure Creation (Part 2/2)

**Acceptance Criteria:**
- Create `pi-config/skills-installed.json` tracking file
- Set appropriate permissions on created directories

### FR-006a: FR-006: Pi Worker Environment Variable Configuration (Part 1/2)
Hydra must set required environment variables when spawning Pi workers.

**Acceptance Criteria:**
- Set `PI_CODING_AGENT_DIR` to `pi-config/agent` path
- Set `HYDRA_BEAD_TITLE` from bead metadata
- Set `HYDRA_BEAD_DESCRIPTION` from bead metadata

### FR-006b: FR-006: Pi Worker Environment Variable Configuration (Part 2/2)

**Acceptance Criteria:**
- Ensure environment variables are accessible to Pi process

### FR-007a: FR-007: Skill Flag Passing to Pi Workers (Part 1/2)
Hydra must pass all 13 skills as command-line flags when spawning Pi.

**Acceptance Criteria:**
- Build `--skill` flags for each of the 13 skills
- Use absolute paths to `pi-config/skills/<skill-name>`
- Maintain consistent skill order for reproducibility

### FR-007b: FR-007: Skill Flag Passing to Pi Workers (Part 2/2)

**Acceptance Criteria:**
- Handle missing skill directories gracefully with warnings

### FR-008a: FR-008: Configuration Schema Extension (Part 1/3)
Hydra configuration must support Pi-specific settings.

**Acceptance Criteria:**
- Add `pi` section to `hydra.yaml` schema
- Support `config_dir` setting (default: `./pi-config`)
- Support `skills.update_interval` (default: `24h`)

### FR-008b: FR-008: Configuration Schema Extension (Part 2/3)

**Acceptance Criteria:**
- Support `skills.auto_update` boolean (default: `true`)
- Support `skills.global` boolean (default: `true`)
- Support `lsp.enabled` boolean (default: `true`)

### FR-008c: FR-008: Configuration Schema Extension (Part 3/3)

**Acceptance Criteria:**
- Support `lsp.servers` map with command and install settings

### FR-009a: FR-009: MCP Server Configuration (Part 1/2)
Hydra must configure MCP servers for LSP integration.

**Acceptance Criteria:**
- Generate `mcp.json` with LSP server configurations
- Include gopls server configuration
- Include typescript-language-server configuration

### FR-009b: FR-009: MCP Server Configuration (Part 2/2)

**Acceptance Criteria:**
- Validate MCP JSON syntax before writing

### FR-010a: FR-010: Skill Installation Script (Part 1/2)
Hydra must provide shell scripts for manual skill management.

**Acceptance Criteria:**
- Create `scripts/setup.sh` for initial setup
- Create `scripts/install-skills.sh` for skill installation
- Create `scripts/update-skills.sh` for skill updates

### FR-010b: FR-010: Skill Installation Script (Part 2/2)

**Acceptance Criteria:**
- Scripts must be executable and handle errors gracefully
- Scripts must log progress and results

### FR-011a: FR-011: Graceful Degradation on Skill Failure (Part 1/2)
Hydra must continue operating if individual skills fail to install or update.

**Acceptance Criteria:**
- Log skill installation failures as warnings, not errors
- Allow worker spawning even if some skills unavailable
- Track unavailable skills in monitoring/logs

### FR-011b: FR-011: Graceful Degradation on Skill Failure (Part 2/2)

**Acceptance Criteria:**
- Retry failed skills on next update cycle

### FR-012a: FR-012: Health Checks for Skills (Part 1/2)
Hydra should verify skill availability before spawning workers.

**Acceptance Criteria:**
- Check if skill directory exists and is not empty
- Verify minimum required skills are present (configurable threshold)
- Log health check results

### FR-012b: FR-012: Health Checks for Skills (Part 2/2)

**Acceptance Criteria:**
- Block worker spawning if critical skills missing (optional)

## 4. Non-Functional Requirements

### NFR-001: Performance
- Skill installation must complete within 60 seconds per skill
- Daily update check must complete within 5 minutes total
- Worker spawning latency increase due to skills: <500ms
- Memory overhead per skill: <50MB

### NFR-002: Reliability
- 99.9% uptime for skill system components
- Automatic retry on transient skill installation failures (max 3 attempts)
- Graceful handling of npm/npx network issues
- No single point of failure (skills are additive, not required)

### NFR-003: Security
- All skill installations verified via checksums where available
- Skills run with minimal permissions (no root required)
- API keys passed via environment variables, never logged
- Network requests only to trusted repositories (GitHub)

### NFR-004: Observability
- Structured logging (JSON) for all skill operations
- Metrics exposed for skill installation success/failure rates
- Update timestamps tracked and queryable
- Health check endpoint for skill system status

### NFR-005: Compatibility
- Skills compatible with Pi coding agent standard format
- LSP servers compatible with LSP protocol 3.17
- Backwards compatible with existing Hydra configurations
- Works on Linux, macOS, and Windows (WSL)

## 5. Technical Considerations

### 5.1 Architecture Approach

**Skill Lifecycle:**
```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Install    │────▶│    Load      │────▶│   Execute    │
│  (npx)       │     │   (Pi)       │     │   (Worker)   │
└──────────────┘     └──────────────┘     └──────────────┘
       │                    │                    │
       ▼                    ▼                    ▼
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│  Daily Check │     │   Memory     │     │   LSP        │
│  (update)    │     │   (cache)    │     │   (analyze)  │
└──────────────┘     └──────────────┘     └──────────────┘
```

### 5.2 Integration Points

**Modified Files:**
- `internal/core/config.go` - Add PiConfig and PiSkillsConfig structs
- `internal/agents/pi.go` - Add skill path building and flag passing
- `internal/core/overseer.go` - Add startup checks and background task

**New Files:**
- `scripts/setup.sh` - One-time setup script
- `scripts/install-skills.sh` - Skill installation script
- `scripts/update-skills.sh` - Skill update script
- `pi-config/agent/mcp.json` - MCP server configuration (generated)
- `pi-config/skills-installed.json` - Installation tracking (generated)

### 5.3 Technology Choices

| Component | Technology | Rationale |
|-----------|-----------|-----------|
| Pi Agent | @mariozechner/pi-coding-agent | Target agent platform |
| Skill Installation | npx skills add | Standard Pi skill format |
| LSP Servers | gopls, typescript-language-server | Official language servers |
| Configuration | YAML (hydra.yaml) | Consistent with existing |
| Scheduling | time.Ticker | Native Go, simple periodic tasks |
| Shell Scripts | Bash | Universal availability |

### 5.4 Data Models

**PiSkillsConfig:**
```go
type PiSkillsConfig struct {
    UpdateInterval string       `yaml:"update_interval"`
    AutoUpdate     bool         `yaml:"auto_update"`
    Global         bool         `yaml:"global"`
    List           []SkillEntry `yaml:"list"`
}

type SkillEntry struct {
    Name string `yaml:"name"`
    Repo string `yaml:"repo"`
}
```

**Tracking File Structure:**
```json
{
  "last_updated": "2026-03-12T15:30:00Z",
  "skills_count": 13,
  "skills": [
    {"name": "golang-pro", "version": "latest", "status": "installed"},
    ...
  ],
  "update_interval": "24h"
}
```

## 6. Risks and Mitigation

### Risk 1: npm/npx Network Failures
**Impact:** High - Skills cannot install without network
**Likelihood:** Medium - GitHub/npm outages occur
**Mitigation:** 
- Implement retry logic with exponential backoff (3 attempts)
- Cache skill installations locally
- Allow workers to spawn with cached skills even if update fails
- Log failures for manual intervention

### Risk 2: Skill Version Incompatibility
**Impact:** Medium - Updated skills may break existing workflows
**Likelihood:** Low - Skills follow semantic versioning
**Mitigation:**
- Pin skill versions in configuration (optional)
- Test skill updates in staging environment first
- Maintain rollback capability
- Monitor worker success rates after updates

### Risk 3: Disk Space Exhaustion
**Impact:** Medium - 13 skills consume significant disk space
**Likelihood:** Low - Modern systems have ample storage
**Mitigation:**
- Monitor disk usage during skill installation
- Clean old skill versions (npx manages this)
- Document disk space requirements (~500MB)
- Alert on low disk space

### Risk 4: LSP Server Resource Consumption
**Impact:** Medium - LSP servers consume memory/CPU
**Likelihood:** High - LSP servers run continuously
**Mitigation:**
- Start LSP servers on-demand, not at startup
- Implement LSP server health checks
- Kill idle LSP servers after timeout
- Monitor resource usage

### Risk 5: Configuration Drift
**Impact:** Low - Multiple Hydra instances may have different skill versions
**Likelihood:** Medium - Updates happen asynchronously
**Mitigation:**
- Document expected skill versions
- Include skill version info in worker logs
- Centralized configuration management (future enhancement)

## 7. MoSCoW Prioritization

### Must Have
- FR-001: Pi Agent Installation Check
- FR-002: LSP Infrastructure Setup
- FR-003: Skill Installation on Startup
- FR-006: Pi Worker Environment Variable Configuration
- FR-007: Skill Flag Passing to Pi Workers
- FR-008: Configuration Schema Extension

### Should Have
- FR-004: Daily Skill Update Background Task
- FR-005: Skill Directory Structure Creation
- FR-010: Skill Installation Script
- FR-011: Graceful Degradation on Skill Failure
- NFR-001: Performance requirements
- NFR-002: Reliability requirements

### Could Have
- FR-009: MCP Server Configuration
- FR-012: Health Checks for Skills
- NFR-003: Security hardening
- NFR-004: Advanced observability

### Won't Have (Phase 1)
- Custom skill repositories (beyond the 13 curated)
- Per-project skill configuration
- Skill marketplace integration
- AI-powered skill recommendations

## 8. Success Metrics

### Technical Metrics
| Metric | Target | Measurement |
|--------|--------|-------------|
| Skill Installation Success Rate | >99% | Log analysis of install attempts |
| Worker Spawn Time | <5s | Time from spawn call to Pi start |
| Skill Update Duration | <5 min | Total time for all 13 skills |
| Pi Worker Success Rate | >95% | Beads completed successfully |

### Business Metrics
| Metric | Target | Measurement |
|--------|--------|-------------|
| Manual Setup Time | 0 min | No manual steps required |
| Skill Currency | <24h | Time since last skill update |
| Configuration Errors | 0 | Invalid hydra.yaml issues |

## 9. Research Insights

### 9.1 Codebase Findings
- Hydra already has PiConnector in `internal/agents/pi.go` using RPC mode
- Worker spawning passes environment variables via `HYDRA_*` prefix
- Configuration supports nested structs suitable for Pi skills
- Existing tool clients (UBS, BV, Beads) follow CLI wrapper pattern

### 9.2 Historical Context
- Agent connector architecture added March 2026 (commit a047635)
- Worker environment variables enhanced March 2026 (commit bf2e91f)
- Pi is currently the default agent in Hydra
- Configuration supports both array and object formats for flexibility

### 9.3 Best Practices Applied
- **Process Isolation**: Each skill runs independently via Pi
- **Graceful Degradation**: Skills enhance but don't block when unavailable
- **Hierarchical Configuration**: Global defaults → hydra.yaml → environment
- **Activity-Based Monitoring**: Workers use activity timeouts vs wall-clock
- **Structured Logging**: JSON format for observability

### 9.4 Library Capabilities
- Pi skills follow Agent Skills standard (agentskills.io)
- Skills are directories with `SKILL.md` files (YAML frontmatter + Markdown)
- Installation via `npx skills add <repo> --skill <name>`
- Environment variable `PI_CODING_AGENT_DIR` overrides config directory
- LSP integration via `@yofriadi/pi-lsp` extension (multi-server support)

### 9.5 Domain Expertise
- **Go Ecosystem**: gopls is official Go team LSP implementation
- **TypeScript Ecosystem**: typescript-language-server is community standard
- **LSP Protocol**: Version 3.17 current, 3.18 upcoming
- **Failure Modes**: Server crashes, protocol mismatches, sync issues most common
- **Security**: Checksum verification, minimal permissions, audit logging

## Appendix A: Complete Skill Inventory

| # | Skill Name | Repository | Category | Weekly Installs |
|---|-----------|-----------|----------|-----------------|
| 1 | golang-pro | jeffallan/claude-skills | Go Development | 4.9K |
| 2 | golang-testing | affaan-m/everything-claude-code | Go Testing | 1.6K |
| 3 | golang-patterns | affaan-m/everything-claude-code | Go Patterns | 1.9K |
| 4 | angular-component | analogjs/angular-skills | Angular | - |
| 5 | angular-signals | analogjs/angular-skills | Angular | - |
| 6 | angular-testing | analogjs/angular-skills | Angular | - |
| 7 | typescript-advanced-types | wshobson/agents | TypeScript | 12.7K |
| 8 | vitest | antfu/skills | Testing | 8.2K |
| 9 | implement-design | figma/mcp-server-guide | Design | 4.3K |
| 10 | systematic-debugging | obra/superpowers | Methodology | 27.7K |
| 11 | test-driven-development | obra/superpowers | Methodology | 23.0K |
| 12 | code-review | supercent-io/skills-template | Methodology | 10.8K |
| 13 | lsp-code-analysis | lsp-client/lsp-skill | LSP | 113 |

## Appendix B: File Locations

**Configuration:**
- Main config: `hydra.yaml`
- Pi config: `pi-config/agent/mcp.json`
- Tracking: `pi-config/skills-installed.json`

**Scripts:**
- Setup: `scripts/setup.sh`
- Install: `scripts/install-skills.sh`
- Update: `scripts/update-skills.sh`

**Source Code:**
- Config: `internal/core/config.go`
- Pi Connector: `internal/agents/pi.go`
- Overseer: `internal/core/overseer.go`

## Appendix C: Configuration Example

```yaml
pi:
  config_dir: "./pi-config"
  skills:
    update_interval: "24h"
    auto_update: true
    global: true
    list:
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
        args: ["serve"]
        install_check: "which gopls"
        install_cmd: "go install golang.org/x/tools/gopls@latest"
      typescript:
        command: "typescript-language-server"
        args: ["--stdio"]
        install_check: "which typescript-language-server"
        install_cmd: "npm install -g typescript-language-server typescript"
```
