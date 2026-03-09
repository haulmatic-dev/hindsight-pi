package security

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestTraumaGuard_BlockedOperationReturnsError tests that blocked operations return an error with explanation
func TestTraumaGuard_BlockedOperationReturnsError(t *testing.T) {
	// Create trauma guard with default patterns
	guard := NewTraumaGuard()
	if guard == nil {
		t.Fatal("NewTraumaGuard() returned nil")
	}

	// Enable the guard
	if err := guard.Enable(); err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}

	// Add a dangerous pattern
	pattern := TraumaPattern{
		ID:          "rm-rf-root",
		Pattern:     "rm -rf /",
		Description: "Recursive delete of root directory",
		Severity:    SeverityCritical,
		Category:    "filesystem",
	}
	if err := guard.AddPattern(pattern); err != nil {
		t.Fatalf("AddPattern() failed: %v", err)
	}

	// Try to execute a dangerous operation
	ctx := context.Background()
	operation := "rm -rf /"
	block, err := guard.CheckOperation(ctx, operation)

	// Should return an error
	if err == nil {
		t.Fatal("CheckOperation() should have returned an error for dangerous operation")
	}

	// Error should be a TraumaGuardError with explanation
	traumaErr, ok := err.(TraumaGuardError)
	if !ok {
		t.Fatalf("Expected TraumaGuardError, got %T", err)
	}

	// Verify the explanation is present and meaningful
	if traumaErr.Explanation == "" {
		t.Error("TraumaGuardError.Explanation is empty")
	}
	if !strings.Contains(traumaErr.Error(), pattern.Description) {
		t.Errorf("Error message should contain pattern description, got: %s", traumaErr.Error())
	}

	// Verify block was returned with details
	if block == nil {
		t.Fatal("CheckOperation() returned nil block")
	}
	if block.PatternID != pattern.ID {
		t.Errorf("Expected PatternID %s, got %s", pattern.ID, block.PatternID)
	}
	if block.Operation != operation {
		t.Errorf("Expected Operation %s, got %s", operation, block.Operation)
	}
	if block.Error == nil {
		t.Error("Block.Error should not be nil")
	}
}

// TestTraumaGuard_AuditLogRecordsBlocks tests that all trauma blocks are recorded in audit log
func TestTraumaGuard_AuditLogRecordsBlocks(t *testing.T) {
	guard := NewTraumaGuard()
	if guard == nil {
		t.Fatal("NewTraumaGuard() returned nil")
	}

	if err := guard.Enable(); err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}

	// Add multiple patterns
	patterns := []TraumaPattern{
		{
			ID:          "drop-database",
			Pattern:     "DROP DATABASE",
			Description: "Database deletion",
			Severity:    SeverityCritical,
			Category:    "database",
		},
		{
			ID:          "force-push-main",
			Pattern:     "git push --force main",
			Description: "Force push to main branch",
			Severity:    SeverityHigh,
			Category:    "git",
		},
	}

	for _, p := range patterns {
		if err := guard.AddPattern(p); err != nil {
			t.Fatalf("AddPattern() failed: %v", err)
		}
	}

	// Execute operations that should be blocked
	operations := []string{
		"DROP DATABASE production",
		"git push --force main",
	}

	ctx := context.Background()
	for _, op := range operations {
		_, _ = guard.CheckOperation(ctx, op) // Ignore errors, we're testing audit log
	}

	// Check audit log
	auditLog := guard.GetAuditLog()
	if len(auditLog) != len(operations) {
		t.Errorf("Expected %d audit log entries, got %d", len(operations), len(auditLog))
	}

	// Verify each block is recorded with timestamp
	for i, block := range auditLog {
		if block.ID == "" {
			t.Errorf("Audit log entry %d has empty ID", i)
		}
		if block.Timestamp.IsZero() {
			t.Errorf("Audit log entry %d has zero timestamp", i)
		}
		if block.PatternID == "" {
			t.Errorf("Audit log entry %d has empty PatternID", i)
		}
		if block.Operation == "" {
			t.Errorf("Audit log entry %d has empty Operation", i)
		}
	}
}

