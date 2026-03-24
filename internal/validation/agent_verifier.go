package validation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// AgentVerifier performs semantic validation using an AI agent
type AgentVerifier struct {
	endpoint string
	timeout  time.Duration
	client   *http.Client
}

// NewAgentVerifier creates a new agent verifier
func NewAgentVerifier(endpoint string, timeout time.Duration) *AgentVerifier {
	return &AgentVerifier{
		endpoint: endpoint,
		timeout:  timeout,
		client:   &http.Client{Timeout: timeout},
	}
}

// AgentVerificationResult represents the outcome of agent verification
type AgentVerificationResult struct {
	Passed   bool
	Errors   []ValidationError
	Score    float64 // 0.0 to 1.0
	Feedback string
}

// VerificationRequest represents the request to the agent
type VerificationRequest struct {
	BeadID             string        `json:"bead_id"`
	Title              string        `json:"title"`
	Description        string        `json:"description"`
	AcceptanceCriteria []string      `json:"acceptance_criteria"`
	TargetFiles        []FileContent `json:"target_files"`
	TestFiles          []FileContent `json:"test_files"`
	Labels             []string      `json:"labels"`
}

// FileContent represents a file and its content
type FileContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// VerificationResponse represents the agent's response
type VerificationResponse struct {
	Passed     bool    `json:"passed"`
	Score      float64 `json:"score"`
	Issues     []Issue `json:"issues"`
	Feedback   string  `json:"feedback"`
	Confidence float64 `json:"confidence"`
}

// Issue represents a problem found by the agent
type Issue struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
}

// Verify performs agent-based verification of a bead implementation
func (a *AgentVerifier) Verify(ctx context.Context, bead types.Bead, projectPath string) (*AgentVerificationResult, error) {
	// Build verification request
	req, err := a.buildVerificationRequest(bead, projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to build verification request: %w", err)
	}

	// Send request to agent
	resp, err := a.sendVerificationRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("agent verification request failed: %w", err)
	}

	// Convert response to result
	result := &AgentVerificationResult{
		Passed:   resp.Passed,
		Score:    resp.Score,
		Feedback: resp.Feedback,
		Errors:   make([]ValidationError, 0),
	}

	// Convert issues to validation errors
	for _, issue := range resp.Issues {
		severity := SeverityError
		switch issue.Severity {
		case "critical":
			severity = SeverityCritical
		case "error":
			severity = SeverityError
		case "warning":
			severity = SeverityWarning
		case "info":
			severity = SeverityInfo
		}

		result.Errors = append(result.Errors, ValidationError{
			Severity: severity,
			Message:  issue.Message,
			File:     issue.File,
			Line:     issue.Line,
			Category: "agent_verification",
		})
	}

	return result, nil
}

// buildVerificationRequest creates a verification request from bead data
func (a *AgentVerifier) buildVerificationRequest(bead types.Bead, projectPath string) (*VerificationRequest, error) {
	req := &VerificationRequest{
		BeadID:             string(bead.ID),
		Title:              bead.Title,
		Description:        bead.Description,
		Labels:             bead.Labels,
		AcceptanceCriteria: extractAcceptanceCriteria(bead),
	}

	// Load target files
	targetFiles := extractTargetFiles(bead)
	for _, file := range targetFiles {
		content, err := a.loadFileContent(projectPath, file)
		if err != nil {
			return nil, fmt.Errorf("failed to load target file %s: %w", file, err)
		}
		req.TargetFiles = append(req.TargetFiles, FileContent{
			Path:    file,
			Content: content,
		})
	}

	// Load test files
	testFiles := a.findTestFiles(targetFiles)
	for _, file := range testFiles {
		content, err := a.loadFileContent(projectPath, file)
		if err != nil {
			// Test file might not exist yet
			continue
		}
		req.TestFiles = append(req.TestFiles, FileContent{
			Path:    file,
			Content: content,
		})
	}

	return req, nil
}

// loadFileContent reads a file's content
func (a *AgentVerifier) loadFileContent(projectPath, file string) (string, error) {
	fullPath := filepath.Join(projectPath, file)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// findTestFiles generates expected test file names from target files
func (a *AgentVerifier) findTestFiles(targetFiles []string) []string {
	testFiles := make([]string, 0)

	for _, file := range targetFiles {
		if strings.HasSuffix(file, ".go") && !strings.HasSuffix(file, "_test.go") {
			dir := filepath.Dir(file)
			base := filepath.Base(file)
			ext := filepath.Ext(base)
			name := strings.TrimSuffix(base, ext)
			testFile := filepath.Join(dir, name+"_test.go")
			testFiles = append(testFiles, testFile)
		}
	}

	return testFiles
}

// sendVerificationRequest sends the request to the agent endpoint
func (a *AgentVerifier) sendVerificationRequest(ctx context.Context, req *VerificationRequest) (*VerificationResponse, error) {
	// Serialize request
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agent returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var verificationResp VerificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&verificationResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &verificationResp, nil
}

// extractAcceptanceCriteria extracts acceptance criteria from bead metadata
func extractAcceptanceCriteria(bead types.Bead) []string {
	criteria := make([]string, 0)

	// Try to get from metadata
	if bead.Metadata != nil {
		if criteriaStr, ok := bead.Metadata["acceptance_criteria"]; ok {
			// Split by newlines or semicolons
			parts := strings.Split(criteriaStr, "\n")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part != "" {
					criteria = append(criteria, part)
				}
			}
		}
	}

	// If no criteria found, try to extract from description
	if len(criteria) == 0 {
		criteria = parseCriteriaFromDescription(bead.Description)
	}

	return criteria
}

// parseCriteriaFromDescription attempts to extract criteria from description text
func parseCriteriaFromDescription(description string) []string {
	criteria := make([]string, 0)

	// Look for common patterns
	lines := strings.Split(description, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Check for bullet points or numbered items that look like criteria
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") {
			criteria = append(criteria, strings.TrimPrefix(strings.TrimPrefix(line, "-"), "*"))
		} else if matched, _ := regexp.MatchString(`^\d+\.\s`, line); matched {
			// Remove number prefix
			parts := strings.SplitN(line, ".", 2)
			if len(parts) > 1 {
				criteria = append(criteria, strings.TrimSpace(parts[1]))
			}
		}
	}

	return criteria
}
