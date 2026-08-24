package core

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// BuildCloseSatisfied returns a handler that closes a bead Pan has judged to
// already be satisfied on the working branch. It runs the same scoped gate probe
// as the pre-spawn path, but on the main checkout: the claim is "already on
// branch", so main is the evidence. Fail-closed on missing gates or any red
// gate; label/event emission stays fail-open.
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

		targets := beadTargetFiles(bead)
		if len(targets) == 0 {
			return fmt.Errorf("close_satisfied %s: no target files to scope gates (fail closed)", id)
		}
		scope := targets[0]

		if o.beadCloser.gateRunner == nil {
			return fmt.Errorf("close_satisfied %s: no gate runner attached (fail closed)", id)
		}

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

		if err := o.beadsClient.CloseBead(projectPath, repoName, id, "already-satisfied (Pan)"); err != nil {
			return fmt.Errorf("close_satisfied %s CloseBead failed: %w", id, err)
		}

		o.dagMu.Lock()
		newlyReady, markErr := o.dag.MarkCompleted(id.String())
		o.dagMu.Unlock()
		if markErr != nil {
			log.Printf("[Pan] %s closed but MarkCompleted failed: %v", id, markErr)
		} else {
			log.Printf("[Pan] %d dependents now ready after %s close_satisfied", len(newlyReady), id)
		}

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
