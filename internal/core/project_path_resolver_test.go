package core

import (
	"testing"
)

// resolveProjectPathByName must resolve beads whose project was registered
// under a different name than the bead ID prefix carries. Multi-user
// discovery registers projects as "<user>/<repo>" (e.g. "ajantha/webapp"),
// while bead IDs carry only the repo name ("webapp-0fc" -> project "webapp").
// Pan's abandon/close_satisfied paths failed 100% with "project path not
// found" for every multi-discovered project (2026-09-11/13).
func TestResolveProjectPathByNameBasenameFallback(t *testing.T) {
	cfg := &Config{
		Projects: []ProjectConfig{
			{Name: "ajantha/webapp", Path: "/home/ajantha/projects/webapp", Type: "single"},
		},
	}
	if got := resolveProjectPathByName(cfg, "webapp", ""); got != "/home/ajantha/projects/webapp" {
		t.Errorf("basename fallback = %q, want /home/ajantha/projects/webapp", got)
	}
}

func TestResolveProjectPathByNameMultiRepoFallback(t *testing.T) {
	cfg := &Config{
		Projects: []ProjectConfig{
			{
				Name: "ajantha/projects",
				Path: "/home/ajantha/projects",
				Type: "multi",
				Repos: []RepoConfig{
					{Name: "webapp"},
					{Name: "srv-smart-engine"},
				},
			},
		},
	}
	if got := resolveProjectPathByName(cfg, "webapp", ""); got != "/home/ajantha/projects/webapp" {
		t.Errorf("multi repo fallback = %q, want /home/ajantha/projects/webapp", got)
	}
}

func TestResolveProjectPathByNameExactMatchWins(t *testing.T) {
	cfg := &Config{
		Projects: []ProjectConfig{
			{Name: "ajantha/webapp", Path: "/wrong/path/webapp", Type: "single"},
			{Name: "webapp", Path: "/explicit/path/webapp", Type: "single"},
		},
	}
	if got := resolveProjectPathByName(cfg, "webapp", ""); got != "/explicit/path/webapp" {
		t.Errorf("exact match = %q, want /explicit/path/webapp", got)
	}
}

func TestResolveProjectPathByNameUnknownStaysEmpty(t *testing.T) {
	cfg := &Config{
		Projects: []ProjectConfig{
			{Name: "hydra", Path: "/opt/dev-tools/components/hydra", Type: "single"},
		},
	}
	if got := resolveProjectPathByName(cfg, "webapp", ""); got != "" {
		t.Errorf("unknown project = %q, want empty", got)
	}
}
