#!/bin/bash
# Update Pi Agent Skills Script
# Updates all 13 skills via npx skills add and tracks progress
# Usage: ./update-skills.sh [--force]

set -e

# Configuration
PI_CONFIG_DIR="${PI_CONFIG_DIR:-./pi-config}"
SKILLS_DIR="$PI_CONFIG_DIR/skills"
TRACKING_FILE="$PI_CONFIG_DIR/skills-installed.json"
LOG_FILE="${LOG_FILE:-/var/log/hydra/update-skills.log}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Counters
SUCCESS_COUNT=0
FAILURE_COUNT=0
SKILL_COUNT=0

# Skill list (13 skills)
declare -a SKILLS=(
    "golang-pro|jeffallan/claude-skills"
    "golang-testing|affaan-m/everything-claude-code"
    "golang-patterns|affaan-m/everything-claude-code"
    "angular-component|analogjs/angular-skills"
    "angular-signals|analogjs/angular-skills"
    "angular-testing|analogjs/angular-skills"
    "typescript-advanced-types|wshobson/agents"
    "vitest|antfu/skills"
    "implement-design|figma/mcp-server-guide"
    "systematic-debugging|obra/superpowers"
    "test-driven-development|obra/superpowers"
    "code-review|supercent-io/skills-template"
    "lsp-code-analysis|lsp-client/lsp-skill"
)

# Function to print status messages
print_header() {
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

# Logging function
log_message() {
    local level="$1"
    local message="$2"
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    
    # Create log directory if needed
    mkdir -p "$(dirname "$LOG_FILE")" 2>/dev/null || true
    
    echo "[$timestamp] [$level] $message" | tee -a "$LOG_FILE" 2>/dev/null
}

# Check if pi-config directory exists
check_config_dir() {
    log_message "INFO" "Checking pi-config directory..."
    
    if [[ ! -d "$PI_CONFIG_DIR" ]]; then
        print_error "Pi config directory not found: $PI_CONFIG_DIR"
        log_message "ERROR" "Config directory not found: $PI_CONFIG_DIR"
        echo ""
        echo "Please run setup first or set PI_CONFIG_DIR to the correct path."
        exit 1
    fi
    
    print_success "Pi config directory found: $PI_CONFIG_DIR"
    log_message "INFO" "Config directory found: $PI_CONFIG_DIR"
    
    # Ensure skills directory exists
    if [[ ! -d "$SKILLS_DIR" ]]; then
        print_info "Creating skills directory: $SKILLS_DIR"
        mkdir -p "$SKILLS_DIR"
        log_message "INFO" "Created skills directory: $SKILLS_DIR"
    fi
}

# Check tracking file for last update time
check_tracking_file() {
    log_message "INFO" "Checking tracking file..."
    
    if [[ -f "$TRACKING_FILE" ]]; then
        # Try to extract last_updated timestamp
        if command -v jq &>/dev/null; then
            LAST_UPDATED=$(jq -r '.last_updated // empty' "$TRACKING_FILE" 2>/dev/null)
            SKILLS_COUNT=$(jq -r '.skills_count // 0' "$TRACKING_FILE" 2>/dev/null)
        else
            # Fallback: use grep to extract timestamp (simplified)
            LAST_UPDATED=$(grep -o '"last_updated": *"[^"]*"' "$TRACKING_FILE" | cut -d'"' -f4)
            SKILLS_COUNT=$(grep -o '"skills_count": *[0-9]*' "$TRACKING_FILE" | grep -o '[0-9]*')
        fi
        
        if [[ -n "$LAST_UPDATED" ]]; then
            print_info "Last update: $LAST_UPDATED"
            print_info "Previously installed skills: ${SKILLS_COUNT:-0}"
            log_message "INFO" "Last update: $LAST_UPDATED, Skills: ${SKILLS_COUNT:-0}"
        else
            print_warning "Tracking file exists but no timestamp found"
            log_message "WARNING" "Tracking file missing timestamp"
        fi
    else
        print_info "No tracking file found (fresh installation)"
        log_message "INFO" "No tracking file found"
    fi
}

# Update a single skill
update_skill() {
    local skill_name="$1"
    local skill_repo="$2"
    local skill_num="$3"
    
    SKILL_COUNT=$((SKILL_COUNT + 1))
    
    echo ""
    print_info "[$skill_num/${#SKILLS[@]}] Updating: $skill_name"
    log_message "INFO" "Starting update for skill: $skill_name (repo: $skill_repo)"
    
    # Run npx skills add command
    local cmd="npx skills add $skill_repo/$skill_name"
    log_message "INFO" "Executing: $cmd"
    
    local output
    local exit_code=0
    
    if output=$($cmd 2>&1); then
        print_success "Updated successfully: $skill_name"
        log_message "INFO" "Skill update successful: $skill_name"
        SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
        
        # Log output if verbose
        if [[ "${VERBOSE:-0}" == "1" ]]; then
            echo "$output" | sed 's/^/  /'
        fi
    else
        exit_code=$?
        print_error "Update failed: $skill_name (exit code: $exit_code)"
        log_message "ERROR" "Skill update failed: $skill_name (exit code: $exit_code)"
        log_message "ERROR" "Command output: $output"
        FAILURE_COUNT=$((FAILURE_COUNT + 1))
        
        # Show error output
        echo "  Error output:"
        echo "$output" | sed 's/^/    /' | head -10
    fi
    
    return $exit_code
}

# Update all skills
update_all_skills() {
    print_header "UPDATING PI AGENT SKILLS"
    echo "Total skills to update: ${#SKILLS[@]}"
    echo "Log file: $LOG_FILE"
    echo ""
    
    log_message "INFO" "Starting skill update process for ${#SKILLS[@]} skills"
    
    local skill_num=0
    for skill_entry in "${SKILLS[@]}"; do
        skill_num=$((skill_num + 1))
        IFS='|' read -r skill_name skill_repo <<< "$skill_entry"
        
        update_skill "$skill_name" "$skill_repo" "$skill_num"
    done
}

# Update tracking file with new timestamp
update_tracking_file() {
    print_header "UPDATING TRACKING FILE"
    
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    local total_count=$((SUCCESS_COUNT + FAILURE_COUNT))
    
    log_message "INFO" "Updating tracking file with timestamp: $timestamp"
    
    # Create tracking file JSON structure
    local tracking_data
    tracking_data=$(cat <<EOF
{
  "last_updated": "$timestamp",
  "skills_count": $SUCCESS_COUNT,
  "update_type": "manual",
  "results": {
    "success": $SUCCESS_COUNT,
    "failed": $FAILURE_COUNT,
    "total": $total_count
  }
}
EOF
)
    
    # Write tracking file
    echo "$tracking_data" > "$TRACKING_FILE"
    
    if [[ $? -eq 0 ]]; then
        print_success "Tracking file updated: $TRACKING_FILE"
        print_info "Timestamp: $timestamp"
        log_message "INFO" "Tracking file updated successfully"
    else
        print_error "Failed to update tracking file"
        log_message "ERROR" "Failed to write tracking file: $TRACKING_FILE"
    fi
}

# Show summary
show_summary() {
    print_header "UPDATE SUMMARY"
    
    local total_count=$((SUCCESS_COUNT + FAILURE_COUNT))
    
    echo ""
    echo "Total skills processed: $total_count"
    echo -e "Successful updates:    ${GREEN}$SUCCESS_COUNT${NC}"
    echo -e "Failed updates:        ${RED}$FAILURE_COUNT${NC}"
    echo ""
    
    # Calculate success rate
    if [[ $total_count -gt 0 ]]; then
        local success_rate=$((SUCCESS_COUNT * 100 / total_count))
        echo "Success rate: $success_rate%"
        echo ""
        
        if [[ $success_rate -eq 100 ]]; then
            print_success "All skills updated successfully!"
            log_message "INFO" "All skills updated successfully (100%)"
        elif [[ $success_rate -ge 75 ]]; then
            print_warning "Most skills updated successfully ($success_rate%)"
            log_message "WARNING" "Partial success: $success_rate% of skills updated"
        else
            print_error "Many skill updates failed ($success_rate%)"
            log_message "ERROR" "Low success rate: $success_rate% of skills updated"
        fi
    fi
    
    echo ""
    echo "Tracking file: $TRACKING_FILE"
    echo "Log file: $LOG_FILE"
    echo ""
    
    # Exit with error code if any failures
    if [[ $FAILURE_COUNT -gt 0 ]]; then
        exit 1
    fi
}

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --force)
                FORCE_UPDATE=1
                shift
                ;;
            --verbose|-v)
                VERBOSE=1
                shift
                ;;
            --help|-h)
                show_help
                exit 0
                ;;
            *)
                print_error "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done
}

