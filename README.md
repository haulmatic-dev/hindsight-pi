# Hydra - Agent Swarm Orchestrator

A self-improving agent swarm orchestrator for software development workflows. Hydra manages ephemeral workers that execute tasks (beads) with intelligent dependency resolution.

## 🎯 Overview

Hydra is the orchestration layer for the Beads task management system. It:

- **Discovers** projects automatically from `~/source-code/`
- **Schedules** tasks based on dependencies (DAG)
- **Spawns** ephemeral workers (one per task)
- **Learns** from execution history via cass_memory
- **Monitors** via REST API and real-time updates

## 🏗️ Architecture

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│   Beads     │────▶│   Overseer   │────▶│   Workers   │
│   (Tasks)   │     │   (Go)       │     │(Ephemeral)  │
└─────────────┘     └──────────────┘     └─────────────┘
                           │
                    ┌──────┴──────┐
                    │     DAG     │
                    │  (Kahn's    │
                    │   Algorithm)│
                    └─────────────┘
```

### Core Components

| Component | Purpose | Technology |
|-----------|---------|------------|
| **Overseer** | Main scheduler daemon | Go 1.23 |
| **DAG** | Dependency resolution | Custom implementation |
| **Beads Client** | Task discovery | `br` CLI integration |
| **REST API** | Monitoring & control | Gin framework |
| **Workers** | Task execution | Subprocess |

## 🚀 Quick Start

### Prerequisites

- Go 1.23 or later
- `br` CLI tool (beads management)
- Projects with `.beads/` directories in `~/source-code/`

### Installation

```bash
# Clone repository
git clone https://github.com/haulmatic-dev/hydra.git
cd hydra

# Build binaries
make build

# Or manually:
go build -o bin/hydra cmd/hydra/main.go
go build -o bin/worker cmd/worker/main.go
```

### Configuration

Create `hydra.yaml`:

```yaml
overseer:
  poll_interval: 5s
  max_workers: 6
  max_retries: 3
  retry_delay: 10s
  worker_timeout: 30m

api:
  enabled: true
  port: 8080

projects:
  auto_discover: true
  base_path: ~/source-code
```

### Running

```bash
# Start Hydra
./bin/hydra -config hydra.yaml

# Debug mode
./bin/hydra -debug
```

## 📡 API Endpoints

### Health & Stats

```bash
# Health check
GET /health

# System statistics
GET /api/v1/stats

# DAG statistics
GET /api/v1/dag/stats
```

### Projects & Workers

```bash
# List discovered projects
GET /api/v1/projects

# List active workers
GET /api/v1/workers

# List ready beads
GET /api/v1/dag/ready
```

### Control

```bash
# Pause overseer
POST /api/v1/overseer/pause

# Resume overseer
POST /api/v1/overseer/resume

# Set max workers
POST /api/v1/overseer/max-workers
{
  "count": 10
}
```

## 🔧 Deployment

### Systemd Service

Create `/etc/systemd/system/hydra.service`:

```ini
[Unit]
Description=Hydra Agent Orchestrator
After=network.target

[Service]
Type=simple
User=hydra
Group=hydra
WorkingDirectory=/opt/hydra
ExecStart=/opt/hydra/bin/hydra -config /etc/hydra/hydra.yaml
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl enable hydra
sudo systemctl start hydra
sudo systemctl status hydra
```

### Docker

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o hydra cmd/hydra/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/hydra .
COPY hydra.yaml .
CMD ["./hydra"]
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `HYDRA_CONFIG` | Config file path | `hydra.yaml` |
| `HYDRA_DEBUG` | Enable debug logging | `false` |
| `HYDRA_BASE_PATH` | Projects base path | `~/source-code` |

## 📊 Monitoring

### Metrics

The overseer exposes these metrics:

- `dag_build_duration_ms` - Time to build dependency graph
- `ready_beads_count` - Number of beads ready to execute
- `active_workers_count` - Currently running workers
- `cycle_detected_total` - Number of cycles detected

### Logging

Structured logging with levels:

```bash
# View logs
journalctl -u hydra -f

# Debug level
./bin/hydra -debug 2>&1 | jq .
```

### Health Checks

```bash
# Kubernetes probe
kubectl exec -it <pod> -- wget -qO- http://localhost:8080/health
```

## 🧪 Testing

```bash
# Run all tests
go test ./...

# Run with race detector
go test -race ./...

# Run benchmarks
go test -bench=. ./...

# Coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## 🔒 Security

- **File Permissions**: Ensure config files are readable only by hydra user
- **API Authentication**: Add middleware for production use
- **Worker Isolation**: Workers run as subprocess with limited permissions
- **Network**: Bind to localhost only by default

## 🐛 Troubleshooting

### Common Issues

**Issue:** No beads found
```bash
# Verify projects exist
ls ~/source-code/*/.beads/

# Check auto-discovery
./bin/hydra -debug 2>&1 | grep "discovered"
```

**Issue:** Workers not spawning
```bash
# Check overseer status
curl http://localhost:8080/api/v1/stats

# Verify DAG has ready beads
curl http://localhost:8080/api/v1/dag/ready
```

**Issue:** Cycle detected
```bash
# View logs for cycle path
journalctl -u hydra | grep "circular dependency"
```

## 📝 Configuration Reference

### Overseer Settings

```yaml
overseer:
  poll_interval: 5s          # How often to check for new beads
  max_workers: 6             # Maximum concurrent workers
  max_retries: 3             # Max retries for failed beads
  retry_delay: 10s           # Base delay between retries
  worker_timeout: 30m        # Max time per worker
```

### Project Discovery

```yaml
projects:
  auto_discover: true
  base_path: ~/source-code
  # Or specify manually:
  # list:
  #   - name: myproject
  #     path: ~/source-code/myproject
  #     type: single
```

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Write tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

## 📄 License

MIT License - See LICENSE file for details

## 🔗 Related Projects

- [Beads](https://github.com/haulmatic-dev/beads) - Task management CLI
- [cass_memory](https://github.com/haulmatic-dev/cass_memory) - Pattern learning system

---

**Built with ❤️ by the Haulmatic team**