// TestTraumaGuard_AuditLogPatternFiltering tests filtering audit log by pattern
func TestTraumaGuard_AuditLogPatternFiltering(t *testing.T) {
	guard := NewTraumaGuard()
	if guard == nil {
		t.Fatal("NewTraumaGuard() returned nil")
	}

	if err := guard.Enable(); err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}

	// Add patterns
	patterns := []TraumaPattern{
		{ID: "pattern-1", Pattern: "test1", Description: "Test 1", Severity: SeverityHigh, Category: "test"},
		{ID: "pattern-2", Pattern: "test2", Description: "Test 2", Severity: SeverityHigh, Category: "test"},
	}

	for _, p := range patterns {
		if err := guard.AddPattern(p); err != nil {
			t.Fatalf("AddPattern() failed: %v", err)
		}
	}

	// Block operations
	ctx := context.Background()
	_, _ = guard.CheckOperation(ctx, "test1 command")
	_, _ = guard.CheckOperation(ctx, "test1 another")
	_, _ = guard.CheckOperation(ctx, "test2 command")

	// Filter by pattern
	pattern1Blocks := guard.GetAuditLogForPattern("pattern-1")
	if len(pattern1Blocks) != 2 {
		t.Errorf("Expected 2 blocks for pattern-1, got %d", len(pattern1Blocks))
	}

	pattern2Blocks := guard.GetAuditLogForPattern("pattern-2")
	if len(pattern2Blocks) != 1 {
		t.Errorf("Expected 1 block for pattern-2, got %d", len(pattern2Blocks))
	}

	// Check non-existent pattern
	nonExistentBlocks := guard.GetAuditLogForPattern("non-existent")
	if len(nonExistentBlocks) != 0 {
		t.Errorf("Expected 0 blocks for non-existent pattern, got %d", len(nonExistentBlocks))
	}
}

// TestTraumaGuard_TemporaryBypass tests temporary bypass functionality with reason
func TestTraumaGuard_TemporaryBypass(t *testing.T) {
	guard := NewTraumaGuard()
	if guard == nil {
		t.Fatal("NewTraumaGuard() returned nil")
	}

	if err := guard.Enable(); err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}

	pattern := TraumaPattern{
		ID:          "critical-op",
		Pattern:     "critical-operation",
		Description: "Critical operation",
		Severity:    SeverityCritical,
		Category:    "test",
	}
	if err := guard.AddPattern(pattern); err != nil {
		t.Fatalf("AddPattern() failed: %v", err)
	}

	// Request bypass with reason
	reason := "Emergency maintenance required"
	duration := 30 * time.Minute
	token, err := guard.RequestBypass(pattern.ID, reason, duration)
	if err != nil {
		t.Fatalf("RequestBypass() failed: %v", err)
	}

	// Verify token structure
	if token == nil {
		t.Fatal("RequestBypass() returned nil token")
	}
	if token.Token == "" {
		t.Error("Bypass token is empty")
	}
	if token.PatternID != pattern.ID {
		t.Errorf("Expected PatternID %s, got %s", pattern.ID, token.PatternID)
	}
	if token.Reason != reason {
		t.Errorf("Expected Reason %s, got %s", reason, token.Reason)
	}
	expectedExpiry := time.Now().Add(duration)
	if token.ExpiresAt.Before(expectedExpiry.Add(-5*time.Second)) || token.ExpiresAt.After(expectedExpiry.Add(5*time.Second)) {
		t.Errorf("Expected expiry around %v, got %v", expectedExpiry, token.ExpiresAt)
	}

	// Verify token is valid
	if !guard.IsBypassValid(token.Token) {
		t.Error("IsBypassValid() returned false for valid token")
	}

	// Validate and consume the token
	validatedToken, err := guard.ValidateBypassToken(token.Token)
	if err != nil {
		t.Fatalf("ValidateBypassToken() failed: %v", err)
	}
	if validatedToken == nil {
		t.Fatal("ValidateBypassToken() returned nil")
	}
	if validatedToken.Token != token.Token {
		t.Error("Validated token doesn't match original")
	}
}

