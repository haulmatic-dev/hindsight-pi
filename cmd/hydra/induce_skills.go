package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/haulmatic-dev/hydra/internal/core"
	"github.com/haulmatic-dev/hydra/internal/skillinducer"
)

// runInduceSkillsSubcommand implements `hydra induce-skills`: a one-shot
// pass over the worker execution records, inducing candidate skills into
// the staging directory. Intentionally NOT gated on
// intelligence.skill_induction.enabled — a manual invocation is explicit
// operator intent; the daemon background loop is the gated path.
//
// Output is staging-only ($HYDRA_HOME/skill-candidates): promotion into the
// shared Pi config stays a human-reviewed PR.
func runInduceSkillsSubcommand(configPath string) {
	config, err := core.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	cfg := config.Intelligence.SkillInduction
	cfg.SkillsDir = resolveSkillsDir(config, cfg.SkillsDir)

	ind, err := skillinducer.New(cfg, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "skill inducer unavailable: %v\n", err)
		fmt.Fprintf(os.Stderr, "hint: set intelligence.skill_induction.model (and api key env) in %s\n", configPath)
		os.Exit(1)
	}

	start := time.Now()
	stats, err := ind.RunOnce(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "induce-skills: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("induce-skills: %s (elapsed %s)\n", stats, time.Since(start).Round(time.Millisecond))
	fmt.Printf("candidates staged in: %s\n", ind.CandidatesDir())
	if stats.Induced > 0 {
		fmt.Println("review candidates, then promote via PR to the shared skill library — never copy them in place")
	}
}

// resolveSkillsDir defaults the dedup library to {pi.config_dir}/skills —
// the shared, read-only skill library the worker fleet already loads.
func resolveSkillsDir(config *core.Config, configured string) string {
	if configured != "" {
		return configured
	}
	if config.Pi.ConfigDir != "" {
		return filepath.Join(config.Pi.ConfigDir, "skills")
	}
	return ""
}

// startSkillInductionLoop launches the daemon background loop when
// intelligence.skill_induction.enabled is true. Fail-open: a construction
// error (missing API key, empty model) logs and disables the loop — it must
// never block daemon startup.
func startSkillInductionLoop(ctx context.Context, config *core.Config) {
	if !config.Intelligence.SkillInduction.Enabled {
		return
	}
	cfg := config.Intelligence.SkillInduction
	cfg.SkillsDir = resolveSkillsDir(config, cfg.SkillsDir)
	cfg = cfg.ApplyDefaults()
	ind, err := skillinducer.New(cfg, nil)
	if err != nil {
		fmt.Printf("⚠️  Skill induction disabled: %v\n", err)
		return
	}
	fmt.Printf("🧠 Skill induction enabled (every %s, staging in %s)\n", cfg.Interval, ind.CandidatesDir())
	go ind.RunLoop(ctx)
}
