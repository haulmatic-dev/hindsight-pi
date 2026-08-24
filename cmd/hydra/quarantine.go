package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/haulmatic-dev/hydra/internal/core"
)

// runDrainQuarantineSubcommand implements `hydra drain-quarantine [--apply]`.
// It is deterministic: no LLM judgment. By default it dry-runs; pass --apply
// to actually delete branches.
func runDrainQuarantineSubcommand(configPath string, args []string) {
	fs := flag.NewFlagSet("drain-quarantine", flag.ExitOnError)
	apply := fs.Bool("apply", false, "actually delete branches (default is dry-run)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	config, err := core.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	var projectName string
	if fs.NArg() > 0 {
		projectName = fs.Arg(0)
	}

	dryRun := !*apply
	anyProject := false
	exitCode := 0
	for i := range config.Projects {
		proj := &config.Projects[i]
		if projectName != "" && proj.Name != projectName {
			continue
		}
		anyProject = true

		deleted, kept, err := core.DrainQuarantineBranches(proj.Path, dryRun)
		if err != nil {
			log.Printf("[drain-quarantine] project=%s error: %v", proj.Name, err)
			exitCode = 1
			continue
		}
		fmt.Printf("project=%s deleted=%d kept=%d (dry-run=%v)\n", proj.Name, len(deleted), kept, dryRun)
	}

	if projectName != "" && !anyProject {
		fmt.Fprintf(os.Stderr, "project %q not found in %s\n", projectName, configPath)
		os.Exit(1)
	}

	os.Exit(exitCode)
}
