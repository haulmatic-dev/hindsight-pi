package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/haulmatic-dev/hydra/internal/pan"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// BuildCloseSatisfied returns a handler that closes a bead Pan has judged to
// already be satisfied on the working branch. It runs the same scoped gate probe
// as the pre-spawn path, but on the main checkout: the claim is "already on
// branch", so main is the evidence.
//
// A satisfied close must never trust a green test suite when the bead's own
// description names a measurable acceptance command (coverage with thresholds,
// or a coverageThreshold/ratchet flip). In that case the full acceptance
// command is executed and must exit 0. If the description mentions
// coverageThreshold/ratchet/flip but the durable config lacks the corresponding
// entry, the close is forbidden. Otherwise, if only a test suite is described,
// the handler abstains so Pan continues the bead instead of closing it.
func BuildCloseSatisfied(o *Overseer) func(beadID string) error {
	return func(beadID string) error {
		id := types.BeadID(beadID)
		bead, projectPath, repoName, err := resolveSatisfiedBead(o, id)
		if err != nil {
			return err
		}
		if bead.Status.IsTerminal() {
			log.Printf("[Pan] %s already terminal (%s); skipping close_satisfied", id, bead.Status)
			return nil
		}

		projectName := beadProject(bead)
		gates := gatesForProject(o.config, projectName)
		if gates == nil || !gates.DaemonOwned {
			return fmt.Errorf("close_satisfied %s: project %s has no daemon_owned gates (fail closed)", id, projectName)
		}

		if o.beadCloser.gateRunner == nil {
			return fmt.Errorf("close_satisfied %s: no gate runner attached (fail closed)", id)
		}

		// Cheap deterministic guard: a ratchet/flip mention requires the durable
		// artifact to be present before any command runs.
		if hasRatchetMention(bead.Description) {
			cfgPath := coverageConfigPath(projectPath, bead.Description)
			if _, statErr := os.Stat(cfgPath); statErr != nil || !configHasCoverageThreshold(cfgPath, bead.Description) {
				return fmt.Errorf("close_satisfied %s: forbidden — coverageThreshold/ratchet/flip mentioned but %s lacks the corresponding entry", id, cfgPath)
			}
		}

		// If the bead description names a measurable acceptance command, run it
		// and require exit 0 before closing.
		if cmd, ok := extractAcceptanceCommand(bead.Description); ok {
			a, err := o.beadCloser.gateRunner.RunGate(GateRequest{
				Project: projectName, BeadID: string(id), Gate: "satisfied-acceptance",
				Spec:              GateSpec{Cmd: cmd, Parser: "exit-code"},
				ProjectPath:       projectPath,
				WorktreePath:      projectPath,
				AllowMainCheckout: true,
			})
			if err != nil {
				return fmt.Errorf("close_satisfied %s acceptance runner fault: %w", id, err)
			}
			if a.Status != GateStatusPassed {
				return fmt.Errorf("close_satisfied %s acceptance command red (exit %d): %s", id, a.ExitCode, a.Summary)
			}
			log.Printf("[Pan] %s satisfied acceptance command passed", id)
			return finalizeSatisfiedClose(o, bead, projectPath, repoName, id)
		}

		// Measurable acceptance is referenced but no command is named: abstain
		// so Pan continues the bead instead of closing on a green test suite.
		if hasCoverageMention(bead.Description) {
			return pan.ErrSatisfiedAbstain
		}

		// Legacy path: scoped test gates only.
		targets := beadTargetFiles(bead)
		if len(targets) == 0 {
			return fmt.Errorf("close_satisfied %s: no target files to scope gates (fail closed)", id)
		}
		scope := targets[0]

		named := []struct {
			name string
			spec GateSpec
		}{
			{"lint", gates.Lint},
			{"build", gates.Build},
			{"test", gates.Test},
		}
		probed := false
		for _, g := range named {
			if g.spec.Cmd == "" || !strings.Contains(g.spec.Cmd, "<scope>") {
				continue
			}
			probed = true
			a, err := o.beadCloser.gateRunner.RunGate(GateRequest{
				Project: projectName, BeadID: string(id), Gate: "satisfied-" + g.name,
				Spec:              g.spec,
				ProjectPath:       projectPath,
				WorktreePath:      projectPath,
				AllowMainCheckout: true,
				Scope:             scope,
			})
			if err != nil {
				return fmt.Errorf("close_satisfied %s gate %s runner fault: %w", id, g.name, err)
			}
			if a.Status != GateStatusPassed {
				return fmt.Errorf("close_satisfied %s gate %s red (exit %d): %s", id, g.name, a.ExitCode, a.Summary)
			}
			log.Printf("[Pan] %s satisfied gate %s passed", id, g.name)
		}
		if !probed {
			return fmt.Errorf("close_satisfied %s: no scoped daemon_owned gates (fail closed)", id)
		}

		return finalizeSatisfiedClose(o, bead, projectPath, repoName, id)
	}
}

