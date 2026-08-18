package main

import (
	"fmt"
	"os"

	"github.com/haulmatic-dev/hydra/internal/core"
)

// runGatesSubcommand implements `hydra gates verify <project>`: the bootstrap
// step of the daemon gate runner (PRD-A FR-002). It executes each declared
// gate once on the REAL checkout (AllowMainCheckout) and reports parse
// status, so `daemon_owned: true` is never enabled against an unverified
// toolchain. Fail-closed: any gate that errors, fails, or misparses exits 1.
func runGatesSubcommand(configPath string, args []string) {
	if len(args) < 2 || args[0] != "verify" {
		fmt.Fprintln(os.Stderr, "usage: hydra gates verify <project-name>")
		os.Exit(2)
	}
	projectName := args[1]

	config, err := core.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	var proj *core.ProjectConfig
	for i := range config.Projects {
		if config.Projects[i].Name == projectName {
			proj = &config.Projects[i]
			break
		}
	}
	if proj == nil {
		fmt.Fprintf(os.Stderr, "project %q not found in %s\n", projectName, configPath)
		os.Exit(1)
	}
	if proj.Gates == nil {
		fmt.Fprintf(os.Stderr, "project %q declares no gates block\n", projectName)
		os.Exit(1)
	}

	root := os.Getenv("HYDRA_HOME")
	if root == "" {
		root = os.Getenv("HOME")
	}
	runner := core.NewGateRunner(root)

	named := []struct {
		name string
		spec core.GateSpec
	}{
		{"test", proj.Gates.Test},
		{"lint", proj.Gates.Lint},
		{"build", proj.Gates.Build},
	}

	failed := false
	for _, g := range named {
		if g.spec.Cmd == "" {
			continue
		}
		a, err := runner.RunGate(core.GateRequest{
			Project: proj.Name, BeadID: "bootstrap-verify", Gate: g.name,
			Spec:              g.spec,
			ProjectPath:       proj.Path,
			WorktreePath:      proj.Path,
			AllowMainCheckout: true,
		})
		if err != nil {
			fmt.Printf("  %-6s ERROR: %v\n", g.name, err)
			failed = true
			continue
		}
		fmt.Printf("  %-6s %s (exit %d)\n%s\n", g.name, a.Status, a.ExitCode, indent(a.Summary))
		if a.Status != core.GateStatusPassed {
			failed = true
		}
	}

	if failed {
		fmt.Println("gates verify: FAILED — do not enable daemon_owned until all gates pass")
		os.Exit(1)
	}
	fmt.Println("gates verify: all declared gates passed and parsed")
}

func indent(s string) string {
	out := ""
	for _, line := range splitLines(s) {
		out += "         " + line + "\n"
	}
	return out
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
