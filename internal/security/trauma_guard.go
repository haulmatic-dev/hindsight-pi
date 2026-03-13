package security

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TraumaPattern represents a dangerous operation pattern that should be blocked
type TraumaPattern struct {
	ID          string
	Pattern     string
	Description string
	Severity    TraumaSeverity
	Category    string
}

// TraumaSeverity indicates how dangerous an operation is
type TraumaSeverity int

const (
	SeverityLow TraumaSeverity = iota
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

// TraumaBlock represents a blocked trauma operation
type TraumaBlock struct {
	ID           string
	PatternID    string
	Operation    string
	Timestamp    time.Time
	Bypassed     bool
	BypassReason string
	Error        error
}

// TraumaGuard provides protection against dangerous operations
type TraumaGuard struct {
	patterns     []TraumaPattern
	auditLog     []TraumaBlock
	enabled      bool
	bypassTokens map[string]BypassToken
}

// BypassToken represents a temporary bypass authorization
type BypassToken struct {
	Token     string
	PatternID string
	Reason    string
	ExpiresAt time.Time
}

// TraumaGuardError is returned when a trauma is blocked
type TraumaGuardError struct {
	Pattern     TraumaPattern
	Operation   string
	Explanation string
}

func (e TraumaGuardError) Error() string {
	return fmt.Sprintf("trauma guard blocked: %s - %s", e.Pattern.Description, e.Explanation)
}

// NewTraumaGuard creates a new trauma guard instance
func NewTraumaGuard() *TraumaGuard {
	return &TraumaGuard{
		patterns:     make([]TraumaPattern, 0),
		auditLog:     make([]TraumaBlock, 0),
		enabled:      false,
		bypassTokens: make(map[string]BypassToken),
	}
}

// Enable activates the trauma guard
func (tg *TraumaGuard) Enable() error {
	tg.enabled = true
	return nil
}

// Disable deactivates the trauma guard
func (tg *TraumaGuard) Disable() error {
	tg.enabled = false
	return nil
}

// IsEnabled returns whether the guard is enabled
func (tg *TraumaGuard) IsEnabled() bool {
	return tg.enabled
}

// AddPattern adds a new trauma pattern to the guard
func (tg *TraumaGuard) AddPattern(pattern TraumaPattern) error {
	for _, p := range tg.patterns {
		if p.ID == pattern.ID {
			return fmt.Errorf("pattern with ID %s already exists", pattern.ID)
		}
	}
	tg.patterns = append(tg.patterns, pattern)
	return nil
}

// RemovePattern removes a trauma pattern by ID
func (tg *TraumaGuard) RemovePattern(patternID string) error {
	for i, p := range tg.patterns {
		if p.ID == patternID {
			tg.patterns = append(tg.patterns[:i], tg.patterns[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("pattern with ID %s not found", patternID)
}

// GetPatterns returns all registered trauma patterns
func (tg *TraumaGuard) GetPatterns() []TraumaPattern {
	return tg.patterns
}

// CheckOperation checks if an operation matches any trauma pattern
func (tg *TraumaGuard) CheckOperation(ctx context.Context, operation string) (*TraumaBlock, error) {
	if !tg.enabled {
		return nil, nil
	}

	for _, pattern := range tg.patterns {
		if strings.Contains(operation, pattern.Pattern) {
			// Check if bypass token is provided in context
			if bypassTokenStr, ok := ctx.Value("bypass_token").(string); ok {
				if bypassToken, exists := tg.bypassTokens[bypassTokenStr]; exists {
					if time.Now().Before(bypassToken.ExpiresAt) && bypassToken.PatternID == pattern.ID {
						// Valid bypass token - log as bypassed but don't block
						block := &TraumaBlock{
							ID:           pattern.ID + "-" + time.Now().Format("20060102150405"),
							PatternID:    pattern.ID,
							Operation:    operation,
							Timestamp:    time.Now(),
							Bypassed:     true,
							BypassReason: bypassToken.Reason,
						}
						tg.auditLog = append(tg.auditLog, *block)
						return block, nil
					}
				}
			}

			// No valid bypass - block the operation
			block := &TraumaBlock{
				ID:        pattern.ID + "-" + time.Now().Format("20060102150405"),
				PatternID: pattern.ID,
				Operation: operation,
				Timestamp: time.Now(),
				Bypassed:  false,
				Error: TraumaGuardError{
					Pattern:     pattern,
					Operation:   operation,
					Explanation: pattern.Description,
				},
			}
			tg.auditLog = append(tg.auditLog, *block)
			return block, block.Error
		}
	}

	return nil, nil
}

// GetAuditLog returns the audit log of all blocked operations
func (tg *TraumaGuard) GetAuditLog() []TraumaBlock {
	return tg.auditLog
}

// GetAuditLogForPattern returns audit log entries for a specific pattern
func (tg *TraumaGuard) GetAuditLogForPattern(patternID string) []TraumaBlock {
	var result []TraumaBlock
	for _, block := range tg.auditLog {
		if block.PatternID == patternID {
			result = append(result, block)
		}
	}
	return result
}

// RequestBypass requests a temporary bypass token for a pattern
func (tg *TraumaGuard) RequestBypass(patternID string, reason string, duration time.Duration) (*BypassToken, error) {
	// Validate inputs
	if reason == "" {
		return nil, fmt.Errorf("reason cannot be empty")
	}
	if duration <= 0 {
		return nil, fmt.Errorf("duration must be greater than zero")
	}

	// Verify pattern exists
	patternExists := false
	for _, p := range tg.patterns {
		if p.ID == patternID {
			patternExists = true
			break
		}
	}
	if !patternExists {
		return nil, fmt.Errorf("pattern with ID %s not found", patternID)
	}

	// Generate a unique token
	tokenStr := fmt.Sprintf("bypass_%s_%d", patternID, time.Now().UnixNano())

	token := &BypassToken{
		Token:     tokenStr,
		PatternID: patternID,
		Reason:    reason,
		ExpiresAt: time.Now().Add(duration),
	}

	tg.bypassTokens[tokenStr] = *token
	return token, nil
}

// ValidateBypassToken validates and consumes a bypass token
func (tg *TraumaGuard) ValidateBypassToken(token string) (*BypassToken, error) {
	bypassToken, exists := tg.bypassTokens[token]
	if !exists {
		return nil, fmt.Errorf("token not found")
	}

	if time.Now().After(bypassToken.ExpiresAt) {
		return nil, fmt.Errorf("token has expired")
	}

	return &bypassToken, nil
}

// IsBypassValid checks if a bypass token is still valid
func (tg *TraumaGuard) IsBypassValid(token string) bool {
	bypassToken, exists := tg.bypassTokens[token]
	if !exists {
		return false
	}

	return time.Now().Before(bypassToken.ExpiresAt)
}

// RevokeBypassToken revokes a bypass token before it expires
func (tg *TraumaGuard) RevokeBypassToken(token string) error {
	if _, exists := tg.bypassTokens[token]; !exists {
		return fmt.Errorf("token not found")
	}

	delete(tg.bypassTokens, token)
	return nil
}

// ClearAuditLog clears the audit log
func (tg *TraumaGuard) ClearAuditLog() error {
	tg.auditLog = make([]TraumaBlock, 0)
	return nil
}

// ExportAuditLog exports the audit log in a structured format
func (tg *TraumaGuard) ExportAuditLog() (string, error) {
	jsonData, err := json.Marshal(tg.auditLog)
	if err != nil {
		return "", fmt.Errorf("failed to marshal audit log: %w", err)
	}
	return string(jsonData), nil
}

// DefaultPatterns returns the default set of trauma patterns
func DefaultPatterns() []TraumaPattern {
	return []TraumaPattern{
		{
			ID:          "rm-rf-root",
			Pattern:     "rm -rf /",
			Description: "Recursive deletion of root directory",
			Severity:    SeverityCritical,
			Category:    "filesystem",
		},
		{
			ID:          "drop-database",
			Pattern:     "DROP DATABASE",
			Description: "Database deletion operation",
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
}
