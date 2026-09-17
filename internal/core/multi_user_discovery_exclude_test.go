package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMultiUserDiscovery_ExcludeProject(t *testing.T) {
	dir := t.TempDir()

	projectPath := filepath.Join(dir, "projects", "demo")
	os.MkdirAll(filepath.Join(projectPath, ".beads"), 0755)
	os.WriteFile(filepath.Join(projectPath, ".beads", "issues.jsonl"), []byte("{}"), 0644)

	ctx := &UserContext{Username: "alice", HomeDir: dir}
	d := NewMultiUserDiscovery()
	d.SetExclude([]string{"demo"})

	project := d.convertProject(UserProject{
		Name: "demo",
		Path: projectPath,
		Type: "single",
	}, ctx)

	if project != nil {
		t.Fatalf("excluded project demo was converted")
	}
}

func TestMultiUserDiscovery_ExcludeRepoInMultiProject(t *testing.T) {
	dir := t.TempDir()

	projectPath := filepath.Join(dir, "my-platform")
	os.MkdirAll(filepath.Join(projectPath, "frontend", ".beads"), 0755)
	os.MkdirAll(filepath.Join(projectPath, "backend", ".beads"), 0755)
	os.MkdirAll(filepath.Join(projectPath, "demo", ".beads"), 0755)
	os.WriteFile(filepath.Join(projectPath, "frontend", ".beads", "issues.jsonl"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(projectPath, "backend", ".beads", "issues.jsonl"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(projectPath, "demo", ".beads", "issues.jsonl"), []byte("{}"), 0644)

	ctx := &UserContext{Username: "alice", HomeDir: dir}
	d := NewMultiUserDiscovery()
	d.SetExclude([]string{"demo"})

	project := d.convertProject(UserProject{
		Name: "my-platform",
		Path: projectPath,
		Type: "multi",
	}, ctx)

	if project == nil {
		t.Fatal("project conversion returned nil")
	}
	if len(project.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d: %+v", len(project.Repos), project.Repos)
	}
	for _, r := range project.Repos {
		if r.Name == "demo" {
			t.Errorf("excluded repo demo was discovered")
		}
	}
}

func TestMultiUserDiscovery_ExcludeByPath(t *testing.T) {
	dir := t.TempDir()

	projectPath := filepath.Join(dir, "projects", "demo")
	os.MkdirAll(filepath.Join(projectPath, ".beads"), 0755)
	os.WriteFile(filepath.Join(projectPath, ".beads", "issues.jsonl"), []byte("{}"), 0644)

	ctx := &UserContext{Username: "alice", HomeDir: dir}
	d := NewMultiUserDiscovery()
	d.SetExclude([]string{projectPath})

	project := d.convertProject(UserProject{
		Name: "demo",
		Path: projectPath,
		Type: "single",
	}, ctx)

	if project != nil {
		t.Fatalf("excluded project by path was converted")
	}
}
