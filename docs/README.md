# Hydra Documentation

Quick links to everything you need.

---

## 🚀 Getting Started

| Document | Who | What |
|----------|-----|------|
| [Quick Start](#quick-start) | Everyone | Build, configure, and run Hydra in 5 minutes |
| [User Guide](guides/user-guide.md) | Developers | Configure your projects and git identity |
| [Admin Guide](guides/admin-guide.md) | DevOps / SRE | Run Hydra as a multi-user daemon |
| [Setup](setup.md) | Developers | PATH and wrapper script setup |

## 📋 Guides

| Document | Description |
|----------|-------------|
| [User Guide](guides/user-guide.md) | Configure `~/.hydra/config.yaml`, projects, tokens |
| [Admin Guide](guides/admin-guide.md) | systemd, security, monitoring, troubleshooting |
| [Deployment](DEPLOYMENT.md) | Production deployment checklist |
| [Monitoring](MONITORING.md) | Metrics, logging, alerts |

## 🏗 Architecture

| Document | Description |
|----------|-------------|
| [System Loop](architecture/system-loop.md) | The three nested control loops and whether each closes — start here |
| [Shepherd Layer](architecture/shepherd.md) | Events, quarantine, fix beads, concurrency governor, verdict memory — how failure is bounded and observed |
| [System Architecture](architecture/system-architecture-20260309.md) | High-level system design |
| [Bead Processing Flow](architecture/bead-processing-flow-20260309.md) | How beads move through the system |
| [Worker Lifecycle](architecture/worker-lifecycle-20260309.md) | Spawn, monitor, complete |
| [Bead Structure Guide](architecture/bead-structure-guide.md) | Bead format and schema |
| [Agent Connector](architecture/agent-connector-20260311.md) | Modular agent integration |
| [Dependency Resolution](architecture/decisions/001-dependency-resolution.md) | DAG and dependency design |

## 🔧 Operations

| Document | Description |
|----------|-------------|
| [Configuration Guide](operations/configuration-guide-20260309.md) | Full config reference |
| [Bead Validation System](operations/bead-validation-system.md) | TDD and PRD validation |
| [Hindsight Performance](operations/hindsight-performance-report.md) | Memory and performance tuning |
| [Charon Cleanup](charon-process-group-cleanup.md) | Orphan process management |

## 🧠 Intelligence & Code Analysis

| Document | Description |
|----------|-------------|
| [Code Intelligence](code-intelligence-20260312.md) | TLDR and GPTCache integration |
| [Hindsight Client](hindsight-client-analysis.md) | Context retrieval system |
| [Hindsight Integration](hindsight-integration-analysis.md) | Setup and configuration |
| [Hindsight Memory Management](HINDSIGHT_MEMORY_MANAGEMENT.md) | Memory retention policies |
| [Hindsight Memory Optimization](HINDSIGHT_MEMORY_OPTIMIZATION.md) | Performance optimization |
| [LSP Integration](LSP_INTEGRATION_SUMMARY.md) | Language server protocol setup |

## 🤖 Agents & Skills

| Document | Description |
|----------|-------------|
| [Pi Agent Skills](pi-agent-skills-20260312.md) | Available Pi skills overview |
| [Pi Skills Implementation](pi-agent-skills-implementation.md) | Adding new skills |
| [Pi Coding Agent Research](pi-coding-agent-research.md) | Agent capabilities study |
| [Dynamic Tool Client Refresh](dynamic-tool-client-refresh.md) | Hot-reloading tool configs |

## 📐 Product Requirements (PRDs)

| Document | Description |
|----------|-------------|
| [Closed-Loop Compliance](PRDs/20260603-closed-loop-compliance-system.md) | Auto-compliance checking |
| [Epic Support](PRDs/epic-support-20250319.md) | Epic bead lifecycle |
| [External Tool Integrations](PRDs/external-tool-integrations-20260309-v2.md) | Third-party integrations |
| [Hindsight Integration](PRDs/hindsight-integration-20260317.md) | Context memory PRD |
| [MCP Agent Mail](PRDs/mcp-agent-mail-integration-20260316.md) | MCP mail integration |
| [Pi Agent Skills](PRDs/pi-agent-skills-20260312.md) | Skills architecture PRD |
| [Self-Healing Validation](PRDs/self-healing-validation-loops-20260523.md) | Auto-retry and validation |

## 🧪 Collective Unconscious

| Document | Description |
|----------|-------------|
| [README](collective-unconscious/README.md) | Pattern extraction system |
| [Test Results](collective-unconscious/test-results/TEST_RESULTS.md) | E2E test results |

---

## Quick Start

```bash
# Clone and build
git clone https://github.com/haulmatic-dev/hydra.git
cd hydra
go build -o bin/hydra ./cmd/hydra

# Create your user config
mkdir -p ~/.hydra
cat > ~/.hydra/config.yaml <<'YAML'
user:
  name: Your Name
  email: you@example.com
git:
  provider: github
  username: your-username
  token_file: ~/.hydra/.github-token
projects:
  - name: my-project
    path: ~/projects/my-project
    type: single
YAML

echo "ghp_xxxxxxxxxxxxxxxxxxxx" > ~/.hydra/.github-token
chmod 600 ~/.hydra/.github-token

# Run Hydra
sudo ./bin/hydra -config hydra-config.yaml
```

See [User Guide](guides/user-guide.md) for full config reference.
See [Admin Guide](guides/admin-guide.md) for running as a systemd service.
