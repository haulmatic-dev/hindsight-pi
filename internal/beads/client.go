package beads

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/buddhi/hydra/pkg/types"
)

// Client wraps the beads CLI commands
type Client struct{}

// NewClient creates a new beads client
func NewClient() *Client {
	return &Client{}
}

// GetReadyBeads retrieves beads that are ready to execute (dependencies satisfied)
func (c *Client) GetReadyBeads(projectPath, repoName string) ([]types.Bead, error) {
	// For single repo, run in project root
	// For multi-repo, run in repo subdirectory
	workDir := projectPath
	if repoName != "" {
		workDir = filepath.Join(projectPath, repoName)
	}

	// Execute br ready
	cmd := exec.Command("br", "ready")
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("br ready failed: %w (output: %s)", err, string(output))
	}

	// Parse output
	return c.parseReadyOutput(string(output), projectPath, repoName)
}

// parseReadyOutput parses the output of 'br ready'
func (c *Client) parseReadyOutput(output, projectPath, repoName string) ([]types.Bead, error) {
	var beads []types.Bead

	// Output format:
	// # Ready Tasks (X)
	//
	// ID    | Title          | Dependencies
	// ------+----------------+-------------
	// 001   | Fix bug        |
	// 002   | Implement feat | 001
	//
	// Or if no ready tasks:
	// No ready tasks found.

	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "ID") ||
			strings.HasPrefix(line, "-") || strings.HasPrefix(line, "No ready") {
			continue
		}

		// Parse line: "001   | Fix bug        | "
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}

		idStr := strings.TrimSpace(parts[0])
		title := strings.TrimSpace(parts[1])

		// Parse ID
		idNum, err := strconv.Atoi(idStr)
		if err != nil {
			continue
		}

		bead := types.Bead{
			ID: types.BeadID{
				Project: filepath.Base(projectPath),
				Repo:    repoName,
				Number:  idNum,
			},
			Title:   title,
			Project: filepath.Base(projectPath),
			Repo:    repoName,
		}

		beads = append(beads, bead)
	}

	return beads, nil
}

// GetBead retrieves full details of a bead
func (c *Client) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	workDir := projectPath
	if repoName != "" {
		workDir = filepath.Join(projectPath, repoName)
	}

	cmd := exec.Command("br", "show", fmt.Sprintf("%d", beadID.Number))
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("br show failed: %w", err)
	}

	return c.parseBeadDetails(string(output), beadID, projectPath, repoName)
}

// parseBeadDetails parses the output of 'br show <id>'
func (c *Client) parseBeadDetails(output string, beadID types.BeadID, projectPath, repoName string) (*types.Bead, error) {
	// TODO: Parse full bead details including type, description, dependencies, etc.
	// For now, return basic info
	return &types.Bead{
		ID:      beadID,
		Title:   "", // Will be filled from parse
		Project: filepath.Base(projectPath),
		Repo:    repoName,
	}, nil
}

// CloseBead marks a bead as completed
func (c *Client) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	workDir := projectPath
	if repoName != "" {
		workDir = filepath.Join(projectPath, repoName)
	}

	cmd := exec.Command("br", "close", fmt.Sprintf("%d", beadID.Number), "-m", reason)
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("br close failed: %w (output: %s)", err, string(output))
	}

	return nil
}

// ListBeads lists all beads in a project/repo
func (c *Client) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	workDir := projectPath
	if repoName != "" {
		workDir = filepath.Join(projectPath, repoName)
	}

	cmd := exec.Command("br", "list")
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("br list failed: %w", err)
	}

	return c.parseListOutput(string(output), projectPath, repoName)
}

// parseListOutput parses the output of 'br list'
func (c *Client) parseListOutput(output, projectPath, repoName string) ([]types.Bead, error) {
	// Similar to parseReadyOutput but includes all beads
	// TODO: Implement full parsing
	return []types.Bead{}, nil
}
