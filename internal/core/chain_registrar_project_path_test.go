package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Regression (2026-09-16 webapp zpf incident): two projects share the name
// "webapp". Chain completion must integrate against the path recorded at
// registration — the root bead's own project — never the first name match,
// which pointed at another user's project where the chain branch did not
// exist and Integrate silently no-opped.
func TestChainRegistrarRecordsRootProjectPath(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "ajantha-webapp")
	second := filepath.Join(dir, "nipuna-webapp")
	for _, p := range []string{first, second} {
		if err := os.MkdirAll(filepath.Join(p, ".beads"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// First project (the name-collision winner): an unrelated single bead.
	writeIssuesFile(t, first, `{"id":"webapp-aaa","title":"unrelated"}`)
	// Second project: a two-bead chain rooted at webapp-root.
	writeIssuesFile(t, second,
		`{"id":"webapp-root","title":"root"}`,
		`{"id":"webapp-leaf","title":"leaf","dependencies":[{"issue_id":"webapp-leaf","depends_on_id":"webapp-root"}]}`)

	projects := []types.Project{
		{Name: "webapp", Path: first},
		{Name: "webapp", Path: second},
	}
	reader := NewBeadFileReader(projects)
	detector := NewChainCompletionDetector(ChainCompletionConfig{}, &Overseer{})
	registrar := NewChainRegistrar(detector, reader)
	registrar.RegisterFromProjects(projects)

	info, ok := detector.GetChainInfo(types.BeadID("webapp-root"))
	if !ok {
		t.Fatal("chain webapp-root not registered")
	}
	if info.ProjectPath != second {
		t.Errorf("ChainInfo.ProjectPath = %q, want %q (root bead's own project, not the first name match %q)",
			info.ProjectPath, second, first)
	}
}

func writeIssuesFile(t *testing.T, projectPath string, lines ...string) {
	t.Helper()
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(projectPath, ".beads", "issues.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
