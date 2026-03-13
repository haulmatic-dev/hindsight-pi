package beads

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// CLIError represents an error from executing a CLI command.
type CLIError struct {
	Command string
	Args    []string
	Stdout  string
	Stderr  string
	Err     error
}

func (e *CLIError) Error() string {
	cmd := e.Command
	if len(e.Args) > 0 {
		cmd = cmd + " " + strings.Join(e.Args, " ")
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("%s failed", cmd))

	if e.Stdout != "" {
		parts = append(parts, fmt.Sprintf("stdout: %s", strings.TrimSpace(e.Stdout)))
	}

	if e.Stderr != "" {
		parts = append(parts, fmt.Sprintf("stderr: %s", strings.TrimSpace(e.Stderr)))
	}

	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}

	return strings.Join(parts, ": ")
}

func (e *CLIError) Unwrap() error {
	return e.Err
}

// CLIExecutor abstracts command execution for testability
type CLIExecutor interface {
	Execute(name string, arg ...string) ([]byte, error)
}

// defaultExecutor implements CLIExecutor using os/exec
type defaultExecutor struct{}

func (e *defaultExecutor) Execute(name string, arg ...string) ([]byte, error) {
	return exec.Command(name, arg...).Output()
}

// defaultExecutorWithDir executes commands in a specific directory
type defaultExecutorWithDir struct {
	Dir string
}

func (e *defaultExecutorWithDir) Execute(name string, arg ...string) ([]byte, error) {
	cmd := exec.Command(name, arg...)
	cmd.Dir = e.Dir
	// Capture both stdout and stderr so we can check for warnings
	return cmd.CombinedOutput()
}

// Client wraps the beads CLI commands
type Client struct {
	executor CLIExecutor
	parser   *Parser
}

// NewClient creates a new beads client using the default executor
func NewClient() *Client {
	return &Client{
		executor: &defaultExecutor{},
		parser:   NewParser(),
	}
}

// NewClientWithExecutor creates a client with a custom executor (for testing)
func NewClientWithExecutor(executor CLIExecutor) *Client {
	return &Client{
		executor: executor,
		parser:   NewParser(),
	}
}

// newContext creates a BeadContext from project path and repo name
func (c *Client) newContext(projectPath, repoName string) BeadContext {
	return BeadContext{
		Project: filepath.Base(projectPath),
		Repo:    repoName,
	}
}

// exec executes a CLI command and returns the output or a CLIError with full details
func (c *Client) exec(cmd string, args ...string) ([]byte, error) {
	output, err := c.executor.Execute(cmd, args...)
	if err != nil {
		return nil, &CLIError{
			Command: cmd,
			Args:    args,
			Err:     err,
		}
	}
	return output, nil
}

// execInDir executes a CLI command in a specific directory
func (c *Client) execInDir(dir string, cmd string, args ...string) ([]byte, error) {
	// Create a directory-aware executor for the command
	executor := &defaultExecutorWithDir{Dir: dir}
	output, err := executor.Execute(cmd, args...)
	if err != nil {
		return nil, &CLIError{
			Command: cmd,
			Args:    args,
			Err:     err,
		}
	}
	return output, nil
}

// execJSON executes a CLI command that returns JSON and parses it
func (c *Client) execJSON(projectPath, repoName, cmd string, args []string, parseFn func([]byte, BeadContext) error) error {
	output, err := c.exec(cmd, args...)
	if err != nil {
		return err
	}
	return parseFn(output, c.newContext(projectPath, repoName))
}

// GetReadyBeads retrieves beads ready to execute (dependencies satisfied)
func (c *Client) GetReadyBeads(projectName, projectPath, repoName string) ([]types.Bead, error) {
	// Use project path as working directory so br commands find the .beads folder
	output, err := c.execInDir(projectPath, "br", "ready", "--json")
	if err != nil {
		// Debug: log the error details
		fmt.Printf("[DEBUG] GetReadyBeads ERROR for %s: %v\n", projectPath, err)
		if cliErr, ok := err.(*CLIError); ok {
			fmt.Printf("[DEBUG] stdout: %s\n", cliErr.Stdout)
			fmt.Printf("[DEBUG] stderr: %s\n", cliErr.Stderr)
		}
		return nil, err
	}

	// Debug: log raw output
	outputStr := string(output)
	fmt.Printf("[DEBUG] GetReadyBeads SUCCESS for %s (len=%d): %s\n", projectPath, len(output), outputStr)

	// Check if output is empty or just []
	if len(outputStr) < 5 || outputStr == "[]" {
		fmt.Printf("[DEBUG] WARNING: Empty or minimal output for %s\n", projectPath)
	}

	beads, err := c.parser.ParseReadyBeads(output, BeadContext{
		Project: projectName,
		Repo:    repoName,
	})

	// Debug: log parsed results
	fmt.Printf("[DEBUG] GetReadyBeads parsed %d beads for %s\n", len(beads), projectPath)

	return beads, err
}

// GetBead retrieves full details of a bead by its string ID
func (c *Client) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	output, err := c.exec("br", "show", beadID.String(), "--json")
	if err != nil {
		return nil, err
	}
	return c.parser.ParseBeadDetail(output, c.newContext(projectPath, repoName))
}

// UpdateBeadStatus updates the status of a bead (e.g., to "in_progress")
func (c *Client) UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error {
	_, err := c.execInDir(projectPath, "br", "update", beadID.String(), "--status", string(status))
	if err != nil {
		return fmt.Errorf("update status failed for bead %s: %w", beadID.String(), err)
	}
	return nil
}

// CloseBead marks a bead as completed and verifies the closure
func (c *Client) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	output, err := c.execInDir(projectPath, "br", "close", beadID.String(), "-r", reason)
	if err != nil {
		return fmt.Errorf("close failed for bead %s: %w", beadID.String(), err)
	}

	// Check if close was skipped due to dependencies
	outputStr := string(output)
	if strings.Contains(outputStr, "Warning: Skipped") || strings.Contains(outputStr, "blocked by") {
		return fmt.Errorf("bead %s skipped due to dependencies: %s", beadID.String(), outputStr)
	}

	// Verify the bead was actually closed
	bead, err := c.GetBead(projectPath, repoName, beadID)
	if err != nil {
		return fmt.Errorf("failed to verify bead %s closure: %w", beadID.String(), err)
	}

	if !bead.Status.IsTerminal() {
		return fmt.Errorf("bead %s not closed (status: %s). May have unresolved dependencies", beadID.String(), bead.Status)
	}

	return nil
}

// ListBeads lists all beads in a project/repo
func (c *Client) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	output, err := c.execInDir(projectPath, "br", "list", "--json")
	if err != nil {
		return nil, err
	}
	return c.parser.ParseBeadList(output, c.newContext(projectPath, repoName))
}