// TestTraumaGuard_BypassTokenExpiration tests that bypass tokens expire correctly
func TestTraumaGuard_BypassTokenExpiration(t *testing.T) {
	guard := NewTraumaGuard()
	if guard == nil {
		t.Fatal("NewTraumaGuard() returned nil")
	}

	if err := guard.Enable(); err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}

	pattern := TraumaPattern{
		ID:          "test-pattern",
		Pattern:     "test",
		Description: "Test pattern",
		Severity:    SeverityLow,
		Category:    "test",
	}
	if err := guard.AddPattern(pattern); err != nil {
		t.Fatalf("AddPattern() failed: %v", err)
	}

	// Create short-lived token
	token, err := guard.RequestBypass(pattern.ID, "test", 1*time.Millisecond)
	if err != nil {
		t.Fatalf("RequestBypass() failed: %v", err)
	}

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	// Token should be expired
	if guard.IsBypassValid(token.Token) {
		t.Error("IsBypassValid() returned true for expired token")
	}

	// Validation should fail
	_, err = guard.ValidateBypassToken(token.Token)
	if err == nil {
		t.Error("ValidateBypassToken() should fail for expired token")
	}
}

// TestTraumaGuard_BypassTokenRevocation tests revoking bypass tokens
func TestTraumaGuard_BypassTokenRevocation(t *testing.T) {
	guard := NewTraumaGuard()
	if guard == nil {
		t.Fatal("NewTraumaGuard() returned nil")
	}

	if err := guard.Enable(); err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}

	pattern := TraumaPattern{
		ID:          "test-pattern",
		Pattern:     "test",
		Description: "Test pattern",
		Severity:    SeverityLow,
		Category:    "test",
	}
	if err := guard.AddPattern(pattern); err != nil {
		t.Fatalf("AddPattern() failed: %v", err)
	}

	// Create token
	token, err := guard.RequestBypass(pattern.ID, "test", 1*time.Hour)
	if err != nil {
		t.Fatalf("RequestBypass() failed: %v", err)
	}

	// Verify it's valid
	if !guard.IsBypassValid(token.Token) {
		t.Fatal("Token should be valid before revocation")
	}

	// Revoke it
	if err := guard.RevokeBypassToken(token.Token); err != nil {
		t.Fatalf("RevokeBypassToken() failed: %v", err)
	}

	// Should no longer be valid
	if guard.IsBypassValid(token.Token) {
		t.Error("IsBypassValid() returned true for revoked token")
	}

	// Validation should fail
	_, err = guard.ValidateBypassToken(token.Token)
	if err == nil {
		t.Error("ValidateBypassToken() should fail for revoked token")
	}
}

// TestTraumaGuard_BypassWithAuditLog tests that bypassed operations are logged
func TestTraumaGuard_BypassWithAuditLog(t *testing.T) {
	guard := NewTraumaGuard()
	if guard == nil {
		t.Fatal("NewTraumaGuard() returned nil")
	}

	if err := guard.Enable(); err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}

	pattern := TraumaPattern{
		ID:          "blocked-op",
		Pattern:     "blocked",
		Description: "Blocked operation",
		Severity:    SeverityHigh,
		Category:    "test",
	}
	if err := guard.AddPattern(pattern); err != nil {
		t.Fatalf("AddPattern() failed: %v", err)
	}

	// Get bypass token
	reason := "Authorized emergency fix"
	token, err := guard.RequestBypass(pattern.ID, reason, 30*time.Minute)
	if err != nil {
		t.Fatalf("RequestBypass() failed: %v", err)
	}

	// Execute operation with bypass (this should be logged as bypassed)
	ctx := context.Background()
	// Note: In real implementation, bypass token would be passed in context or header
	ctx = context.WithValue(ctx, "bypass_token", token.Token)

	block, err := guard.CheckOperation(ctx, "blocked command")
	if err != nil {
		t.Fatalf("CheckOperation() should not error with valid bypass: %v", err)
	}

	// Block should indicate it was bypassed
	if block != nil {
		if !block.Bypassed {
			t.Error("Block should indicate it was bypassed")
		}
		if block.BypassReason != reason {
			t.Errorf("Expected BypassReason %s, got %s", reason, block.BypassReason)
		}
	}

	// Audit log should show this was a bypassed operation
	auditLog := guard.GetAuditLog()
	if len(auditLog) == 0 {
		t.Fatal("Expected audit log entry for bypassed operation")
	}

	lastEntry := auditLog[len(auditLog)-1]
	if !lastEntry.Bypassed {
		t.Error("Last audit entry should show Bypassed=true")
	}
	if lastEntry.BypassReason != reason {
		t.Errorf("Expected BypassReason %s, got %s", reason, lastEntry.BypassReason)
	}
}

