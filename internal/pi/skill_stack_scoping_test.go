package pi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectStacks(t *testing.T) {
	cases := []struct {
		marker string
		want   string
	}{
		{"go.mod", "go"},
		{"package.json", "node"},
		{"angular.json", "angular"},
		{"pyproject.toml", "python"},
		{"Cargo.toml", "rust"},
		{"tsconfig.json", "ts"},
	}
	for _, tc := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, tc.marker), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := DetectStacks(dir); !got[tc.want] {
			t.Errorf("%s: expected stack %q, got %v", tc.marker, tc.want, got)
		}
	}
	if s := DetectStacks(""); len(s) != 0 {
		t.Errorf("empty path: expected no stacks, got %v", s)
	}
	if s := DetectStacks(t.TempDir()); len(s) != 0 {
		t.Errorf("bare dir: expected no stacks, got %v", s)
	}
}

func TestSkillEnabledForStacks(t *testing.T) {
	goSkill := Skill{Name: "golang-pro", Stacks: []string{"go"}}
	generic := Skill{Name: "code-review"}

	if !skillEnabledForStacks(goSkill, map[string]bool{}) {
		t.Error("no detected stacks must include all skills")
	}
	if !skillEnabledForStacks(generic, map[string]bool{"go": true}) {
		t.Error("stack-agnostic skill must always be enabled")
	}
	if !skillEnabledForStacks(goSkill, map[string]bool{"go": true}) {
		t.Error("go skill on go project must be enabled")
	}
	if skillEnabledForStacks(goSkill, map[string]bool{"node": true}) {
		t.Error("go skill on node-only project must be dropped")
	}
}

func TestBuildSkillFlagsForProject_GoDropsFrontendSkills(t *testing.T) {
	configDir := t.TempDir()
	createTestSkills(t, configDir)

	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := BuildSkillFlagsForProject(configDir, proj)
	if err != nil {
		t.Fatalf("BuildSkillFlagsForProject: %v", err)
	}

	found := map[string]bool{}
	for _, f := range result.Found {
		found[f] = true
	}

	for _, keep := range []string{
		"golang-pro", "golang-testing", "golang-patterns",
		"code-review", "code-refactoring", "systematic-debugging",
		"test-driven-development", "lsp-code-analysis",
	} {
		if !found[keep] {
			t.Errorf("expected %q kept for a Go project", keep)
		}
	}
	for _, drop := range []string{
		"angular-component", "angular-signals", "angular-testing",
		"typescript-advanced-types", "nodejs-backend-patterns",
		"modern-javascript-patterns", "vitest", "implement-design",
	} {
		if found[drop] {
			t.Errorf("expected %q dropped for a Go project", drop)
		}
	}
}

func TestBuildSkillFlagsForProject_NoProjectIncludesAll(t *testing.T) {
	configDir := t.TempDir()
	createTestSkills(t, configDir)

	result, err := BuildSkillFlagsForProject(configDir, "")
	if err != nil {
		t.Fatalf("BuildSkillFlagsForProject: %v", err)
	}
	if len(result.Found) != len(DefaultSkills) {
		t.Errorf("empty projectPath must include all %d skills, got %d", len(DefaultSkills), len(result.Found))
	}
}
