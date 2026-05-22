package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestDetectProfile(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		want     types.ProjectProfile
	}{
		{
			name:  "go only",
			files: []string{"go.mod"},
			want: types.ProjectProfile{
				Primary:   "go",
				Detected:  []string{"go"},
				Manifest:  "go.mod",
				BuildCmd:  "go build ./...",
				TestCmd:   "go test ./...",
				Extension: ".go",
			},
		},
		{
			name:  "typescript only",
			files: []string{"package.json"},
			want: types.ProjectProfile{
				Primary:   "typescript",
				Detected:  []string{"typescript"},
				Manifest:  "package.json",
				BuildCmd:  "npm run build",
				TestCmd:   "npm test",
				Extension: ".ts",
			},
		},
		{
			name:  "polyglot go + typescript (go first)",
			files: []string{"go.mod", "package.json"},
			want: types.ProjectProfile{
				Primary:   "go",
				Detected:  []string{"go", "typescript"},
				Manifest:  "go.mod",
				BuildCmd:  "go build ./...",
				TestCmd:   "go test ./...",
				Extension: ".go",
			},
		},
		{
			name:  "rust",
			files: []string{"Cargo.toml"},
			want: types.ProjectProfile{
				Primary:   "rust",
				Detected:  []string{"rust"},
				Manifest:  "Cargo.toml",
				BuildCmd:  "cargo build",
				TestCmd:   "cargo test",
				Extension: ".rs",
			},
		},
		{
			name:  "python pyproject",
			files: []string{"pyproject.toml"},
			want: types.ProjectProfile{
				Primary:   "python",
				Detected:  []string{"python"},
				Manifest:  "pyproject.toml",
				BuildCmd:  "python -m build",
				TestCmd:   "pytest",
				Extension: ".py",
			},
		},
		{
			name:  "nothing",
			files: []string{},
			want:  types.ProjectProfile{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte(""), 0644); err != nil {
					t.Fatal(err)
				}
			}
			got := DetectProfile(dir)
			if got.Primary != tt.want.Primary {
				t.Errorf("Primary = %q, want %q", got.Primary, tt.want.Primary)
			}
			if len(got.Detected) != len(tt.want.Detected) {
				t.Errorf("Detected = %v, want %v", got.Detected, tt.want.Detected)
			}
			if got.Manifest != tt.want.Manifest {
				t.Errorf("Manifest = %q, want %q", got.Manifest, tt.want.Manifest)
			}
		})
	}
}

func TestSkillMatchesProfile(t *testing.T) {
	goProfile := types.ProjectProfile{Primary: "go", Detected: []string{"go"}}
	tsProfile := types.ProjectProfile{Primary: "typescript", Detected: []string{"typescript"}}
	polyProfile := types.ProjectProfile{Primary: "go", Detected: []string{"go", "typescript"}}

	tests := []struct {
		name string
		tags []string
		prof types.ProjectProfile
		want bool
	}{
		{"generic skill, go project", nil, goProfile, true},
		{"go skill, go project", []string{"go"}, goProfile, true},
		{"ts skill, go project", []string{"typescript"}, goProfile, false},
		{"ts skill, ts project", []string{"typescript"}, tsProfile, true},
		{"go skill, poly project", []string{"go"}, polyProfile, true},
		{"ts skill, poly project", []string{"typescript"}, polyProfile, true},
		{"unknown project", []string{"go"}, types.ProjectProfile{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SkillMatchesProfile(tt.tags, tt.prof)
			if got != tt.want {
				t.Errorf("SkillMatchesProfile(%v, %+v) = %v, want %v", tt.tags, tt.prof, got, tt.want)
			}
		})
	}
}

func TestParseSkillTags(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{"golang-pro", []string{"go"}},
		{"angular-component", []string{"typescript"}},
		{"typescript-advanced-types", []string{"typescript"}},
		{"nodejs-backend-patterns", []string{"typescript", "javascript"}},
		{"vitest", []string{"typescript", "javascript"}},
		{"rust-patterns", []string{"rust"}},
		{"python-testing", []string{"python"}},
		{"code-review", nil},      // generic
		{"test-driven-development", nil}, // generic
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSkillTags(tt.name)
			if len(got) != len(tt.want) {
				t.Errorf("ParseSkillTags(%q) = %v, want %v", tt.name, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParseSkillTags(%q)[%d] = %q, want %q", tt.name, i, got[i], tt.want[i])
				}
			}
		})
	}
}
