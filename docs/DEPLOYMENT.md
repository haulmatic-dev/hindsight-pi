# Production Deployment Guide

## Overview

This guide covers deploying Hydra to production environments using systemd service management.

## Table of Contents

- [Quick Start](#quick-start)
- [Prerequisites](#prerequisites)
- [Automated Deployment](#automated-deployment)
- [Manual Deployment](#manual-deployment)
- [Configuration](#configuration)
- [Service Management](#service-management)
- [Monitoring](#monitoring)
- [Security](#security)
- [Troubleshooting](#troubleshooting)
- [Backup and Recovery](#backup-and-recovery)
- [Updating](#updating)

## Quick Start

```bash
# Clone and deploy
git clone https://github.com/haulmatic-dev/hydra.git
cd hydra
sudo ./scripts/deploy.sh production

# Check status
sudo systemctl status hydra
curl http://localhost:8080/health
```

## Prerequisites

### System Requirements

- **OS:** Linux (Ubuntu 20.04+, CentOS 8+, Debian 11+)
- **CPU:** 2+ cores
- **Memory:** 4GB+ RAM
- **Disk:** 10GB+ free space
- **Network:** Internet access

### Software Requirements

- **Go:** 1.23 or later
- **Git:** 2.30+
- **Beads CLI:** `pip install beads-cli`
- **Pi Agent:** Latest version

### Optional Services

- **MCP Server:** For agent communication
- **cass_memory:** For pattern learning
- **TLDR:** For code intelligence
- **GPTCache:** For LLM caching

## Automated Deployment

The deployment script (`scripts/deploy.sh`) automates the entire process:

```bash
sudo ./scripts/deploy.sh [environment]

# Environments: dev, staging, production (default: production)
sudo ./scripts/deploy.sh production
```

### What the Script Does

1. **Creates `hydra` user** - Dedicated service account
2. **Builds binaries** - Compiles hydra and worker
3. **Creates directories** - Sets up proper file structure
4. **Installs configuration** - Creates `/etc/hydra/hydra.yaml`
5. **Sets up systemd** - Creates and enables service
6. **Configures logging** - Sets up log rotation
7. **Starts service** - Launches Hydra

### Script Options

```bash
# Check dependencies only
sudo ./scripts/deploy.sh --check

# Dry run (show what would be done)
sudo ./scripts/deploy.sh --dry-run

# Verbose output
sudo ./scripts/deploy.sh --verbose
```

## Manual Deployment

### Step 1: Create User

```bash
sudo useradd -r -s /bin/false -d /opt/hydra -m hydra
sudo usermod -aG docker hydra  # If using Docker
```

### Step 2: Install Binaries

```bash
# Create directories
sudo mkdir -p /opt/hydra/{bin,logs,config}
sudo mkdir -p /etc/hydra
sudo mkdir -p /var/log/hydra
sudo mkdir -p /var/lib/hydra

# Build
cd /path/to/hydra
go build -o /opt/hydra/bin/hydra cmd/hydra/main.go
go build -o /opt/hydra/bin/worker cmd/worker/main.go

# Set ownership
sudo chown -R hydra:hydra /opt/hydra
sudo chown -R hydra:hydra /var/log/hydra
sudo chown -R hydra:hydra /var/lib/hydra
```

### Step 3: Configuration

Create `/etc/hydra/hydra.yaml`:

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
  host: "127.0.0.1"

projects:
  auto_discover: true
  base_path: /home

intelligence:
  cass_memory:
    enabled: true
    query_limit: 10
    history_days: 30
  
  tldr:
    enabled: true
    base_url: "http://localhost:3000"
    timeout: 10s
    max_files: 50
    max_tokens: 100000
  
  gptcache:
    enabled: true
    server_url: "http://localhost:8000"

mcp:
  enabled: true
  url: "ws://127.0.0.1:8765"
  reconnect_interval: 5s
  max_reconnect_attempts: 10
  heartbeat_interval: 30s
  file_reservation_ttl: 30m

logging:
  level: info
  format: json
  output: /var/log/hydra/hydra.log
```

Set permissions:

```bash
sudo chown hydra:hydra /etc/hydra/hydra.yaml
sudo chmod 600 /etc/hydra/hydra.yaml
```

### Step 4: Systemd Service

Copy the service file:

```bash
sudo cp scripts/hydra.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable hydra
sudo systemctl start hydra
```

Service file location: `/etc/systemd/system/hydra.service`

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `HYDRA_CONFIG` | Config file path | `/etc/hydra/hydra.yaml` |
| `HYDRA_HOME` | Installation directory | `/opt/hydra` |
| `HYDRA_ENVIRONMENT` | Environment name | `production` |
| `HYDRA_DEBUG` | Enable debug logging | `false` |
| `HYDRA_LOG_LEVEL` | Log level | `info` |

### Configuration Files

**Main config:** `/etc/hydra/hydra.yaml`

**Service override:** `/etc/systemd/system/hydra.service.d/override.conf`

```ini
[Service]
Environment="HYDRA_DEBUG=true"
Environment="HYDRA_LOG_LEVEL=debug"
```

## Service Management

### Using systemd

```bash
# Check status
sudo systemctl status hydra

# Start service
sudo systemctl start hydra

# Stop service
sudo systemctl stop hydra

# Restart service
sudo systemctl restart hydra

# Reload config
sudo systemctl reload hydra

# View logs
sudo journalctl -u hydra -f
```

### Using Management Script

```bash
# Make executable
chmod +x scripts/hydra-service.sh

# Status
sudo ./scripts/hydra-service.sh status

# View logs (last 50 lines)
sudo ./scripts/hydra-service.sh logs

# Follow logs in real-time
sudo ./scripts/hydra-service.sh follow

# Health check
sudo ./scripts/hydra-service.sh health

# Update to latest version
sudo ./scripts/hydra-service.sh update

# Backup configuration
sudo ./scripts/hydra-service.sh backup
```

### Service Commands Reference

| Command | Description |
|---------|-------------|
| `status` | Show service status and health |
| `start` | Start the service |
| `stop` | Stop the service |
| `restart` | Restart the service |
| `logs [n]` | Show last n lines of logs |
| `follow` | Follow logs in real-time |
| `health` | Run comprehensive health checks |
| `update` | Update to latest version |
| `backup` | Backup configuration and logs |

## Monitoring

### Health Checks

```bash
# API health endpoint
curl http://localhost:8080/health

# System statistics
curl http://localhost:8080/api/v1/stats

# DAG statistics
curl http://localhost:8080/api/v1/dag/stats

# Active workers
curl http://localhost:8080/api/v1/workers

# Projects
curl http://localhost:8080/api/v1/projects
```

### Key Metrics

Monitor these metrics for system health:

- **active_workers_count** - Should be ≤ max_workers
- **ready_beads_count** - Beads waiting to execute
- **blocked_beads_count** - Beads waiting on dependencies
- **failed_beads_count** - Should be monitored for spikes
- **api_response_time** - Should be < 100ms

### Logging

**Log locations:**
- `/var/log/hydra/hydra.log` - Main application logs
- `/var/log/hydra/hydra-error.log` - Error logs
- `/var/log/syslog` or `journalctl` - System logs

**View logs:**
```bash
# Recent logs
sudo tail -f /var/log/hydra/hydra.log

# Last 100 lines
sudo tail -n 100 /var/log/hydra/hydra.log

# Since last boot
sudo journalctl -u hydra -b

# Specific time range
sudo journalctl -u hydra --since "1 hour ago"
```

**Log rotation:**
- Rotated daily
- 14 days retention
- Compressed old logs

## Security

### File Permissions

```bash
# Config file (sensitive)
sudo chmod 600 /etc/hydra/hydra.yaml
sudo chown hydra:hydra /etc/hydra/hydra.yaml

# Binaries
sudo chmod 755 /opt/hydra/bin/*
sudo chown root:root /opt/hydra/bin/*

# Logs
sudo chmod 640 /var/log/hydra/*
sudo chown hydra:hydra /var/log/hydra/*

# Data directory
sudo chmod 750 /var/lib/hydra
sudo chown hydra:hydra /var/lib/hydra
```

### Firewall

**UFW:**
```bash
sudo ufw allow 8080/tcp comment 'Hydra API'
sudo ufw reload
```

**Firewalld:**
```bash
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --reload
```

**iptables:**
```bash
sudo iptables -A INPUT -p tcp --dport 8080 -j ACCEPT
sudo iptables-save
```

### Reverse Proxy (Nginx)

Install Nginx:
```bash
sudo apt-get install nginx  # Ubuntu/Debian
sudo yum install nginx      # CentOS/RHEL
```

Configure (`/etc/nginx/sites-available/hydra`):

```nginx
upstream hydra {
    server 127.0.0.1:8080;
}

server {
    listen 80;
    server_name hydra.yourdomain.com;

    location / {
        proxy_pass http://hydra;
        proxy_http_version 1.1;
        
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # WebSocket support
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        
        # Timeouts
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }
}
```

Enable:
```bash
sudo ln -s /etc/nginx/sites-available/hydra /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx
```

### SSL/TLS (Let's Encrypt)

```bash
sudo apt-get install certbot python3-certbot-nginx
sudo certbot --nginx -d hydra.yourdomain.com
```

## Troubleshooting

### Service Won't Start

1. **Check logs:**
   ```bash
   sudo journalctl -u hydra -n 50 --no-pager
   ```

2. **Verify config syntax:**
   ```bash
   cat /etc/hydra/hydra.yaml | python3 -c 'import yaml,sys; yaml.safe_load(sys.stdin)'
   ```

3. **Check permissions:**
   ```bash
   ls -la /opt/hydra/bin/
   ls -la /etc/hydra/
   ls -la /var/log/hydra/
   ```

4. **Test manually:**
   ```bash
   sudo -u hydra /opt/hydra/bin/hydra -config /etc/hydra/hydra.yaml
   ```

### High Resource Usage

**Check usage:**
```bash
# CPU/Memory
sudo systemctl status hydra
ps aux | grep hydra

# Disk usage
du -sh /var/log/hydra/
du -sh /var/lib/hydra/

# Limit resources
# Edit /etc/systemd/system/hydra.service:
# LimitAS=2G
# LimitCPU=200%
# Then: sudo systemctl daemon-reload && sudo systemctl restart hydra
```

### Workers Failing

1. **Check worker logs:**
   ```bash
   sudo grep "worker" /var/log/hydra/hydra.log
   ```

2. **Verify dependencies:**
   ```bash
   which pi
   pi --version
   
   which br
   br --version
   ```

3. **Test manually:**
   ```bash
   HYDRA_BEAD_ID=test HYDRA_PROJECT=test /opt/hydra/bin/worker
   ```

### API Not Responding

1. **Check if running:**
   ```bash
   sudo systemctl is-active hydra
   curl http://localhost:8080/health
   ```

2. **Check port:**
   ```bash
   sudo netstat -tlnp | grep 8080
   sudo ss -tlnp | grep 8080
   ```

3. **Firewall:**
   ```bash
   sudo ufw status
   sudo iptables -L | grep 8080
   ```

## Backup and Recovery

### Automated Backup

The management script can create backups:

```bash
# Create backup
sudo ./scripts/hydra-service.sh backup

# Backups stored in:
# /var/backups/hydra/hydra.yaml.YYYYMMDD
# /var/backups/hydra/hydra-logs.YYYYMMDD.tar.gz
```

### Manual Backup

```bash
# Configuration
sudo cp /etc/hydra/hydra.yaml /backup/hydra.yaml.$(date +%Y%m%d)

# Logs
sudo tar -czf /backup/hydra-logs.$(date +%Y%m%d).tar.gz /var/log/hydra/

# Data
sudo tar -czf /backup/hydra-data.$(date +%Y%m%d).tar.gz /var/lib/hydra/
```

### Recovery

```bash
# Stop service
sudo systemctl stop hydra

# Restore config
sudo cp /backup/hydra.yaml.YYYYMMDD /etc/hydra/hydra.yaml
sudo chown hydra:hydra /etc/hydra/hydra.yaml
sudo chmod 600 /etc/hydra/hydra.yaml

# Restore logs (optional)
sudo tar -xzf /backup/hydra-logs.YYYYMMDD.tar.gz -C /var/log/

# Start service
sudo systemctl start hydra

# Verify
sudo systemctl status hydra
curl http://localhost:8080/health
```

## Updating

### Automated Update

```bash
# Update to latest version
sudo ./scripts/hydra-service.sh update
```

### Manual Update

```bash
# 1. Stop service
sudo systemctl stop hydra

# 2. Pull latest code
cd /opt/hydra
git pull origin main

# 3. Build new binaries
sudo -u hydra go build -o bin/hydra cmd/hydra/main.go
sudo -u hydra go build -o bin/worker cmd/worker/main.go

# 4. Start service
sudo systemctl start hydra

# 5. Verify
sudo systemctl status hydra
curl http://localhost:8080/health
```

### Rolling Update (Zero Downtime)

For multi-instance deployments:

1. Update instance 1
2. Wait for health check
3. Update instance 2
4. Repeat for all instances

## Multi-Server Deployment

For high availability:

1. **Load Balancer** (nginx/HAProxy)
2. **Multiple Hydra instances**
3. **Shared storage** for SQLite or use PostgreSQL
4. **MCP cluster** for message routing

## Production Checklist

### Pre-Deployment

- [ ] Build binaries for target architecture
- [ ] Test in staging environment
- [ ] Create configuration file
- [ ] Set up monitoring
- [ ] Configure backups

### Infrastructure

- [ ] Provision server (2+ CPU, 4GB+ RAM)
- [ ] Open port 8080 (API)
- [ ] Set up reverse proxy with SSL
- [ ] Configure firewall rules
- [ ] Set up log aggregation

### Security

- [ ] Create dedicated `hydra` user
- [ ] Set file permissions (600 for config)
- [ ] Enable API authentication
- [ ] Configure rate limiting
- [ ] Set up audit logging
- [ ] Rotate credentials

### Monitoring

- [ ] Deploy Prometheus
- [ ] Import Hydra dashboard
- [ ] Set up alerts:
  - [ ] High error rate
  - [ ] Worker failures
  - [ ] Cycle detection
  - [ ] API latency
  - [ ] Disk space
  - [ ] Memory usage

### Backup

- [ ] Schedule automated backups
- [ ] Test backup restoration
- [ ] Document recovery procedure
- [ ] Set up offsite backup

---

**Version:** 1.0  
**Last Updated:** 2026-03-08

## Support

- **Issues:** https://github.com/haulmatic-dev/hydra/issues
- **Documentation:** https://github.com/haulmatic-dev/hydra/tree/main/docs