// TestTraumaGuard_DefaultPatterns tests that default patterns are provided
func TestTraumaGuard_DefaultPatterns(t *testing.T) {
	patterns := DefaultPatterns()
	if patterns == nil {
		t.Fatal("DefaultPatterns() returned nil")
	}
	if len(patterns) == 0 {
		t.Fatal("DefaultPatterns() returned empty slice")
	}

	// Check for expected default patterns
	expectedPatterns := map[string]bool{
		"rm -rf /":              false,
		"DROP DATABASE":         false,
		"git push --force main": false,
	}

	for _, p := range patterns {
		if _, exists := expectedPatterns[p.Pattern]; exists {
			expectedPatterns[p.Pattern] = true
		}
		// Verify pattern has required fields
		if p.ID == "" {
			t.Error("Default pattern has empty ID")
		}
		if p.Description == "" {
			t.Errorf("Default pattern %s has empty Description", p.Pattern)
		}
	}

	for pattern, found := range expectedPatterns {
		if !found {
			t.Errorf("Expected default pattern not found: %s", pattern)
		}
	}
}

// TestTraumaGuard_EnableDisable tests enabling and disabling the guard
func TestTraumaGuard_EnableDisable(t *testing.T) {
	guard := NewTraumaGuard()
	if guard == nil {
		t.Fatal("NewTraumaGuard() returned nil")
	}

	// Should start disabled
	if guard.IsEnabled() {
		t.Error("Guard should start disabled")
	}

	// Enable
	if err := guard.Enable(); err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}
	if !guard.IsEnabled() {
		t.Error("Guard should be enabled after Enable()")
	}

	// Disable
	if err := guard.Disable(); err != nil {
		t.Fatalf("Disable() failed: %v", err)
	}
	if guard.IsEnabled() {
		t.Error("Guard should be disabled after Disable()")
	}
}

// TestTraumaGuard_RemovePattern tests pattern removal
func TestTraumaGuard_RemovePattern(t *testing.T) {
	guard := NewTraumaGuard()
	if guard == nil {
		t.Fatal("NewTraumaGuard() returned nil")
	}

	if err := guard.Enable(); err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}

	pattern := TraumaPattern{
		ID:          "removable",
		Pattern:     "test-remove",
		Description: "Removable pattern",
		Severity:    SeverityLow,
		Category:    "test",
	}
	if err := guard.AddPattern(pattern); err != nil {
		t.Fatalf("AddPattern() failed: %v", err)
	}

	// Verify pattern exists
	patterns := guard.GetPatterns()
	if len(patterns) != 1 {
		t.Fatalf("Expected 1 pattern, got %d", len(patterns))
	}

	// Remove pattern
	if err := guard.RemovePattern(pattern.ID); err != nil {
		t.Fatalf("RemovePattern() failed: %v", err)
	}

	// Verify pattern is removed
	patterns = guard.GetPatterns()
	if len(patterns) != 0 {
		t.Errorf("Expected 0 patterns after removal, got %d", len(patterns))
	}

	// Removing non-existent pattern should error
	if err := guard.RemovePattern("non-existent"); err == nil {
		t.Error("RemovePattern() should error for non-existent pattern")
	}
}

// TestTraumaGuard_ClearAuditLog tests clearing the audit log
func TestTraumaGuard_ClearAuditLog(t *testing.T) {
	guard := NewTraumaGuard()
	if guard == nil {
		t.Fatal("NewTraumaGuard() returned nil")
	}

	if err := guard.Enable(); err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}

	pattern := TraumaPattern{
		ID:          "test",
		Pattern:     "test",
		Description: "Test",
		Severity:    SeverityLow,
		Category:    "test",
	}
	if err := guard.AddPattern(pattern); err != nil {
		t.Fatalf("AddPattern() failed: %v", err)
	}

	// Create some audit entries
	ctx := context.Background()
	_, _ = guard.CheckOperation(ctx, "test")
	_, _ = guard.CheckOperation(ctx, "test")

	// Verify entries exist
	if len(guard.GetAuditLog()) != 2 {
		t.Fatal("Expected 2 audit log entries")
	}

	// Clear audit log
	if err := guard.ClearAuditLog(); err != nil {
		t.Fatalf("ClearAuditLog() failed: %v", err)
	}

	// Verify log is empty
	if len(guard.GetAuditLog()) != 0 {
		t.Errorf("Expected 0 audit log entries after clear, got %d", len(guard.GetAuditLog()))
	}
}

