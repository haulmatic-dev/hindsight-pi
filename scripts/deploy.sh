#!/bin/bash
# Hydra Production Deployment Script
# Usage: ./deploy.sh [environment]
# Environment: dev|staging|production (default: production)

set -e

# Git-ownership sweep (2026-11-08): root-run deploy git ops can leave
# root-owned entries in the shared .git module db, wedging the daemon's
# git add ("insufficient permission for adding an object to repository
# database"). Repair on every exit path — guarded so a missing helper
# can never fail the deploy (fail-open).
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
git_ownership_sweep() {
    if [ -x "$REPO_ROOT/scripts/fix-git-ownership.sh" ]; then
        "$REPO_ROOT/scripts/fix-git-ownership.sh" "$REPO_ROOT" || true
    fi
}
trap git_ownership_sweep EXIT
trap 'trap - EXIT; git_ownership_sweep; exit 143' TERM INT HUP

ENVIRONMENT="${1:-production}"
# Baseline for the post-deploy freshness assertion (see scripts/verify-deploy.sh).
DEPLOY_STARTED_AT="$(date +%s)"
HYDRA_USER="hydra"
HYDRA_HOME="${HYDRA_HOME:-/opt/dev-tools/var/hydra}"
HYDRA_CONFIG="/etc/hydra/hydra.yaml"
HYDRA_SERVICE="/etc/systemd/system/hydra.service"

echo "🚀 Hydra Production Deployment"
echo "================================"
echo "Environment: $ENVIRONMENT"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print status
print_status() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

# Check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        print_error "This script must be run as root"
        exit 1
    fi
}

# Create hydra user
create_user() {
    echo "Creating hydra user..."
    if id "$HYDRA_USER" &>/dev/null; then
        print_warning "User $HYDRA_USER already exists"
    else
        useradd -r -s /bin/false -d "$HYDRA_HOME" -m "$HYDRA_USER"
        print_status "Created user: $HYDRA_USER"
    fi
}

# Create directories
create_directories() {
    echo "Creating directories..."
    mkdir -p "$HYDRA_HOME"/{bin,logs,config}
    mkdir -p /etc/hydra
    mkdir -p /var/log/hydra
    mkdir -p /var/lib/hydra
    
    chown -R "$HYDRA_USER:$HYDRA_USER" "$HYDRA_HOME"
    chown -R "$HYDRA_USER:$HYDRA_USER" /var/log/hydra
    chown -R "$HYDRA_USER:$HYDRA_USER" /var/lib/hydra
    
    print_status "Created directories"
}

# Build Hydra binaries
build_binaries() {
    echo "Building Hydra binaries..."
    
    if [[ ! -d "cmd/hydra" ]]; then
        print_error "Not in Hydra repository root"
        exit 1
    fi

    # An in-flight run owns the daemon. Deploying restarts it, which kills every
    # worker mid-task and makes the run redo that work — the single largest
    # source of wasted capacity measured on 2026-07-30.
    if [[ -x "scripts/run-lock.sh" ]] && scripts/run-lock.sh check; then
        print_error "Deploy refused: $(scripts/run-lock.sh describe)"
        print_warning "Wait for it to finish, or override with HYDRA_IGNORE_RUN_LOCK=1"
        [[ "${HYDRA_IGNORE_RUN_LOCK:-0}" == "1" ]] || exit 1
    fi

    # Nightly known-answer verdict (Shepherd D2). Blocks only on a fresh `fail`
    # — a missing, stale, or inconclusive verdict warns and proceeds, because
    # absence of evidence is not evidence of a regression.
    if [[ -x "scripts/testbed-gate.sh" ]]; then
        if ! scripts/testbed-gate.sh; then
            print_error "Deploy blocked by the nightly testbed matrix (see above)"
            exit 1
        fi
    fi

    # Build the PACKAGES, not single files. `go build cmd/worker/main.go` compiles
    # main.go alone as "command-line-arguments" and silently ignores its siblings
    # (pi_agent_dir.go, pi_extensions.go, pi_rpc_turn.go), so the deploy died on
    # 2026-07-31 with eight "undefined:" errors for symbols that were right there
    # in the same package. Any file added beside main.go breaks a file-list build.
    go build -o "$HYDRA_HOME/bin/hydra" ./cmd/hydra
    print_status "Built hydra binary"

    go build -o "$HYDRA_HOME/bin/worker" ./cmd/worker
    print_status "Built worker binary"
    
    # Set permissions
    chown "$HYDRA_USER:$HYDRA_USER" "$HYDRA_HOME/bin/"*
    chmod +x "$HYDRA_HOME/bin/"*

    # Install the br shim as the daemon's `br`. The real br recreates
    # .beads/issues.jsonl mode 0600 on every rewrite, which re-locks the daemon
    # (or the human) out of shared stores after any write by the other party;
    # the shim repairs group perms inside the same invocation (2026-09-22
    # telematics + buddhi outage). $HYDRA_HOME/bin must precede the raw
    # beads-rust release dir on the unit's PATH for this to be the br the
    # daemon actually execs — see scripts/hydra.service.
    if [[ -f "packaging/br" ]]; then
        install -m 0755 -o "$HYDRA_USER" -g "$HYDRA_USER" packaging/br "$HYDRA_HOME/bin/br"
        print_status "Installed br shim to $HYDRA_HOME/bin/br"
    fi
}

