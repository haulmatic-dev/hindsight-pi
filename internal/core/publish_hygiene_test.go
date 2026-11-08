package core

import "testing"

// hydra-tvfd: dogfood/ is reserved for daemon-owned repos (hydra
// self-improvement); user-owned product repos get hydra/.
func TestIntegrationBranchPrefix(t *testing.T) {
	t.Cleanup(func() { daemonOwnedChecker = nil })

	// Nil checker (tests, standalone tools): legacy dogfood/ default.
	if got := IntegrationBranchPrefix("/any/path"); got != "dogfood/" {
		t.Fatalf("nil checker: got %q, want dogfood/", got)
	}

	SetDaemonOwnedChecker(func(p string) bool { return p == "/daemon/repo" })
	if got := IntegrationBranchPrefix("/daemon/repo"); got != "dogfood/" {
		t.Fatalf("daemon-owned: got %q, want dogfood/", got)
	}
	if got := IntegrationBranchPrefix("/home/user/projects/product"); got != "hydra/" {
		t.Fatalf("user-owned: got %q, want hydra/", got)
	}
}

// hydra-k3nb L2: project attribution must survive path formatting drift
// (trailing slash, symlinked parents) — an exact-string match silently
// demoted user-owned projects to the daemon identity.
func TestNormalizeProjectPath(t *testing.T) {
	dir := t.TempDir()
	if normalizeProjectPath(dir+"/") != normalizeProjectPath(dir) {
		t.Fatal("trailing slash changed the normalized path")
	}
	if normalizeProjectPath(dir) == "" {
		t.Fatal("normalized path must never be empty for a real dir")
	}
}

// hydra-9dp5: merged-PR cleanup must only touch hydra-owned branch names.
func TestCleanupScopeGuard(t *testing.T) {
	for _, b := range []string{"dogfood/x-1", "hydra/x-2", "hydra/quarantine/z"} {
		if !IsHydraOwnedBranch(b, nil) {
			t.Fatalf("%s must be cleanup-eligible", b)
		}
	}
	for _, b := range []string{"main", "development", "feat/integration-test-impl", ""} {
		if IsHydraOwnedBranch(b, nil) {
			t.Fatalf("%s must NOT be cleanup-eligible", b)
		}
	}
}

// hydra-k3nb L2 follow-up: repos under a user's type=multi parent dir must
// attribute to that user — before this, every auto-discovered repo silently
// published under the daemon identity.
func TestGetUserContextForProject_MultiParentPrefix(t *testing.T) {
	parent := t.TempDir()
	child := parent + "/srv-jobfile"
	o := &Overseer{userContexts: map[string]*UserContext{"ajantha": {
		Username: "ajantha", HomeDir: parent,
		Config: UserConfig{Projects: []UserProject{{Name: "projects", Path: parent, Type: "multi"}}},
	}}}
	if got := o.getUserContextForProject(child); got == nil || got.Username != "ajantha" {
		t.Fatalf("multi child: got %+v, want ajantha", got)
	}
	if got := o.getUserContextForProject(child + "/"); got == nil {
		t.Fatal("trailing slash must still attribute")
	}
	if got := o.getUserContextForProject("/other/place/repo"); got != nil {
		t.Fatalf("unrelated path must not attribute, got %s", got.Username)
	}
}
