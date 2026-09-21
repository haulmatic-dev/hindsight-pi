package core

import (
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestResolveRepoPath(t *testing.T) {
	cases := []struct {
		name    string
		project types.Project
		repo    types.Repo
		want    string
	}{
		{
			name:    "single repo ignores repo arg",
			project: types.Project{Name: "svc", Path: "/home/u/svc", Type: types.RepoTypeSingle},
			repo:    types.Repo{Name: "svc", Path: "ignored"},
			want:    "/home/u/svc",
		},
		{
			name:    "multi repo with relative path",
			project: types.Project{Name: "platform", Path: "/home/u/platform", Type: types.RepoTypeMulti},
			repo:    types.Repo{Name: "frontend", Path: "frontend"},
			want:    "/home/u/platform/frontend",
		},
		{
			name:    "multi repo with empty path falls back to name",
			project: types.Project{Name: "platform", Path: "/home/u/platform", Type: types.RepoTypeMulti},
			repo:    types.Repo{Name: "backend"},
			want:    "/home/u/platform/backend",
		},
		{
			name:    "multi repo with absolute path preserved",
			project: types.Project{Name: "platform", Path: "/home/u/platform", Type: types.RepoTypeMulti},
			repo:    types.Repo{Name: "api", Path: "/opt/monorepo/api"},
			want:    "/opt/monorepo/api",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveRepoPath(tc.project, tc.repo); got != tc.want {
				t.Errorf("resolveRepoPath() = %q, want %q", got, tc.want)
			}
		})
	}
}
