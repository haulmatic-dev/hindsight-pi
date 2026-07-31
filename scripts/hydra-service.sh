#!/bin/bash
# Hydra Service Management Script
# Usage: ./hydra-service.sh {start|stop|restart|status|logs|update}

set -e

HYDRA_USER="hydra"
HYDRA_HOME="${HYDRA_HOME:-/opt/dev-tools/var/hydra}"
HYDRA_CONFIG="/etc/hydra/hydra.yaml"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_status() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

show_status() {
    echo "Hydra Service Status"
    echo "===================="
    systemctl status hydra --no-pager
    echo ""
    
    # Check API health
    if curl -s http://localhost:8080/health > /dev/null 2>&1; then
        print_status "API is healthy"
        curl -s http://localhost:8080/api/v1/stats | jq . 2>/dev/null || echo "Stats unavailable"
    else
        print_error "API is not responding"
    fi
}

show_logs() {
    local lines="${1:-50}"
    echo "Showing last $lines lines of Hydra logs..."
    echo ""
    
    if [[ -f /var/log/hydra/hydra.log ]]; then
        tail -n "$lines" /var/log/hydra/hydra.log
    else
        journalctl -u hydra -n "$lines" --no-pager
    fi
}

follow_logs() {
    echo "Following Hydra logs (Ctrl+C to exit)..."
    echo ""
    
    if [[ -f /var/log/hydra/hydra.log ]]; then
        tail -f /var/log/hydra/hydra.log
    else
        journalctl -u hydra -f
    fi
}

update_hydra() {
    echo "Updating Hydra..."
    
    # Check if we're in the git repo
    if [[ ! -d ".git" ]]; then
        print_error "Not in a git repository. Please run from Hydra source directory."
        exit 1
    fi
    
    # Pull latest changes
    print_info "Pulling latest changes..."
    git pull origin main || git pull origin develop
    
    # Stop service
    print_info "Stopping Hydra service..."
    systemctl stop hydra
    
    # Build new binaries
    print_info "Building new binaries..."
    go build -o "$HYDRA_HOME/bin/hydra" cmd/hydra/main.go
    go build -o "$HYDRA_HOME/bin/worker" cmd/worker/main.go
    
    chown "$HYDRA_USER:$HYDRA_USER" "$HYDRA_HOME/bin/"*
    
    # Start service
    print_info "Starting Hydra service..."
    systemctl start hydra
    
    sleep 2
    
    if systemctl is-active --quiet hydra; then
        print_status "Hydra updated successfully"
    else
        print_error "Failed to start Hydra after update"
        exit 1
    fi
}

check_health() {
    echo "Hydra Health Check"
    echo "=================="
    echo ""
    
    # Service status
    if systemctl is-active --quiet hydra; then
        print_status "Service is running"
    else
        print_error "Service is not running"
        return 1
    fi
    
    # API health
    if curl -s http://localhost:8080/health > /dev/null 2>&1; then
        print_status "API is responding"
        curl -s http://localhost:8080/health | jq . 2>/dev/null || true
    else
        print_error "API is not responding"
        return 1
    fi
    
    # Check DAG stats
    local stats
    stats=$(curl -s http://localhost:8080/api/v1/dag/stats 2>/dev/null)
    if [[ -n "$stats" ]]; then
        print_status "DAG is operational"
        echo "$stats" | jq . 2>/dev/null || echo "$stats"
    fi
    
    # Check workers
    local workers
    workers=$(curl -s http://localhost:8080/api/v1/workers 2>/dev/null)
    if [[ -n "$workers" ]]; then
        local count
        count=$(echo "$workers" | jq '. | length' 2>/dev/null || echo "0")
        print_info "Active workers: $count"
    fi
    
    echo ""
    print_status "All health checks passed"
}

backup_config() {
    local backup_dir="/var/backups/hydra"
    local timestamp
    timestamp=$(date +%Y%m%d_%H%M%S)
    
    mkdir -p "$backup_dir"
    
    cp "$HYDRA_CONFIG" "$backup_dir/hydra.yaml.$timestamp"
    tar -czf "$backup_dir/hydra-backup-$timestamp.tar.gz" -C /var/log/hydra . 2>/dev/null || true
    
    print_status "Backup created: $backup_dir/hydra.yaml.$timestamp"
}

# Main command handler
case "${1:-status}" in
    start)
        systemctl start hydra
        sleep 1
        show_status
        ;;
    stop)
        systemctl stop hydra
        print_status "Hydra stopped"
        ;;
    restart)
        systemctl restart hydra
        sleep 2
        show_status
        ;;
    status)
        show_status
        ;;
    logs)
        show_logs "${2:-50}"
        ;;
    follow)
        follow_logs
        ;;
    update)
        update_hydra
        ;;
    health)
        check_health
        ;;
    backup)
        backup_config
        ;;
    *)
        echo "Hydra Service Management"
        echo "========================"
        echo ""
        echo "Usage: $0 {start|stop|restart|status|logs|follow|update|health|backup}"
        echo ""
        echo "Commands:"
        echo "  start         Start Hydra service"
        echo "  stop          Stop Hydra service"
        echo "  restart       Restart Hydra service"
        echo "  status        Show service status and health"
        echo "  logs [n]      Show last n lines of logs (default: 50)"
        echo "  follow        Follow logs in real-time"
        echo "  update        Update Hydra to latest version"
        echo "  health        Run health checks"
        echo "  backup        Backup configuration and logs"
        echo ""
        exit 1
        ;;
esac
