#!/usr/bin/env bash
#
# FR-010: Skill Installation Script - Skills Installer
# Installs all 13 skills via npx skills add
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

# Configuration
readonly CONFIG_DIR="${HOME}/.config/pi"

# Array of skills to install
readonly SKILLS=(
    "golang-pro"
    "golang-testing"
    "golang-patterns"
    "angular-component"
    "angular-signals"
    "angular-testing"
    "typescript-advanced-types"
    "vitest"
    "implement-design"
    "systematic-debugging"
    "test-driven-development"
    "code-review"
    "lsp-code-analysis"
)

# Counters for summary
declare -i SUCCESS_COUNT=0
declare -i FAILURE_COUNT=0
declare -a FAILED_SKILLS=()

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
    
    # Check if pi-config directory exists
    if [[ ! -d "$CONFIG_DIR" ]]; then
        error_exit "Pi configuration directory not found at $CONFIG_DIR. Please run ./scripts/setup.sh first." 1
    fi
    
    log_success "Configuration directory found: $CONFIG_DIR"
    
    # Check if npx is available
    if ! check_command npx; then
        error_exit "npx is not available. Please ensure npm is properly installed." 1
    fi
    
    # Check if pi agent is installed
    if ! check_command pi; then
        log_warn "Pi agent command not found in PATH. Skills may still install via npx."
    else
        log_success "Pi agent found"
    fi
    
    log_success "All prerequisites met"
}

# Install a single skill
install_skill() {
    local skill_name="$1"
    
    log_info "Installing skill: $skill_name..."
    
    # Attempt to install the skill
    if npx pi skills add "$skill_name" --yes 2>&1; then
        log_success "Successfully installed: $skill_name"
        ((SUCCESS_COUNT++))
        return 0
    else
        log_error "Failed to install: $skill_name"
        ((FAILURE_COUNT++))
        FAILED_SKILLS+=("$skill_name")
        return 1
    fi
}

# Install all skills
install_all_skills() {
    log_info "Starting installation of ${#SKILLS[@]} skills..."
    log_info "This may take several minutes depending on your connection."
    echo
    
    for skill in "${SKILLS[@]}"; do
        install_skill "$skill"
        echo
    done
}

# Print summary
print_summary() {
    echo
    log_info "Installation Summary"
    echo "========================================"
    log_success "Successfully installed: $SUCCESS_COUNT/${#SKILLS[@]}"
    
    if [[ $FAILURE_COUNT -gt 0 ]]; then
        log_error "Failed to install: $FAILURE_COUNT/${#SKILLS[@]}"
        echo
        log_error "Failed skills:"
        for skill in "${FAILED_SKILLS[@]}"; do
            echo "  - $skill"
        done
        echo
        log_warn "You can retry installation by running this script again."
        log_warn "Or install individual skills manually with: npx pi skills add <skill-name>"
        return 1
    else
        log_success "All skills installed successfully!"
        return 0
    fi
}

# Main function
main() {
    log_info "Starting skill installation..."
    log_info "Configuration directory: $CONFIG_DIR"
    
    # Check prerequisites
    check_prerequisites
    
    # Install all skills
    install_all_skills
    
    # Print summary
    if print_summary; then
        log_success "Skill installation completed successfully!"
        exit 0
    else
        log_error "Skill installation completed with errors."
        exit 2
    fi
}

# Handle script interruption
trap 'error_exit "Installation interrupted" 130' INT TERM

# Run main function
main "$@"
