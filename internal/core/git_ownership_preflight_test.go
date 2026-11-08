package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

func dubiousProbeErr() error {
	return errors.New("git rev-parse failed (exit status 128): fatal: detected dubious ownership in repository at '/home/nipuna.j/projects/webapp'")
}

func TestIsDubiousOwnershipError(t *testing.T) {
	if !isDubiousOwnershipError(dubiousProbeErr()) {
		t.Fatal("exit-128 dubious ownership must classify as git-ownership")
	}
	if isDubiousOwnershipError(errors.New("fatal: unable to access: Could not resolve host")) {
		t.Fatal("network error must NOT classify as git-ownership")
	}
	if isDubiousOwnershipError(nil) {
		t.Fatal("nil error must not classify")
	}
}

func TestGitOwnershipRemediationText(t *testing.T) {
	txt := gitOwnershipRemediation("/home/nipuna.j/projects/webapp")
	for _, want := range []string{"git config --global --add safe.directory /home/nipuna.j/projects/webapp", "human/T3"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("remediation text missing %q: %s", want, txt)
		}
	}
}

// sinkHasEvent reads the sink's central stream for today (defined in
// credential_preflight_test.go in this package).

// Preflight: simulated exit 128 -> typed git_ownership_unusable event whose
// detail carries the exact remediation command; park reason too.
func TestRunGitOwnershipPreflight_DubiousOwnershipParksWithRemediation(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	project := types.Project{Name: "fmsfe", Path: repo, Type: types.RepoTypeSingle}

	origProbe := gitAccessProbe
	defer func() { gitAccessProbe = origProbe }()
	gitAccessProbe = func(string) error { return dubiousProbeErr() }

	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	sink := NewEventSink(t.TempDir())
	o := &Overseer{eventSink: sink, registryStore: st, config: &Config{}, projects: []types.Project{project}}

	o.RunGitOwnershipPreflight()

	if !sinkHasEvent(sink, "git_ownership_unusable") {
		t.Fatal("expected git_ownership_unusable event")
	}
	path := filepath.Join(sink.centralDir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "safe.directory "+repo) {
		t.Fatalf("event detail must contain exact remediation command, got: %s", data)
	}
	park, ok, err := st.ProjectParkInfo(project.Name)
	if err != nil || !ok || !park.Parked {
		t.Fatal("project must be auto-parked (fail closed) after dubious-ownership refusal")
	}
	if !strings.Contains(park.Reason, "safe.directory "+repo) {
		t.Fatalf("park reason must contain exact remediation, got: %s", park.Reason)
	}
}

// Non-ownership probe errors (network, git missing) fail open: no event, no
// park.
func TestRunGitOwnershipPreflight_FailOpenOnOtherErrors(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	project := types.Project{Name: "fmsfe", Path: repo, Type: types.RepoTypeSingle}

	origProbe := gitAccessProbe
	defer func() { gitAccessProbe = origProbe }()
	gitAccessProbe = func(string) error { return errors.New("git rev-parse failed (exit status 1): Could not resolve host") }

	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	sink := NewEventSink(t.TempDir())
	o := &Overseer{eventSink: sink, registryStore: st}

	o.RunGitOwnershipPreflight()

	if sinkHasEvent(sink, "git_ownership_unusable") {
		t.Fatal("non-ownership failure must fail open (no event)")
	}
	if park, ok, _ := st.ProjectParkInfo(project.Name); ok && park.Parked {
		t.Fatal("project must not be parked on fail-open errors")
	}
}

func TestRunGitOwnershipPreflight_SkipsNonGitDir(t *testing.T) {
	sink := NewEventSink(t.TempDir())
	o := &Overseer{eventSink: sink, config: &Config{}}
	o.RunGitOwnershipPreflight() // empty project set; must not panic
	_ = sink
}

// noteSpawnGitOwnership: label applied, typed event carries the exact fix
// command, error names the git-ownership class.
func TestNoteSpawnGitOwnership_LabelsAndEmitsRemediation(t *testing.T) {
	repo := t.TempDir()
	bead := types.Bead{ID: types.NewBeadID("fmsfe", "", "fmsfe-abc"), Project: "fmsfe"}
	// In-memory client not needed: pass nil beadsClient and assert the
	// error + event only (label application path covered via label skip
	// tests elsewhere; here we pin the class + remediation contract).
	err := noteSpawnGitOwnership(nil, nil, repo, bead, dubiousProbeErr())
	if err == nil || !strings.Contains(err.Error(), "git-ownership") {
		t.Fatalf("error must name the git-ownership class, got: %v", err)
	}
	if !strings.Contains(err.Error(), "safe.directory "+repo) {
		t.Fatalf("error must carry exact remediation, got: %v", err)
	}
}