# Show help
show_help() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Update Pi Agent skills for Hydra"
    echo ""
    echo "Options:"
    echo "  --force          Force update even if recently updated"
    echo "  --verbose, -v    Show detailed output"
    echo "  --help, -h       Show this help message"
    echo ""
    echo "Environment Variables:"
    echo "  PI_CONFIG_DIR    Path to pi-config directory (default: ./pi-config)"
    echo "  LOG_FILE         Path to log file (default: /var/log/hydra/update-skills.log)"
    echo ""
    echo "Examples:"
    echo "  $0                    # Standard update"
    echo "  $0 --force            # Force update"
    echo "  $0 --verbose          # Verbose output"
    echo "  PI_CONFIG_DIR=/custom $0  # Use custom config directory"
}

# Main function
main() {
    parse_args "$@"
    
    # Initialize logging
    log_message "INFO" "==========================================="
    log_message "INFO" "Pi Agent Skills Update Script Started"
    log_message "INFO" "==========================================="
    log_message "INFO" "Config directory: $PI_CONFIG_DIR"
    log_message "INFO" "Tracking file: $TRACKING_FILE"
    log_message "INFO" "Total skills to update: ${#SKILLS[@]}"
    
    # Run checks
    check_config_dir
    check_tracking_file
    
    # Check if skills CLI is available
    if ! command -v npx &>/dev/null; then
        print_error "npx command not found. Please install Node.js and npm."
        log_message "ERROR" "npx command not found"
        exit 1
    fi
    
    # Run updates
    update_all_skills
    
    # Update tracking file
    update_tracking_file
    
    # Show summary
    show_summary
}

# Run main function
main "$@"
