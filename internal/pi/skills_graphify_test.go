package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGraphifyIsARegisteredDefaultSkill pins the wiring that makes the installed
// skill actually reach Pi.
//
// Skill flags are built from DefaultSkills. A skill present in the config
// directory but absent from that list is never passed to Pi — so
// `graphify install --platform pi` on its own accomplishes nothing. This test
// exists because that gap was invisible: the skill was installed on the host, the
// directory listing looked correct, and no worker would ever have seen it.
func TestGraphifyIsARegisteredDefaultSkill(t *testing.T) {
	var found *Skill
	for i := range DefaultSkills {
		if DefaultSkills[i].Name == "graphify" {
			found = &DefaultSkills[i]
			break
		}
	}
	if found == nil {
		t.Fatal("graphify must be in DefaultSkills or its flag is never passed to Pi")
	}
	// Stack-agnostic on purpose: the code graph answers questions about any
	// codebase, and stack-scoping would drop it on projects whose stack is known.
	if len(found.Stacks) != 0 {
		t.Errorf("graphify must be stack-agnostic, got Stacks=%v", found.Stacks)
	}
	// Externally managed by pip plus `graphify install --platform pi`, so it must
	// carry no Repo — otherwise the setup manager hands it to `npx skills add`.
	if strings.TrimSpace(found.Repo) != "" {
		t.Errorf("graphify Repo must be empty (externally managed), got %q", found.Repo)
	}
}

// TestBuildSkillFlags_IncludesGraphifyWhenInstalled checks the end result: a
// --skill flag pointing at the installed skill directory.
func TestBuildSkillFlags_IncludesGraphifyWhenInstalled(t *testing.T) {
	configDir := t.TempDir()
	skillDir := filepath.Join(configDir, "skills", "graphify")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: graphify\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := BuildSkillFlagsForProject(configDir, t.TempDir())
	if err != nil {
		t.Fatalf("BuildSkillFlagsForProject: %v", err)
	}
	if !strings.Contains(strings.Join(res.Flags, " "), skillDir) {
		t.Errorf("expected a --skill flag for %s; found=%v missing=%v", skillDir, res.Found, res.Missing)
	}
}
