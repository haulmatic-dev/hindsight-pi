package core

import (
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// TestPublishOneProject_MultiRepoSkipsParent: a multi project's publish loop
// must run PublishRepo per repo dir, never on the parent (not a git repo).
// Non-git dirs make PublishRepo return benign "no branch", so the aggregate
// clears push-pending and reports nothing pending.
func TestPublishOneProject_MultiRepoSkipsParent(t *testing.T) {
	parent := t.TempDir()
	o := &Overseer{}
	o.pushPending = make(map[string]*PushPendingEntry)
	o.config = &Config{}
	o.projects = []types.Project{{
		Name: "fms",
		Path: parent,
		Type: types.RepoTypeMulti,
		Repos: []types.Repo{
			{Name: "fms-frontend", Path: "fms-frontend"},
			{Name: "fms-backend", Path: "fms-backend"},
		},
	}}
	// Identity resolution fail-closed logs but does not block the loop.
	pushed, reason := o.publishOneProject(o.projects[0], nil)
	if pushed {
		t.Fatal("non-git repos must not report pushed")
	}
	if !benignPublishReasons[reason] && reason != "" {
		t.Fatalf("reason = %q, want benign/empty", reason)
	}
	if _, ok := o.pushPending[parent]; ok {
		t.Fatal("benign repos must not register push-pending on the parent")
	}
	_ = filepath.Join // keep filepath import if unused
}
