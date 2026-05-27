// Package cli provides a unified command-line runner for Hydra subcommands.
// It wraps os/exec with consistent error handling, directory context, timeouts,
// and optional pluggable executors for testing.
package cli

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// Executor defines the interface for running commands.
// The default implementation uses os/exec. Tests may provide mock executors.
type Executor interface {
	Execute(name string, arg ...string) ([]byte, error)
}

// ExecutorWithDir extends Executor with directory-aware execution.
type ExecutorWithDir interface {
	Executor
	ExecuteInDir(dir string, name string, arg ...string) ([]byte, error)
}

// ExecutorWithContext extends Executor with context-aware execution.
type ExecutorWithContext interface {
	Executor
	ExecuteContext(ctx context.Context, name string, arg ...string) ([]byte, error)
}

// CLIError represents an error from a CLI command execution.
type CLIError struct {
	Command string
	Args    []string
	Stdout  string
	Stderr  string
	Err     error
}

func (e *CLIError) Error() string {
	return fmt.Sprintf("command %s %v failed: %v", e.Command, e.Args, e.Err)
}

func (e *CLIError) Unwrap() error {
	return e.Err
}

// defaultExecutor wraps exec.Command for standard execution.
type defaultExecutor struct{}

func (e *defaultExecutor) Execute(name string, arg ...string) ([]byte, error) {
	return exec.Command(name, arg...).Output()
}

// defaultExecutorWithDir wraps exec.Command with a working directory.
type defaultExecutorWithDir struct {
	Dir string
}

func (e *defaultExecutorWithDir) Execute(name string, arg ...string) ([]byte, error) {
	cmd := exec.Command(name, arg...)
	cmd.Dir = e.Dir
	return cmd.Output()
}

func (e *defaultExecutorWithDir) ExecuteInDir(dir string, name string, arg ...string) ([]byte, error) {
	cmd := exec.Command(name, arg...)
	cmd.Dir = dir
	return cmd.Output()
}

// Runner executes CLI commands with optional directory and context support.
type Runner struct {
	executor Executor
	timeout  time.Duration
}

// NewRunner creates a new CLI runner with the default executor.
func NewRunner() *Runner {
	return &Runner{
		executor: &defaultExecutor{},
	}
}

// NewRunnerWithTimeout creates a runner with a default timeout for all commands.
func NewRunnerWithTimeout(timeout time.Duration) *Runner {
	return &Runner{
		executor: &defaultExecutor{},
		timeout:  timeout,
	}
}

// WithExecutor returns a new Runner using the given executor (for testing).
func (r *Runner) WithExecutor(e Executor) *Runner {
	return &Runner{
		executor: e,
		timeout:  r.timeout,
	}
}

// Run executes a command and returns the output.
func (r *Runner) Run(name string, arg ...string) ([]byte, error) {
	output, err := r.executor.Execute(name, arg...)
	if err != nil {
		return nil, &CLIError{
			Command: name,
			Args:    arg,
			Err:     err,
			Stdout:  string(output),
		}
	}
	return output, nil
}

// RunInDir executes a command in a specific directory.
func (r *Runner) RunInDir(dir string, name string, arg ...string) ([]byte, error) {
	var output []byte
	var err error

	if execWithDir, ok := r.executor.(ExecutorWithDir); ok {
		output, err = execWithDir.ExecuteInDir(dir, name, arg...)
	} else {
		// Fallback: wrap default executor with directory
		fallback := &defaultExecutorWithDir{Dir: dir}
		output, err = fallback.Execute(name, arg...)
	}

	if err != nil {
		return nil, &CLIError{
			Command: name,
			Args:    arg,
			Err:     err,
			Stdout:  string(output),
		}
	}
	return output, nil
}

// RunContext executes a command with context (supports timeouts/cancellation).
func (r *Runner) RunContext(ctx context.Context, name string, arg ...string) ([]byte, error) {
	if execCtx, ok := r.executor.(ExecutorWithContext); ok {
		output, err := execCtx.ExecuteContext(ctx, name, arg...)
		if err != nil {
			return nil, &CLIError{
				Command: name,
				Args:    arg,
				Err:     err,
				Stdout:  string(output),
			}
		}
		return output, nil
	}

	// Fallback: use standard exec with context
	cmd := exec.CommandContext(ctx, name, arg...)
	output, err := cmd.Output()
	if err != nil {
		return nil, &CLIError{
			Command: name,
			Args:    arg,
			Err:     err,
			Stdout:  string(output),
		}
	}
	return output, nil
}

// RunInDirContext executes a command in a directory with context.
func (r *Runner) RunInDirContext(ctx context.Context, dir string, name string, arg ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, arg...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return nil, &CLIError{
			Command: name,
			Args:    arg,
			Err:     err,
			Stdout:  string(output),
		}
	}
	return output, nil
}

// RunWithTimeout executes a command with a timeout.
func (r *Runner) RunWithTimeout(timeout time.Duration, name string, arg ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return r.RunContext(ctx, name, arg...)
}
