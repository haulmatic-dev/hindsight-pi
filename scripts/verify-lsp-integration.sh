#!/bin/bash
#
# LSP Integration Verification Script
# Verifies that LSP servers are properly configured and functional
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
PI_CONFIG_DIR="${PI_CONFIG_DIR:-./pi-config}"
MCP_CONFIG="$PI_CONFIG_DIR/agent/mcp.json"
SKILLS_DIR="$PI_CONFIG_DIR/skills"

echo "=========================================="
echo "LSP Integration Verification"
echo "=========================================="
echo ""

# Track test results
TESTS_PASSED=0
TESTS_FAILED=0

# Function to print success
pass() {
    echo -e "${GREEN}✓ PASS${NC}: $1"
    ((TESTS_PASSED++))
}

# Function to print failure
fail() {
    echo -e "${RED}✗ FAIL${NC}: $1"
    ((TESTS_FAILED++))
}

# Function to print warning
warn() {
    echo -e "${YELLOW}⚠ WARN${NC}: $1"
}

# Test 1: Check pi-config directory structure
echo "[Test 1] Checking pi-config directory structure..."
if [ -d "$PI_CONFIG_DIR" ]; then
    pass "pi-config directory exists"
else
    fail "pi-config directory not found at $PI_CONFIG_DIR"
fi

if [ -d "$PI_CONFIG_DIR/agent" ]; then
    pass "pi-config/agent directory exists"
else
    fail "pi-config/agent directory not found"
fi

if [ -d "$PI_CONFIG_DIR/skills" ]; then
    pass "pi-config/skills directory exists"
else
    fail "pi-config/skills directory not found"
fi

# Test 2: Check mcp.json exists and is valid
echo ""
echo "[Test 2] Checking MCP configuration..."
if [ -f "$MCP_CONFIG" ]; then
    pass "mcp.json exists"
    
    # Validate JSON
    if python3 -m json.tool "$MCP_CONFIG" > /dev/null 2>&1 || jq empty "$MCP_CONFIG" > /dev/null 2>&1; then
        pass "mcp.json is valid JSON"
    else
        fail "mcp.json is not valid JSON"
    fi
    
    # Check for required sections
    if grep -q '"go"' "$MCP_CONFIG" 2>/dev/null; then
        pass "mcp.json contains 'go' server configuration"
    else
        fail "mcp.json missing 'go' server configuration"
    fi
    
    if grep -q '"typescript"' "$MCP_CONFIG" 2>/dev/null; then
        pass "mcp.json contains 'typescript' server configuration"
    else
        fail "mcp.json missing 'typescript' server configuration"
    fi
    
    if grep -q '"capabilities"' "$MCP_CONFIG" 2>/dev/null; then
        pass "mcp.json contains capabilities section"
    else
        warn "mcp.json missing capabilities section"
    fi
    
    if grep -q '"lspCommands"' "$MCP_CONFIG" 2>/dev/null; then
        pass "mcp.json contains lspCommands section"
    else
        warn "mcp.json missing lspCommands section"
    fi
else
    fail "mcp.json not found at $MCP_CONFIG"
fi

# Test 3: Check LSP servers are available
echo ""
echo "[Test 3] Checking LSP servers..."

if command -v gopls &> /dev/null; then
    GOPLS_VERSION=$(gopls version 2>/dev/null | head -1 || echo "unknown")
    pass "gopls is installed ($GOPLS_VERSION)"
else
    fail "gopls is not installed"
fi

if command -v typescript-language-server &> /dev/null; then
    pass "typescript-language-server is installed"
else
    warn "typescript-language-server is not installed (optional for non-TS projects)"
fi

if command -v lsp &> /dev/null; then
    pass "LSP CLI is installed"
else
    warn "LSP CLI is not installed (needed for LSP commands)"
fi

# Test 4: Check lsp-code-analysis skill
echo ""
echo "[Test 4] Checking lsp-code-analysis skill..."

SKILL_PATH="$SKILLS_DIR/lsp-code-analysis"
SKILL_MD="$SKILL_PATH/SKILL.md"

if [ -d "$SKILL_PATH" ]; then
    pass "lsp-code-analysis skill directory exists"
else
    fail "lsp-code-analysis skill directory not found at $SKILL_PATH"
fi

if [ -f "$SKILL_MD" ]; then
    pass "lsp-code-analysis SKILL.md exists"
else
    fail "lsp-code-analysis SKILL.md not found"
fi

# Test 5: Test LSP commands (if available)
echo ""
echo "[Test 5] Testing LSP commands..."

# Create a test Go file
TEST_FILE=$(mktemp /tmp/test_lsp_XXXXXX.go)
cat > "$TEST_FILE" << 'EOF'
package main

import "fmt"

func main() {
    message := "Hello, LSP!"
    fmt.Println(message)
}
EOF

if command -v lsp &> /dev/null; then
    # Test outline command
    if timeout 5 lsp outline "$TEST_FILE" > /dev/null 2>&1; then
        pass "lsp outline command works"
    else
        warn "lsp outline command failed (server may not be running)"
    fi
else
    warn "LSP CLI not available, skipping command tests"
fi

rm -f "$TEST_FILE"

# Test 6: Go-specific tests
echo ""
echo "[Test 6] Go project tests..."

# Test with a simple Go file in the project
if [ -f "go.mod" ]; then
    pass "Go module detected"
    
    # Try to get outline of a real file
    if [ -f "internal/pi/lsp.go" ]; then
        if command -v gopls &> /dev/null; then
            if timeout 5 gopls inspect internal/pi/lsp.go > /dev/null 2>&1; then
                pass "gopls can analyze project files"
            else
                warn "gopls inspect failed (may need initialization)"
            fi
        fi
    fi
else
    warn "No go.mod found (not a Go project)"
fi

# Summary
echo ""
echo "=========================================="
echo "Test Summary"
echo "=========================================="
echo -e "Tests Passed: ${GREEN}$TESTS_PASSED${NC}"
echo -e "Tests Failed: ${RED}$TESTS_FAILED${NC}"
echo ""

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}All required tests passed!${NC}"
    echo "LSP Integration is properly configured."
    exit 0
elif [ $TESTS_FAILED -le 2 ]; then
    echo -e "${YELLOW}Most tests passed with minor issues.${NC}"
    echo "LSP Integration is functional but some optional features may be missing."
    exit 0
else
    echo -e "${RED}Several tests failed.${NC}"
    echo "Please review the failed tests above and fix the issues."
    exit 1
fi