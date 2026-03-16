#!/usr/bin/env bash
#
# FR-010: Skill Installation Script - Setup
# Initial setup script for Pi agent and development environment
#

set -euo pipefail

# Colors for output
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly NC='\033[0m' # No Color

# Script directory
readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Error handler
error_exit() {
    log_error "$1"
    exit "${2:-1}"
}

# Check if command exists
check_command() {
    if ! command -v "$1" &> /dev/null; then
        return 1
    fi
    return 0
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check if npm is installed
    if ! check_command npm; then
        error_exit "npm is not installed. Please install Node.js and npm first." 1
    fi
    
    local npm_version
    npm_version=$(npm --version)
    log_success "npm found: version $npm_version"
    
    # Check if node is installed
    if ! check_command node; then
        error_exit "node is not installed. Please install Node.js first." 1
    fi
    
    local node_version
    node_version=$(node --version)
    log_success "node found: version $node_version"
    
    # Check if npx is available
    if ! check_command npx; then
        error_exit "npx is not available. Please ensure npm is properly installed." 1
    fi
    
    log_success "All prerequisites met"
}

# Install Pi agent
install_pi_agent() {
    log_info "Installing Pi agent (@mariozechner/pi-coding-agent)..."
    
    if check_command pi; then
        log_warn "Pi agent is already installed. Checking for updates..."
    fi
    
    if npm install -g @mariozechner/pi-coding-agent; then
        log_success "Pi agent installed successfully"
    else
        error_exit "Failed to install Pi agent. Please check your npm permissions." 2
    fi
}

# Install LSP tools
install_lsp_tools() {
    log_info "Installing Language Server Protocol tools..."
    
    # Install gopls (Go language server)
    if check_command go; then
        log_info "Installing gopls (Go language server)..."
        if go install golang.org/x/tools/gopls@latest; then
            log_success "gopls installed successfully"
        else
            log_warn "Failed to install gopls. You may need to install it manually."
        fi
    else
        log_warn "Go is not installed. Skipping gopls installation."
        log_warn "To install gopls later, run: go install golang.org/x/tools/gopls@latest"
    fi
    
    # Install typescript-language-server
    log_info "Installing typescript-language-server..."
    if npm install -g typescript-language-server; then
        log_success "typescript-language-server installed successfully"
    else
        log_warn "Failed to install typescript-language-server. You may need to install it manually."
    fi
}

# Create pi-config directory structure
create_config_structure() {
    log_info "Creating Pi configuration directory structure..."
    
    local config_dir="$HOME/.config/pi"
    
    # Create main config directory
    if [[ ! -d "$config_dir" ]]; then
        if mkdir -p "$config_dir"; then
            log_success "Created $config_dir"
        else
            error_exit "Failed to create $config_dir" 3
        fi
    else
        log_warn "Directory $config_dir already exists"
    fi
    
    # Create subdirectories
    local subdirs=("skills" "templates" "logs")
    for subdir in "${subdirs[@]}"; do
        if [[ ! -d "$config_dir/$subdir" ]]; then
            if mkdir -p "$config_dir/$subdir"; then
                log_success "Created $config_dir/$subdir"
            else
                log_warn "Failed to create $config_dir/$subdir"
            fi
        else
            log_warn "Directory $config_dir/$subdir already exists"
        fi
    done
    
    log_success "Configuration directory structure ready"
}

# Main setup function
main() {
    log_info "Starting Pi agent setup..."
    log_info "Project root: $PROJECT_ROOT"
    
    # Check prerequisites
    check_prerequisites
    
    # Install components
    install_pi_agent
    install_lsp_tools
    create_config_structure
    
    log_success "Setup completed successfully!"
    log_info "Next step: Run ./scripts/install-skills.sh to install skills"
    
    exit 0
}

# Handle script interruption
trap 'error_exit "Setup interrupted" 130' INT TERM

# Run main function
main "$@"
