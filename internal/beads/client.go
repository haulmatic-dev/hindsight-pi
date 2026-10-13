package beads

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/haulmatic-dev/hydra/internal/logx"
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
	// br's fsqlite WAL layer stages files under FIXED names in $TMPDIR
	// (beads.db, bd-beads.json, beads.db-fsqlite-ns-gate, ...). Any two br
	// processes — even against different stores — collide on those names,
	// producing cross-store WAL phantom writes and SYNC_CONFLICT lockouts
	// (observed 2026-10-07: hydra fix beads phantom-duplicated into
	// srv-jobfile's WAL; store unreadable for hours). Isolate each
	// invocation's TMPDIR and clean it up afterwards.
	tmpDir, tmpErr := os.MkdirTemp("", "hydra-br-")
	if tmpErr != nil {
		// Fail open: run with the inherited TMPDIR rather than not running.
		log.Printf("[beads] TMPDIR isolation unavailable (%v); using inherited TMPDIR", tmpErr)
	}
	cmd := exec.Command(name, arg...)
	cmd.Dir = e.Dir
	if tmpErr == nil {
		defer os.RemoveAll(tmpDir)
		cmd.Env = append(os.Environ(), "TMPDIR="+tmpDir)
	}
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

// repoCwd resolves the working directory for br commands. For a multi-repo
// project the .beads workspace lives in the repo subdirectory
// (projectPath/repoName), not the project root — running br in the parent
// (which has no .beads) fails. Single-repo callers pass an empty repoName and
// run in projectPath unchanged, so existing behavior is preserved.
func repoCwd(projectPath, repoName string) string {
	if repoName != "" {
		return filepath.Join(projectPath, repoName)
	}
	return projectPath
}

