# Hydra - Self-Improving Agent Swarm Orchestrator

Hydra is an autonomous agent swarm orchestrator designed for software development workflows. It spawns ephemeral agents (one per task/bead) that execute, learn from cass_memory and llm-tldr, and die after completion.

## Architecture

```
Hydra Orchestrator (Go)
├── Overseer - Main scheduler daemon
├── Workers - Ephemeral processes (one per bead)
├── MCP Agent Mail - Communication layer
└── Intelligence Layer - cass_memory, llm-tldr, GPTCache
```

## Features

- **One Agent = One Bead**: Each agent processes a single task and exits
- **Self-Improving**: Learns from cass_memory and llm-tldr
- **Multi-Repo Support**: Handles single and multi-repo projects
- **GraphQL API**: Real-time monitoring via subscriptions
- **MCP Integration**: File reservations and agent-to-agent communication

## Quick Start

```bash
# Start Hydra
cd cmd/hydra && go run main.go

# Or build and run
go build -o hydra cmd/hydra/main.go
./hydra
```

## Configuration

Hydra auto-discovers projects in `~/source-code/`. Create `hydra.yaml` to customize:

```yaml
overseer:
  poll_interval: 5s
  max_workers: 6
  
projects:
  - name: "my-project"
    path: "/Users/buddhi/source-code/my-project"
    type: "single"
```

## Development

```bash
# Run tests
go test ./...

# Build worker binary
go build -o bin/worker cmd/worker/main.go
```

## License

MIT
