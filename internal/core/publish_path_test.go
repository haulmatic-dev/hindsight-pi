package core

import "testing"

func TestClassifyPushFailure(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   PublishPathClass
	}{
		{"empty", "", ClassAccepted},
		{"gh013 ruleset", "remote: error: GH013: Push declined due to repository rule violations", ClassProtected},
		{"gh006", "remote: error: GH006: Protected branch update failed", ClassProtected},
		{"protected branch", "! [remote rejected] development -> development (protected branch hook declined)", ClassProtected},
		{"pre-receive", "remote: pre-receive hook declined", ClassProtected},
		{"required review", "remote: error: required review on this branch", ClassProtected},
		{"pr required", "remote: changes must be made through a pull request", ClassProtected},
		{"non-ff", "! [rejected] development -> development (non-fast-forward)", ClassStale},
		{"fetch first", "error: failed to push some refs to 'origin'\nhint: Updates were rejected because the remote contains work that you do not have locally (fetch first)", ClassStale},
		{"dns", "ssh: Could not resolve hostname github.com: Name or service not known", ClassTransient},
		{"auth", "remote: Invalid username or password\nfatal: Authentication failed for 'https://github.com/o/r.git/'", ClassTransient},
		{"timeout", "fatal: unable to access 'https://github.com/o/r.git/': Failed to connect to github.com port 443: Connection timed out", ClassTransient},
		{"rpc eof", "fatal: the remote end hung up unexpectedly\nerror: RPC failed; HTTP 502 curl 22", ClassTransient},
		{"garbage", "some totally unrecognized failure text", ClassUnknown},
		{"repo not found", "remote: Repository not found.\nfatal: repository 'https://github.com/o/r.git/' not found", ClassNotFound},
		{"not a git repo", "fatal: 'https://github.com/o/r.git/' does not appear to be a git repository", ClassNotFound},
		// stale beats protected when both markers present: GitHub phrases
		// non-FF as "remote rejected (non-fast-forward)".
		{"remote rejected non-ff", "! [remote rejected] dev -> dev (non-fast-forward)", ClassStale},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyPushFailure(tc.stderr); got != tc.want {
				t.Errorf("ClassifyPushFailure(%q) = %q, want %q", tc.stderr, got, tc.want)
			}
		})
	}
}

func TestIsHydraOwnedBranch(t *testing.T) {
	extra := []string{"sandbox/"}
	cases := []struct {
		branch string
		want   bool
	}{
		{"hydra/wt-hydra-515", true},
		{"dogfood/hydra-515", true},
		{"sandbox/thing", true},
		{"development", false},
		{"main", false},
		{"feature/foo", false},
		{"", false},
		{"hydrafoo", false},
	}
	for _, tc := range cases {
		if got := IsHydraOwnedBranch(tc.branch, extra); got != tc.want {
			t.Errorf("IsHydraOwnedBranch(%q) = %v, want %v", tc.branch, got, tc.want)
		}
	}
	if IsHydraOwnedBranch("dogfood/x", nil) != true {
		t.Error("built-in prefixes must hold without extras")
	}
}

func TestResolvePublishPath(t *testing.T) {
	cases := []struct {
		name  string
		class PublishPathClass
		owned bool
		tier1 bool
		want  PublishAction
	}{
		{"accepted owned", ClassAccepted, true, false, ActPushDirect},
		{"accepted unowned", ClassAccepted, false, false, ActParkLocalLoud},
		{"protected product", ClassProtected, true, false, ActCreatePR},
		{"protected tier1", ClassProtected, true, true, ActCreatePR},
		{"stale", ClassStale, true, false, ActRebaseRetry},
		{"transient", ClassTransient, true, true, ActParkLocalLoud},
		{"unknown", ClassUnknown, true, false, ActParkLocalLoud},
		{"not found", ClassNotFound, true, true, ActParkLocalLoud},
		{"unexpected class", PublishPathClass("WEIRD"), true, true, ActParkLocalLoud},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolvePublishPath(tc.class, tc.owned, tc.tier1); got != tc.want {
				t.Errorf("ResolvePublishPath(%q,%v,%v) = %q, want %q", tc.class, tc.owned, tc.tier1, got, tc.want)
			}
		})
	}
}