// TestTraumaGuard_ExportAuditLog tests exporting audit log
func TestTraumaGuard_ExportAuditLog(t *testing.T) {
	guard := NewTraumaGuard()
	if guard == nil {
		t.Fatal("NewTraumaGuard() returned nil")
	}

	if err := guard.Enable(); err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}

	pattern := TraumaPattern{
		ID:          "test",
		Pattern:     "test",
		Description: "Test",
		Severity:    SeverityLow,
		Category:    "test",
	}
	if err := guard.AddPattern(pattern); err != nil {
		t.Fatalf("AddPattern() failed: %v", err)
	}

	// Create audit entry
	ctx := context.Background()
	_, _ = guard.CheckOperation(ctx, "test")

	// Export
	export, err := guard.ExportAuditLog()
	if err != nil {
		t.Fatalf("ExportAuditLog() failed: %v", err)
	}
	if export == "" {
		t.Error("ExportAuditLog() returned empty string")
	}

	// Should contain pattern info
	if !strings.Contains(export, pattern.ID) {
		t.Error("Export should contain pattern ID")
	}
}

// TestTraumaGuard_DisabledGuardDoesNotBlock tests that disabled guard doesn't block
func TestTraumaGuard_DisabledGuardDoesNotBlock(t *testing.T) {
	guard := NewTraumaGuard()
	if guard == nil {
		t.Fatal("NewTraumaGuard() returned nil")
	}

	// Don't enable - guard should be disabled by default

	pattern := TraumaPattern{
		ID:          "dangerous",
		Pattern:     "rm -rf /",
		Description: "Dangerous",
		Severity:    SeverityCritical,
		Category:    "test",
	}
	if err := guard.AddPattern(pattern); err != nil {
		t.Fatalf("AddPattern() failed: %v", err)
	}

	// Check operation - should not block when disabled
	ctx := context.Background()
	block, err := guard.CheckOperation(ctx, "rm -rf /")
	if err != nil {
		t.Errorf("CheckOperation() should not error when guard is disabled: %v", err)
	}
	if block != nil {
		t.Error("CheckOperation() should return nil block when guard is disabled")
	}
}

// TestTraumaGuard_InvalidBypassRequests tests invalid bypass request handling
func TestTraumaGuard_InvalidBypassRequests(t *testing.T) {
	guard := NewTraumaGuard()
	if guard == nil {
		t.Fatal("NewTraumaGuard() returned nil")
	}

	if err := guard.Enable(); err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}

	// Request bypass for non-existent pattern
	_, err := guard.RequestBypass("non-existent", "reason", 30*time.Minute)
	if err == nil {
		t.Error("RequestBypass() should error for non-existent pattern")
	}

	// Request bypass with empty reason
	pattern := TraumaPattern{
		ID:          "test",
		Pattern:     "test",
		Description: "Test",
		Severity:    SeverityLow,
		Category:    "test",
	}
	if err := guard.AddPattern(pattern); err != nil {
		t.Fatalf("AddPattern() failed: %v", err)
	}

	_, err = guard.RequestBypass(pattern.ID, "", 30*time.Minute)
	if err == nil {
		t.Error("RequestBypass() should error for empty reason")
	}

	// Request bypass with zero duration
	_, err = guard.RequestBypass(pattern.ID, "reason", 0)
	if err == nil {
		t.Error("RequestBypass() should error for zero duration")
	}
}

// TestTraumaGuard_AddDuplicatePattern tests handling duplicate patterns
func TestTraumaGuard_AddDuplicatePattern(t *testing.T) {
	guard := NewTraumaGuard()
	if guard == nil {
		t.Fatal("NewTraumaGuard() returned nil")
	}

	if err := guard.Enable(); err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}

	pattern := TraumaPattern{
		ID:          "duplicate",
		Pattern:     "duplicate-pattern",
		Description: "Duplicate",
		Severity:    SeverityLow,
		Category:    "test",
	}

	// Add first time
	if err := guard.AddPattern(pattern); err != nil {
		t.Fatalf("First AddPattern() failed: %v", err)
	}

	// Add second time - should error or update
	err := guard.AddPattern(pattern)
	// Implementation choice: either error or silently update
	// We just verify the state is consistent
	patterns := guard.GetPatterns()
	if len(patterns) != 1 {
		t.Errorf("Expected 1 pattern, got %d", len(patterns))
	}

	// Suppress unused variable error
	_ = err
}