// loadJSONLMetadata reads the canonical .beads/issues.jsonl and returns a map
// of bead ID to merged metadata/bead_metadata. br's native responses do not
// expose arbitrary metadata, but validators and workers rely on fields like
// target_files. This fallback keeps those fields available without requiring
// a br schema change.
func (c *Client) loadJSONLMetadata(projectPath, repoName string) (map[string]map[string]string, error) {
	filePath := filepath.Join(repoCwd(projectPath, repoName), ".beads", "issues.jsonl")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	result := make(map[string]map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw struct {
			ID           string            `json:"id"`
			Metadata     map[string]string `json:"metadata"`
			BeadMetadata map[string]string `json:"bead_metadata"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		merged := make(map[string]string)
		for k, v := range raw.BeadMetadata {
			merged[k] = v
		}
		for k, v := range raw.Metadata {
			merged[k] = v
		}
		if len(merged) > 0 {
			result[raw.ID] = merged
		}
	}
	return result, nil
}

// applyJSONLMetadata overlays metadata from the canonical JSONL onto a bead.
// It does not overwrite keys already present from the br response.
func (c *Client) applyJSONLMetadata(bead *types.Bead, allMeta map[string]map[string]string) {
	if bead.Metadata == nil {
		bead.Metadata = make(map[string]string)
	}
	if meta, ok := allMeta[string(bead.ID)]; ok {
		for k, v := range meta {
			if _, exists := bead.Metadata[k]; !exists {
				bead.Metadata[k] = v
			}
		}
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

// brExecLocks serializes br invocations per store directory. A raced
// concurrent `br` rebuild can delete .beads/issues.jsonl and rebuild
// beads.db with 0 issues (srv-smart-engine, 2026-09-11). Serializing per
// store keeps concurrency across independent projects while eliminating the
// race for any single .beads directory.
var brExecLocks sync.Map // string dir -> *sync.Mutex

// execInDir runs a CLI command in a directory, serializing br calls and
// retrying transient daemon-lock failures (exit 8 / "database is locked")
// with exponential backoff. Non-transient errors return immediately.
func (c *Client) execInDir(dir string, cmd string, args ...string) ([]byte, error) {
	if cmd == "br" {
		key := filepath.Clean(dir)
		muIface, _ := brExecLocks.LoadOrStore(key, &sync.Mutex{})
		mu := muIface.(*sync.Mutex)
		mu.Lock()
		defer mu.Unlock()
	}
	const maxAttempts = 4
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		out, err := c.execInDirOnce(dir, cmd, args...)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if !isTransientError(err) {
			// Preserve output on error: closeBeadOnce classifies skip vs
			// already-closed from stdout, which only arrives with nonzero
			// exits when the CLIError carries it.
			return out, err
		}
		time.Sleep(time.Duration(1<<uint(attempt-1)) * 150 * time.Millisecond)
	}
	return nil, lastErr
}

// execInDirOnce executes a CLI command in a specific directory (single attempt).
func (c *Client) execInDirOnce(dir string, cmd string, args ...string) ([]byte, error) {
	// If the executor supports directory-aware execution, use it
	if execWithDir, ok := c.executor.(interface {
		ExecuteInDir(dir string, name string, arg ...string) ([]byte, error)
	}); ok {
		output, err := execWithDir.ExecuteInDir(dir, cmd, args...)
		if err != nil {
			return output, &CLIError{
				Command: cmd,
				Args:    args,
				Stdout:  string(output),
				Err:     err,
			}
		}
		return output, nil
	}

	// Fall back to creating a directory-aware executor
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
	output, err := c.execInDir(repoCwd(projectPath, repoName), "br", "ready", "--json")
	if err != nil {
		logx.Debugf("GetReadyBeads ERROR for %s: %v", projectPath, err)
		if cliErr, ok := err.(*CLIError); ok {
			logx.Debugf("stdout: %s", cliErr.Stdout)
			logx.Debugf("stderr: %s", cliErr.Stderr)
		}
		return nil, err
	}

	outputStr := string(output)
	logx.Debugf("GetReadyBeads SUCCESS for %s (len=%d): %s", projectPath, len(output), outputStr)

	if len(outputStr) < 5 || outputStr == "[]" {
		logx.Debugf("GetReadyBeads empty/minimal output for %s", projectPath)
	}

	beads, err := c.parser.ParseReadyBeads(output, BeadContext{
		Project: projectName,
		Repo:    repoName,
	})

	// Enrich metadata from the canonical JSONL. br's response format does not
	// include arbitrary metadata, but workers and validators need fields such
	// as target_files that are stored in issues.jsonl.
	if meta, metaErr := c.loadJSONLMetadata(projectPath, repoName); metaErr == nil {
		for i := range beads {
			c.applyJSONLMetadata(&beads[i], meta)
		}
	}

	logx.Debugf("GetReadyBeads parsed %d beads for %s", len(beads), projectPath)

	return beads, err
}

// GetBead retrieves full details of a bead by its string ID
func (c *Client) GetBead(projectPath, repoName string, beadID types.BeadID) (*types.Bead, error) {
	// Must run in project directory to find .beads folder
	output, err := c.execInDir(repoCwd(projectPath, repoName), "br", "show", beadID.String(), "--json")
	if err != nil {
		return nil, err
	}
	bead, err := c.parser.ParseBeadDetail(output, c.newContext(projectPath, repoName))
	if err != nil {
		return nil, err
	}

	// Enrich metadata from the canonical JSONL so target_files and other
	// metadata fields remain visible to validators and workers.
	if meta, metaErr := c.loadJSONLMetadata(projectPath, repoName); metaErr == nil {
		c.applyJSONLMetadata(bead, meta)
	}

	return bead, nil
}

// UpdateBeadStatus updates the status of a bead (e.g., to "in_progress")
func (c *Client) UpdateBeadStatus(projectPath string, beadID types.BeadID, status types.BeadStatus) error {
	_, err := c.execInDir(projectPath, "br", "update", beadID.String(), "--status", string(status))
	if err != nil {
		return fmt.Errorf("update status failed for bead %s: %w", beadID.String(), err)
	}
	return nil
}

// AddDependency records that beadID depends on dependsOn (br dep add).
func (c *Client) AddDependency(projectPath, repoName string, beadID, dependsOn types.BeadID) error {
	_, err := c.execInDir(repoCwd(projectPath, repoName), "br", "dep", "add", beadID.String(), dependsOn.String())
	if err != nil {
		return fmt.Errorf("dep add %s -> %s failed: %w", beadID.String(), dependsOn.String(), err)
	}
	return nil
}

// CloseBead marks a bead as completed and verifies the closure
func (c *Client) CloseBead(projectPath, repoName string, beadID types.BeadID, reason string) error {
	// Transient daemon-lock (exit 8) retry is handled centrally in execInDir,
	// which serializes br calls and retries with backoff. closeBeadOnce keeps
	// the --force retry-on-open-children logic.
	return c.closeBeadOnce(projectPath, repoName, beadID, reason)
}

// isTransientError returns true if the error looks like a transient br daemon
// issue (database lock) rather than a logical error (bead not found, etc.).
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// br daemon returns exit 8 on database lock / write conflicts. We also treat
	// any "-force" retry-on-skip success-path error as non-transient.
	return strings.Contains(errStr, "exit status 8") ||
		strings.Contains(errStr, "database is locked") ||
		strings.Contains(errStr, "resource temporarily unavailable")
}

// closeBeadOnce performs a single close attempt with --force retry-on-skip logic.
func (c *Client) closeBeadOnce(projectPath, repoName string, beadID types.BeadID, reason string) error {
	output, err := c.execInDir(repoCwd(projectPath, repoName), "br", "close", beadID.String(), "-r", reason)
	outputStr := string(output)

	// Classify the close outcome. A skip (open children) can arrive as a
	// zero-exit warning OR as a nonzero exit (br daemon exit 3, "already
	// closed or skipped") — both must reach the --force retry below.
	// Regression 2026-09-29 (srv-telematics u8z7/kher/qj74/5gq): a nonzero
	// skip returned nil as "idempotent success", so Close() completed, the
	// worker was marked completed, and the bead stayed open — an endless
	// respawn loop with no error anywhere. Silence is the bug.
	skipped := strings.Contains(outputStr, "Warning: Skipped") || strings.Contains(outputStr, "blocked by")
	if err != nil {
		if strings.Contains(outputStr, "already closed") {
			// Explicit already-closed wording: genuinely idempotent success.
			return nil
		}
		exitCode := -1
		if cliErr, ok := err.(*CLIError); ok {
			if exitErr, ok2 := cliErr.Err.(*exec.ExitError); ok2 {
				exitCode = exitErr.ExitCode()
			}
		}
		if exitCode == 3 {
			// Exit 3 is ambiguous ("already closed or skipped") with no
			// distinguishing wording. Never assume "already closed": force
			// close is a harmless no-op when already terminal, and the
			// terminality verification below fails loud if it is not.
			skipped = true
		} else if !skipped {
			// Non-skipped CLI error — propagate (will be checked for transience)
			return fmt.Errorf("close failed for bead %s: %w", beadID.String(), err)
		}
	}

	// Check if close was skipped due to open children / dependencies.
	// In that case, retry with --force to allow closing failed/abandoned beads
	// without blocking downstream work. Without --force, a failed worker that
	// leaves a bead stuck in_progress with open children would never be recovered.
	if skipped {
		// Epics are never force-closed over an open-children skip. br's own
		// parent-child graph is the ground truth the DAG cannot see when
		// compile-time epic-child dependency links failed (br single-parent
		// constraint); a forced close would silently mark an epic done over
		// open work (srv-jobfile master epic: closed with 33 open children).
		bead, gerr := c.GetBead(projectPath, repoName, beadID)
		if gerr != nil {
			return fmt.Errorf("close of bead %s skipped (%s); refusing to force-close because bead type could not be verified: %v",
				beadID.String(), outputStr, gerr)
		}
		if bead.Status.IsTerminal() {
			// Terminal already: the skip was benign; verification below passes.
		} else if bead.Type == "epic" {
			return fmt.Errorf("refusing to force-close epic %s: br reports open children (%s)",
				beadID.String(), outputStr)
		} else {
			log.Printf("[CloseBead] Bead %s has open children; retrying close with --force (reason: %s)", beadID.String(), reason)
			forceOutput, forceErr := c.execInDir(repoCwd(projectPath, repoName), "br", "close", beadID.String(), "-r", reason, "--force")
			if forceErr != nil {
				return fmt.Errorf("force-close failed for bead %s: %w (original skip: %s)", beadID.String(), forceErr, outputStr)
			}
			if strings.Contains(string(forceOutput), "Warning: Skipped") {
				return fmt.Errorf("force-close still skipped for bead %s: %s", beadID.String(), string(forceOutput))
			}
			log.Printf("[CloseBead] Force-closed bead %s (had open children at the time)", beadID.String())
		}
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

// ListBeads lists all beads in a project/repo with pagination support.
func (c *Client) ListBeads(projectPath, repoName string) ([]types.Bead, error) {
	allBeads := make([]types.Bead, 0)
	offset := 0
	limit := 500
	ctx := c.newContext(projectPath, repoName)

	for {
		output, err := c.execInDir(repoCwd(projectPath, repoName), "br", "list", "--json", "--limit", fmt.Sprintf("%d", limit), "--offset", fmt.Sprintf("%d", offset))
		if err != nil {
			return nil, err
		}
		beads, hasMore, err := c.parser.ParseBeadListPaginated(output, ctx)
		if err != nil {
			return nil, err
		}
		allBeads = append(allBeads, beads...)
		if !hasMore {
			break
		}
		offset += limit
	}
	return allBeads, nil
}

// CreateBead creates a new bead using the br CLI.
func (c *Client) CreateBead(projectPath string, req types.CreateBeadRequest) (types.BeadID, error) {
	args := []string{"create", req.Title, "--json"}
	if req.Description != "" {
		args = append(args, "--description", req.Description)
	}
	if req.Type != "" {
		args = append(args, "--type", req.Type)
	}
	if req.Priority > 0 {
		args = append(args, "--priority", fmt.Sprintf("%d", req.Priority))
	}
	if len(req.Labels) > 0 {
		args = append(args, "--labels", strings.Join(req.Labels, ","))
	}
	if len(req.Dependencies) > 0 {
		depStrs := make([]string, len(req.Dependencies))
		for i, dep := range req.Dependencies {
			depStrs[i] = dep.String()
		}
		args = append(args, "--deps", strings.Join(depStrs, ","))
	}
	if req.Status != "" {
		args = append(args, "--status", req.Status)
	}

	output, err := c.execInDir(projectPath, "br", args...)
	if err != nil {
		if cliErr, ok := err.(*CLIError); ok {
			cliErr.Stdout = string(output)
		}
		return "", fmt.Errorf("create bead failed: %w", err)
	}

	beadID, err := c.parser.ParseCreatedBead(output)
	if err != nil {
		return "", fmt.Errorf("failed to parse created bead: %w", err)
	}

	return beadID, nil
}

// UpdateBeadLabels adds labels to an existing bead using br label add.
func (c *Client) UpdateBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	// br's -l flag is singular: repeated flags in one invocation exit 2,
	// which silently dropped every gate-failed label (ux5/ffh7l 2026-08-21).
	for _, label := range labels {
		if _, err := c.execInDir(projectPath, "br", "label", "add", string(beadID), "-l", label); err != nil {
			return err
		}
	}
	return nil
}

// RemoveBeadLabels removes labels from a bead using br label remove. Used by
// Pan's clear-hard-park repair to un-park beads whose durable park label
// (worktree-unavailable, integration-exhausted) has outlived its cause.
func (c *Client) RemoveBeadLabels(projectPath, repoName string, beadID types.BeadID, labels []string) error {
	for _, label := range labels {
		if _, err := c.execInDir(projectPath, "br", "label", "remove", string(beadID), "-l", label); err != nil {
			return err
		}
	}
	return nil
}

// UpdateBeadDescription replaces a bead's description via br update.
// Used by the daemon gate runner to hand a failed gate's parsed summary +
// raw log path to the retry worker (FR-002 slice 2).
func (c *Client) UpdateBeadDescription(projectPath, repoName string, beadID types.BeadID, description string) error {
	_, err := c.execInDir(projectPath, "br", "update", string(beadID), "--description", description)
	return err
}

// UpdateBeadAgentContext writes a bead's governing-instructions JSON via
// `br update --agent-context @file` (beads_rust#297, br 0.2.10). The context
// pack is serialized to a temp file because inline JSON breaks on shell
// quoting; the @path form is br's supported channel. Callers must treat a
// returned error as fail-open: an unguided spawn beats a blocked one.
func (c *Client) UpdateBeadAgentContext(projectPath, repoName string, beadID types.BeadID, ctxJSON string) error {
	f, err := os.CreateTemp("", "hydra-agent-context-*.json")
	if err != nil {
		return fmt.Errorf("agent-context temp file: %w", err)
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err := f.WriteString(ctxJSON); err != nil {
		f.Close()
		return fmt.Errorf("agent-context temp file write: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("agent-context temp file close: %w", err)
	}
	_, err = c.execInDir(projectPath, "br", "update", string(beadID), "--agent-context", "@"+path)
	return err
}