var (
	// ratchetMentionPattern catches explicit ratchet/flip/coverageThreshold
	// references that require a durable config artifact.
	ratchetMentionPattern = regexp.MustCompile(`(?i)(coverageThreshold|ratchet|flip)`)
	// coverageMentionPattern catches broader coverage-threshold requirements that
	// mean a green test suite is not enough.
	coverageMentionPattern = regexp.MustCompile(`(?i)(coverage\s*threshold|threshold\s*coverage|coverage\s*ratio|functions coverage|lines coverage|statements coverage|branches coverage|--collectCoverageFrom)`)
	// acceptanceCmdPattern identifies a description line that names a measurable
	// acceptance command.
	acceptanceCmdPattern = regexp.MustCompile(`(?i)(--collectCoverageFrom|coverageThreshold)`)
	// configFilePattern finds an explicit config file reference.
	configFilePattern = regexp.MustCompile(`(?i)(package\.json|jest\.config\.\w+)`)
	// modulePathPattern extracts a src/modules/... glob or path for targeted
	// ratchet lookups.
	modulePathPattern = regexp.MustCompile(`src/modules/[^\s"'\x60]+`)
)

func hasRatchetMention(s string) bool  { return ratchetMentionPattern.MatchString(s) }
func hasCoverageMention(s string) bool { return coverageMentionPattern.MatchString(s) }

// coverageConfigPath returns the config file to inspect for a ratchet entry.
// It defaults to package.json but honors an explicit reference in the bead text.
func coverageConfigPath(projectPath, desc string) string {
	if m := configFilePattern.FindString(desc); m != "" {
		return filepath.Join(projectPath, m)
	}
	return filepath.Join(projectPath, "package.json")
}

// configHasCoverageThreshold reports whether the named config contains a
// coverageThreshold entry that matches the bead description. When the
// description names a module path, the path must appear in the config;
// otherwise any coverageThreshold entry is sufficient.
func configHasCoverageThreshold(path, desc string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	content := string(data)
	if m := modulePathPattern.FindString(desc); m != "" {
		return strings.Contains(content, m)
	}
	return strings.Contains(content, "coverageThreshold")
}

// extractAcceptanceCommand pulls the first description command that names a
// measurable acceptance command. It strips markdown code fences/backticks and
// leading shell prompts.
func extractAcceptanceCommand(desc string) (string, bool) {
	for _, raw := range strings.Split(desc, "\n") {
		line := strings.TrimSpace(raw)

		// Code-fence or inline backtick content takes precedence over prose.
		if cmd, ok := extractBacktickCommand(line); ok && acceptanceCmdPattern.MatchString(cmd) {
			if looksLikeCommand(cmd) {
				return cleanCommand(cmd), true
			}
		}

		if !acceptanceCmdPattern.MatchString(line) {
			continue
		}
		if !looksLikeCommand(line) {
			continue
		}
		return cleanCommand(line), true
	}
	return "", false
}

func looksLikeCommand(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "npm ") || strings.Contains(lower, "yarn ") ||
		strings.Contains(lower, "pnpm ") || strings.Contains(lower, "npx ") ||
		strings.Contains(lower, "jest ") || strings.Contains(lower, "sh ") ||
		strings.Contains(lower, "bash ") || strings.Contains(lower, "exit ")
}

func extractBacktickCommand(line string) (string, bool) {
	start := strings.Index(line, "`")
	if start == -1 {
		return "", false
	}
	end := strings.LastIndex(line[start+1:], "`")
	if end == -1 {
		return "", false
	}
	return line[start+1 : start+1+end], true
}

func cleanCommand(line string) string {
	cmd := strings.TrimPrefix(line, "$")
	cmd = strings.TrimSpace(cmd)
	return cmd
}

