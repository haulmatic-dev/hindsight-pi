package core

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// GraphifyIndexer keeps each project's code knowledge graph fresh so the
// `graphify` skill has something to query.
//
// Scope is deliberately narrow: it runs `graphify update <project>` per project
// on an interval and nothing else.
//
//   - Per project, never per bead. Beads run in throwaway worktrees; building a
//     graph in each would repeat a 14.5s extraction hundreds of times per run.
//   - `update` only, never `extract`. update is pure tree-sitter AST work with no
//     model call, so refreshing costs CPU rather than tokens.
//   - Hard timeout. Upstream has an O(V x E) pathology in report generation on
//     very large monorepos (graphify issue #341: 114 minutes on 50k files, never
//     finishing), so a build that misbehaves gets killed rather than wedging the
//     daemon.
//
// Every failure is logged and swallowed. A missing or broken graph must never
// affect scheduling — the worst case is that the skill has nothing to query and
// the agent falls back to reading files, which is what it did before.
type GraphifyIndexer struct {
	binary          string
	refreshInterval time.Duration
	timeout         time.Duration
	skipCluster     bool

	sem chan struct{}

	mu        sync.Mutex
	lastBuilt map[string]time.Time
	inFlight  map[string]bool
}

// NewGraphifyIndexer builds an indexer from config, applying defaults for unset
// values so a half-filled config block cannot produce a zero timeout (which
// would cancel every build immediately).
func NewGraphifyIndexer(cfg GraphifyConfig) *GraphifyIndexer {
	binary := strings.TrimSpace(cfg.Binary)
	if binary == "" {
		binary = "graphify"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	refresh := cfg.RefreshInterval
	if refresh < 0 {
		refresh = 0
	}
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}

	return &GraphifyIndexer{
		binary:          binary,
		refreshInterval: refresh,
		timeout:         timeout,
		skipCluster:     cfg.SkipClustering,
		sem:             make(chan struct{}, maxConcurrent),
		lastBuilt:       make(map[string]time.Time),
		inFlight:        make(map[string]bool),
	}
}

// Available reports whether the graphify binary can be found. Checked once at
// call time rather than cached, so installing graphify does not require a
// daemon restart.
func (g *GraphifyIndexer) Available() bool {
	if g == nil {
		return false
	}
	_, err := exec.LookPath(g.binary)
	return err == nil
}

// Refresh rebuilds the graph for projectPath if it is due, running in the
// background so callers are never blocked. Returns true when a build was
// started.
//
// Skipped when: the binary is missing, a build for this project is already
// running, or the last build is newer than RefreshInterval.
func (g *GraphifyIndexer) Refresh(projectPath string) bool {
	if g == nil || strings.TrimSpace(projectPath) == "" {
		return false
	}
	if !g.Available() {
		return false
	}

	abs, err := filepath.Abs(projectPath)
	if err != nil {
		abs = projectPath
	}

	if !g.claim(abs) {
		return false
	}

	go func() {
		defer g.release(abs)
		g.sem <- struct{}{}
		defer func() { <-g.sem }()

		if err := g.build(abs); err != nil {
			// Fail-open: a graph is an optimisation, not a dependency.
			log.Printf("[Graphify] update failed for %s: %v", abs, err)
			return
		}
		log.Printf("[Graphify] graph refreshed for %s", abs)
	}()
	return true
}

// claim reserves a build slot for a project, enforcing the refresh interval and
// preventing concurrent builds of the same project.
func (g *GraphifyIndexer) claim(abs string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.inFlight[abs] {
		return false
	}
	if last, ok := g.lastBuilt[abs]; ok && g.refreshInterval > 0 {
		if time.Since(last) < g.refreshInterval {
			return false
		}
	}
	g.inFlight[abs] = true
	// Stamped before the build, not after: a slow build must not become a hot
	// loop of retries the moment it finishes.
	g.lastBuilt[abs] = time.Now()
	return true
}

func (g *GraphifyIndexer) release(abs string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.inFlight, abs)
}

// build runs a single bounded `graphify update`.
func (g *GraphifyIndexer) build(abs string) error {
	// graphify writes its output INSIDE the repo (graphify-out/, ~26 MB on this
	// codebase). Hydra commits and pushes worker changes, so without an ignore
	// rule the graph would be committed into whatever repo it indexed — user
	// projects included, which do not carry our .gitignore. Written before the
	// build so a partial or failed run is covered too.
	ensureGitignoreLine(filepath.Join(abs, ".gitignore"), "graphify-out/")

	ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
	defer cancel()

	args := []string{"update", abs}
	if g.skipCluster {
		args = append(args, "--no-cluster")
	}

	cmd := exec.CommandContext(ctx, g.binary, args...)
	cmd.Dir = abs

	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timed out after %s (graph too large? see graphify issue #341)", g.timeout)
	}
	if err != nil {
		return fmt.Errorf("%w: %s", err, tailOutput(out, 300))
	}
	return nil
}

// LastBuilt reports when a project's graph was last refreshed by this indexer.
func (g *GraphifyIndexer) LastBuilt(projectPath string) (time.Time, bool) {
	if g == nil {
		return time.Time{}, false
	}
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		abs = projectPath
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.lastBuilt[abs]
	return t, ok
}

func tailOutput(b []byte, max int) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= max {
		return s
	}
	return "…" + s[len(s)-max:]
}