# Create configuration
#
# PROVISIONING ONLY. This writes a canned starter config, so it must never touch
# an existing one. On 2026-07-31 an ordinary deploy overwrote the live
# /etc/hydra/hydra.yaml with this template and silently discarded every tuned
# setting — max_workers, the provider pool, the concurrency governor, the shared
# Pi config dir, hindsight, the project list — and the daemon came up on the
# template. A deploy builds and restarts; it does not reprovision.
create_config() {
    if [[ -f "$HYDRA_CONFIG" ]]; then
        print_status "Config exists at $HYDRA_CONFIG — leaving it alone"
        return 0
    fi
    if [[ "${HYDRA_PROVISION:-0}" != "1" ]]; then
        print_warning "No config at $HYDRA_CONFIG. Re-run with HYDRA_PROVISION=1 to write a starter config."
        return 0
    fi

    echo "Creating configuration..."

    cat > "$HYDRA_CONFIG" << 'EOF'
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
    timeout: 10s
  
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
EOF

    chown "$HYDRA_USER:$HYDRA_USER" "$HYDRA_CONFIG"
    chmod 600 "$HYDRA_CONFIG"
    print_status "Created configuration at $HYDRA_CONFIG"
}

# Create systemd service
#
# PROVISIONING ONLY, for the same reason as create_config. The template lacks
# things the live unit carries — EnvironmentFile=/etc/hydra/api-keys.env and the
# beads-rust/beads-viewer entries on PATH — so overwriting a working unit takes
# the daemon's API keys and `br` binary away with it. That happened on
# 2026-07-31.
create_systemd_service() {
    if [[ -f "$HYDRA_SERVICE" ]]; then
        print_status "Service unit exists at $HYDRA_SERVICE — leaving it alone"
        return 0
    fi
    if [[ "${HYDRA_PROVISION:-0}" != "1" ]]; then
        print_warning "No unit at $HYDRA_SERVICE. Re-run with HYDRA_PROVISION=1 to write a starter unit."
        return 0
    fi

    echo "Creating systemd service..."

    cat > "$HYDRA_SERVICE" << EOF
[Unit]
Description=Hydra Agent Orchestrator
Documentation=https://github.com/haulmatic-dev/hydra
After=network.target
Wants=network.target

[Service]
Type=simple
User=$HYDRA_USER
Group=$HYDRA_USER
WorkingDirectory=$HYDRA_HOME

Environment="HYDRA_CONFIG=$HYDRA_CONFIG"
Environment="HYDRA_HOME=$HYDRA_HOME"
Environment="PATH=$HYDRA_HOME/bin:/usr/local/bin:/usr/bin:/bin"
Environment="HYDRA_ENVIRONMENT=$ENVIRONMENT"

ExecStart=$HYDRA_HOME/bin/hydra -config $HYDRA_CONFIG
ExecReload=/bin/kill -HUP \$MAINPID

# Resource limits
LimitNOFILE=65536
LimitNPROC=4096

# Restart policy
Restart=always
RestartSec=10
StartLimitInterval=60s
StartLimitBurst=3

# Logging
StandardOutput=append:/var/log/hydra/hydra.log
StandardError=append:/var/log/hydra/hydra-error.log
SyslogIdentifier=hydra

[Install]
WantedBy=multi-user.target
EOF

    chmod 644 "$HYDRA_SERVICE"
    systemctl daemon-reload
    print_status "Created systemd service"
}