// finalizeSatisfiedClose closes the bead, updates the DAG when possible, and
// emits the satisfied-closed label/event. A DAG node that is already failed is
// skipped for MarkCompleted so the close completes cleanly.
func finalizeSatisfiedClose(o *Overseer, bead *types.Bead, projectPath, repoName string, id types.BeadID) error {
	if err := o.beadsClient.CloseBead(projectPath, repoName, id, "already-satisfied (Pan)"); err != nil {
		return fmt.Errorf("close_satisfied %s CloseBead failed: %w", id, err)
	}

	o.dagMu.Lock()
	nodeStatus, nodeErr := o.dag.GetNodeStatus(id.String())
	if nodeErr == nil && nodeStatus == types.StatusFailed {
		log.Printf("[Pan] %s DAG node is failed; skipping MarkCompleted", id)
	} else {
		newlyReady, markErr := o.dag.MarkCompleted(id.String())
		if markErr != nil {
			log.Printf("[Pan] %s closed but MarkCompleted failed: %v", id, markErr)
		} else {
			log.Printf("[Pan] %d dependents now ready after %s close_satisfied", len(newlyReady), id)
		}
	}
	o.dagMu.Unlock()

	if o.completed != nil {
		o.completed[id.String()] = true
	}

	if err := o.beadsClient.UpdateBeadLabels(projectPath, repoName, id, []string{"closed-satisfied"}); err != nil {
		log.Printf("[Pan] %s label update failed (fail open): %v", id, err)
	}

	if o.beadCloser != nil && o.beadCloser.events != nil {
		o.beadCloser.events.EmitRepo(projectPath, Event{Type: "satisfied_closed", Project: repoName,
			Bead: id.String(), Reason: "already-satisfied",
			Fields: map[string]any{"tokens": 0}})
	}
	return nil
}

// resolveSatisfiedBead locates the bead and its project path, using the ID
// prefix when the bead's Project field is empty (scheduler-path beads).
func resolveSatisfiedBead(o *Overseer, id types.BeadID) (*types.Bead, string, string, error) {
	repoName := id.Repo()
	projectPath := resolveProjectPathByName(o.config, id.Project(), repoName)
	if projectPath == "" {
		return nil, "", "", fmt.Errorf("close_satisfied %s: project path not found", id)
	}

	bead, err := o.beadsClient.GetBead(projectPath, repoName, id)
	if err != nil {
		return nil, "", "", fmt.Errorf("close_satisfied %s GetBead failed: %w", id, err)
	}
	return bead, projectPath, repoName, nil
}

// resolveProjectPathByName finds the configured path for a project, respecting
// multi-repo layout. Falls back to empty string when unknown.
func resolveProjectPathByName(cfg *Config, projectName, repoName string) string {
	if cfg == nil {
		return ""
	}
	for _, p := range cfg.Projects {
		if p.Name != projectName {
			continue
		}
		if p.Type == "multi" && repoName != "" {
			for _, r := range p.Repos {
				if r.Name == repoName {
					return r.Path
				}
			}
			return filepath.Join(p.Path, repoName)
		}
		return p.Path
	}
	// Fallback: multi-user discovery registers projects as "<user>/<repo>"
	// (e.g. "ajantha/webapp") while bead IDs carry only the repo name
	// ("webapp-0fc" -> project "webapp"). Pan's close paths failed 100% with
	// "project path not found" for every such project (2026-09-11/13). Match by
	// repo name inside a multi project, then by path basename. Exact matches
	// above always win over these fallbacks.
	for _, p := range cfg.Projects {
		if p.Type == "multi" {
			for _, r := range p.Repos {
				if r.Name == projectName || (repoName != "" && r.Name == repoName) {
					if r.Path != "" {
						return r.Path
					}
					return filepath.Join(p.Path, r.Name)
				}
			}
		}
		if filepath.Base(p.Path) == projectName {
			return p.Path
		}
	}
	return ""
}

// gatesForProject returns the gate set for a project name, or nil.
func gatesForProject(cfg *Config, projectName string) *GateSet {
	if cfg == nil {
		return nil
	}
	for i := range cfg.Projects {
		if cfg.Projects[i].Name == projectName && cfg.Projects[i].Gates != nil {
			return cfg.Projects[i].Gates
		}
	}
	return nil
}
