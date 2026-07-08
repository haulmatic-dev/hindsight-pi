package beads

import "testing"

// TestRepoCwd verifies the working-directory resolution: multi-repo projects
// descend into the repo subdir; single projects (empty repoName) are unchanged.
func TestRepoCwd(t *testing.T) {
	if got := repoCwd("/proj", ""); got != "/proj" {
		t.Errorf("empty repoName: got %q, want /proj", got)
	}
	if got := repoCwd("/proj", "svc"); got != "/proj/svc" {
		t.Errorf("with repoName: got %q, want /proj/svc", got)
	}
}

// TestListBeads_DescendsIntoRepoSubdir verifies ListBeads runs br in the repo
// subdirectory when a repoName is given (the multi-repo bug: br was run in the
// project root, which has no .beads, and failed).
func TestListBeads_DescendsIntoRepoSubdir(t *testing.T) {
	m := NewMockExecutor()
	m.AddResponse("br", []string{"list", "--json", "--limit", "500", "--offset", "0"}, []byte(`{"issues":[]}`), nil)
	c := NewClientWithExecutor(m)

	if _, err := c.ListBeads("/home/u/projects/monorepo", "svc-a"); err != nil {
		t.Fatalf("ListBeads: %v", err)
	}
	if len(m.dirs) == 0 {
		t.Fatal("expected br to run in a directory")
	}
	if got := m.dirs[0]; got != "/home/u/projects/monorepo/svc-a" {
		t.Errorf("br cwd: got %q, want /home/u/projects/monorepo/svc-a", got)
	}
}

// TestListBeads_SingleProjectUnchanged verifies the single-project path still
// runs br in the project root (no behavior change for the common case).
func TestListBeads_SingleProjectUnchanged(t *testing.T) {
	m := NewMockExecutor()
	m.AddResponse("br", []string{"list", "--json", "--limit", "500", "--offset", "0"}, []byte(`{"issues":[]}`), nil)
	c := NewClientWithExecutor(m)

	if _, err := c.ListBeads("/home/u/projects/single", ""); err != nil {
		t.Fatalf("ListBeads: %v", err)
	}
	if got := m.dirs[0]; got != "/home/u/projects/single" {
		t.Errorf("br cwd: got %q, want /home/u/projects/single", got)
	}
}
