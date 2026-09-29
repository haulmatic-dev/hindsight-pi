package main

import (
	"log"
	"path/filepath"
	"time"

	"github.com/haulmatic-dev/hydra/internal/core"
	"github.com/haulmatic-dev/hydra/internal/triage"
)

// runTriageSubcommand runs ONE PR-triage pass over all configured projects
// and exits (hydra-muvh / FR-030). Uses the same config gate as the daemon:
// triage.enabled must be true, otherwise it reports and exits 0 without
// touching any repo. Comments are rate-limited by the same shared state file
// the daemon uses, so a manual run never double-posts within 24h.
func runTriageSubcommand(configPath string) {
	config, err := core.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("triage: load config: %v", err)
	}
	if !config.Triage.Enabled {
		log.Println("triage: disabled in config (triage.enabled: true to enable)")
		return
	}
	judge := triage.JudgeConfig{
		Provider:   config.Triage.Provider,
		Model:      config.Triage.Model,
		Thinking:   config.Triage.Thinking,
		APIKeyName: config.Triage.APIKeyName,
	}
	interval := time.Duration(config.Triage.IntervalSeconds) * time.Second
	_ = interval // one-shot; interval only matters for the daemon loop
	svc := triage.New(time.Hour, judge, func() []triage.Project {
		projects := make([]triage.Project, 0, len(config.Projects))
		for _, p := range config.Projects {
			if p.Type == "multi" {
				for _, r := range p.Repos {
					path := r.Path
					if path == "" {
						path = r.Name
					}
					if !filepath.IsAbs(path) {
						path = filepath.Join(p.Path, path)
					}
					projects = append(projects, triage.Project{Name: p.Name + "/" + r.Name, Path: path})
				}
				continue
			}
			projects = append(projects, triage.Project{Name: p.Name, Path: p.Path})
		}
		return projects
	}, core.TriageStatePath())
	log.Printf("triage: running one pass over %d configured project(s)", len(config.Projects))
	svc.RunOnce()
	log.Println("triage: pass complete")
}