# Setup log rotation
setup_logrotate() {
    if [[ -f /etc/logrotate.d/hydra ]]; then
        print_status "logrotate config exists — leaving it alone"
        return 0
    fi
    if [[ "${HYDRA_PROVISION:-0}" != "1" ]]; then
        print_warning "No /etc/logrotate.d/hydra. Re-run with HYDRA_PROVISION=1 to write a starter rule."
        return 0
    fi

    echo "Setting up log rotation..."

    cat > /etc/logrotate.d/hydra << 'EOF'
/var/log/hydra/*.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    create 0640 hydra hydra
    sharedscripts
    postrotate
        systemctl reload hydra > /dev/null 2>&1 || true
    endscript
}
EOF

    print_status "Configured log rotation"
}

# Setup firewall
setup_firewall() {
    if [[ "${HYDRA_PROVISION:-0}" != "1" ]]; then
        return 0
    fi
    echo "Configuring firewall..."
    
    # Check if ufw is installed
    if command -v ufw &> /dev/null; then
        ufw allow 8080/tcp comment 'Hydra API'
        print_status "Configured UFW firewall (port 8080)"
    elif command -v firewall-cmd &> /dev/null; then
        firewall-cmd --permanent --add-port=8080/tcp
        firewall-cmd --reload
        print_status "Configured firewalld (port 8080)"
    else
        print_warning "No firewall detected, skipping firewall configuration"
    fi
}

# Install dependencies
check_dependencies() {
    echo "Checking dependencies..."
    
    # Check Go
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed. Please install Go 1.23+"
        exit 1
    fi
    
    # Check Go version
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    if [[ "$(printf '%s\n' "1.23" "$GO_VERSION" | sort -V | head -n1)" != "1.23" ]]; then
        print_error "Go version 1.23+ required, found $GO_VERSION"
        exit 1
    fi
    print_status "Go $GO_VERSION installed"
    
    # Check beads CLI
    if ! command -v br &> /dev/null; then
        print_warning "beads CLI (br) not found. Install with: pip install beads-cli"
    else
        print_status "beads CLI installed"
    fi
    
}

# Start service
start_service() {
    echo ""
    echo "Starting Hydra service..."
    
    systemctl enable hydra
    systemctl start hydra
    
    sleep 2
    
    if systemctl is-active --quiet hydra; then
        print_status "Hydra service started successfully"

        # Prove the running code is the code we just installed. `systemctl
        # is-active` only says a process exists — on 2026-07-30 the service was
        # "started successfully" all day while executing a binary from the
        # previous day, because the install wrote to a path nothing runs.
        if [[ -x "scripts/verify-deploy.sh" ]]; then
            if ! scripts/verify-deploy.sh "$DEPLOY_STARTED_AT"; then
                print_error "The running daemon is not the binary just installed — deploy NOT applied"
                exit 1
            fi
        fi
        echo ""
        echo "Service status:"
        systemctl status hydra --no-pager -l
    else
        print_error "Failed to start Hydra service"
        echo ""
        echo "Check logs: journalctl -u hydra -n 50"
        exit 1
    fi
}

# Print final instructions
print_instructions() {
    echo ""
    echo "================================"
    echo "🎉 Deployment Complete!"
    echo "================================"
    echo ""
    echo "Hydra is now running on port 8080"
    echo ""
    echo "Useful commands:"
    echo "  systemctl status hydra      # Check service status"
    echo "  systemctl stop hydra        # Stop service"
    echo "  systemctl restart hydra     # Restart service"
    echo "  journalctl -u hydra -f      # View logs"
    echo "  curl http://localhost:8080/health  # Health check"
    echo ""
    echo "Configuration: $HYDRA_CONFIG"
    echo "Logs: /var/log/hydra/"
    echo "Binaries: $HYDRA_HOME/bin/"
    echo ""
}

# Main deployment flow
main() {
    check_root
    check_dependencies
    create_user
    create_directories
    build_binaries
    create_config
    create_systemd_service
    setup_logrotate
    setup_firewall
    start_service
    print_instructions
}

# Run main function
main
