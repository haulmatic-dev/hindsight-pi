package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// IndexRequest represents a request to index a repository
type IndexRequest struct {
	RepoPath string
	Attempt  int
}

// TLDRIndexer provides deterministic TLDR indexing with concurrency control
type TLDRIndexer struct {
	queue         chan IndexRequest
	semaphore     chan struct{} // Controls max concurrent
	maxConcurrent int
	retryAttempts int
	retryDelay    time.Duration
	tldrPath      string
	active        map[string]bool // Track currently indexing repos (dedup)
	mu            sync.RWMutex
	stopCh        chan struct{}
	wg            sync.WaitGroup
	started       bool
}

// NewTLDRIndexer creates a new TLDR indexer with the specified configuration
func NewTLDRIndexer(maxConcurrent, retryAttempts int, retryDelay time.Duration, tldrPath string) *TLDRIndexer {
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}
	if retryAttempts <= 0 {
		retryAttempts = 2
	}
	if retryDelay <= 0 {
		retryDelay = 5 * time.Second
	}
	if tldrPath == "" {
		tldrPath = "hydra-llm-tldr"
	}

	return &TLDRIndexer{
		queue:         make(chan IndexRequest, 100),
		semaphore:     make(chan struct{}, maxConcurrent),
		maxConcurrent: maxConcurrent,
		retryAttempts: retryAttempts,
		retryDelay:    retryDelay,
		tldrPath:      tldrPath,
		active:        make(map[string]bool),
		stopCh:        make(chan struct{}),
	}
}

// QueueIndex adds a repository to the indexing queue
// Returns false if the repository is already being indexed or if the queue is full
func (ti *TLDRIndexer) QueueIndex(repoPath string) bool {
	// Check if already active
	ti.mu.Lock()
	if ti.active[repoPath] {
		ti.mu.Unlock()
		return false
	}
	ti.active[repoPath] = true
	ti.mu.Unlock()

	// Try to add to queue
	select {
	case ti.queue <- IndexRequest{RepoPath: repoPath, Attempt: 0}:
		return true
	default:
		// Queue is full, remove from active
		ti.mu.Lock()
		delete(ti.active, repoPath)
		ti.mu.Unlock()
		return false
	}
}

// Start begins processing the indexing queue
func (ti *TLDRIndexer) Start() {
	ti.mu.Lock()
	defer ti.mu.Unlock()

	if ti.started {
		return
	}

	ti.started = true
	ti.wg.Add(1)
	go ti.processQueue()
}

// Stop gracefully shuts down the indexer
func (ti *TLDRIndexer) Stop() {
	ti.mu.Lock()
	if !ti.started {
		ti.mu.Unlock()
		return
	}
	ti.started = false
	ti.mu.Unlock()

	close(ti.stopCh)
	ti.wg.Wait()
}

// processQueue is the main processing loop
func (ti *TLDRIndexer) processQueue() {
	defer ti.wg.Done()

	for {
		select {
		case <-ti.stopCh:
			return
		case req := <-ti.queue:
			ti.handleRequest(req)
		}
	}
}

// handleRequest processes a single indexing request with retry logic
func (ti *TLDRIndexer) handleRequest(req IndexRequest) {
	// Acquire semaphore slot
	select {
	case ti.semaphore <- struct{}{}:
		// Acquired slot
	case <-ti.stopCh:
		// Stopping, release the active lock and return
		ti.mu.Lock()
		delete(ti.active, req.RepoPath)
		ti.mu.Unlock()
		return
	}

	defer func() {
		// Release semaphore slot
		<-ti.semaphore
	}()

	// Check if repository has code changes
	if !ti.hasCodeChanges(req.RepoPath) {
		// No changes, mark as done
		ti.mu.Lock()
		delete(ti.active, req.RepoPath)
		ti.mu.Unlock()
		return
	}

	// Attempt to index with retries
	var lastErr error
	for attempt := 0; attempt <= ti.retryAttempts; attempt++ {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-time.After(ti.retryDelay):
			case <-ti.stopCh:
				// Stopping, mark as done and return
				ti.mu.Lock()
				delete(ti.active, req.RepoPath)
				ti.mu.Unlock()
				return
			}
		}

		err := ti.indexRepo(req.RepoPath)
		if err == nil {
			// Success!
			ti.mu.Lock()
			delete(ti.active, req.RepoPath)
			ti.mu.Unlock()
			return
		}

		lastErr = err
	}

	// All retries exhausted, log error and continue
	fmt.Fprintf(os.Stderr, "TLDR indexing failed for %s after %d attempts: %v\n",
		req.RepoPath, ti.retryAttempts+1, lastErr)

	// Remove from active to allow future attempts
	ti.mu.Lock()
	delete(ti.active, req.RepoPath)
	ti.mu.Unlock()
}

// indexRepo runs the llm-tldr semantic index command
func (ti *TLDRIndexer) indexRepo(repoPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ti.tldrPath,
		"semantic", "index", ".",
		"--model", "all-MiniLM-L6-v2",
		"--lang", "all",
	)
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("indexing timed out after 30 seconds")
		}
		return fmt.Errorf("indexing failed: %w (output: %s)", err, string(output))
	}

	return nil
}

// hasCodeChanges checks if the repository has uncommitted changes using git diff
func (ti *TLDRIndexer) hasCodeChanges(repoPath string) bool {
	// First check if it's a git repository
	gitDir := fmt.Sprintf("%s/.git", repoPath)
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		// Not a git repository, assume changes exist to be safe
		return true
	}

	// Run git diff --quiet to check for changes
	// --quiet exits with 1 if there are changes, 0 if clean
	cmd := exec.Command("git", "diff", "--quiet")
	cmd.Dir = repoPath

	err := cmd.Run()
	if err != nil {
		// Exit code 1 means there are changes
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true
		}
		// Some other error occurred, assume changes exist to be safe
		return true
	}

	// No changes
	return false
}
