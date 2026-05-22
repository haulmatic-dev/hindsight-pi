package project

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// detectionRule maps a manifest file to a project profile.
type detectionRule struct {
	manifest  string
	lang      string
	buildCmd  string
	testCmd   string
	extension string
}

// Rules are evaluated in order; first match wins for Primary.
// Keep Go early since many Go projects also have package.json (frontend).
var rules = []detectionRule{
	{"go.mod", "go", "go build ./...", "go test ./...", ".go"},
	{"Cargo.toml", "rust", "cargo build", "cargo test", ".rs"},
	{"pyproject.toml", "python", "python -m build", "pytest", ".py"},
	{"setup.py", "python", "python setup.py build", "pytest", ".py"},
	{"requirements.txt", "python", "pip install -r requirements.txt", "pytest", ".py"},
	{"package.json", "typescript", "npm run build", "npm test", ".ts"},
	{"Gemfile", "ruby", "bundle exec rake build", "bundle exec rspec", ".rb"},
	{"composer.json", "php", "composer install", "vendor/bin/phpunit", ".php"},
	{"pom.xml", "java", "mvn compile", "mvn test", ".java"},
	{"build.gradle", "java", "gradle build", "gradle test", ".java"},
	{"mix.exs", "elixir", "mix compile", "mix test", ".ex"},
}

// DetectProfile scans a project root for manifest files and returns a
// ProjectProfile.  It works for both greenfield (single stack) and brownfield
// (polyglot / migration) projects.
func DetectProfile(projectPath string) types.ProjectProfile {
	var detected []string
	var primaryRule *detectionRule

	for _, r := range rules {
		if fileExists(filepath.Join(projectPath, r.manifest)) {
			detected = append(detected, r.lang)
			if primaryRule == nil {
				primaryRule = &r
			}
		}
	}

	if primaryRule == nil {
		return types.ProjectProfile{}
	}

	return types.ProjectProfile{
		Primary:   primaryRule.lang,
		Detected:  detected,
		Manifest:  primaryRule.manifest,
		BuildCmd:  primaryRule.buildCmd,
		TestCmd:   primaryRule.testCmd,
		Extension: primaryRule.extension,
	}
}

// DetectProfileWithOverride is like DetectProfile but respects an explicit
// language override from configuration.
func DetectProfileWithOverride(projectPath string, override string) types.ProjectProfile {
	p := DetectProfile(projectPath)
	if override != "" {
		p.Primary = override
		// Ensure override is in Detected list.
		found := false
		for _, d := range p.Detected {
			if d == override {
				found = true
				break
			}
		}
		if !found {
			p.Detected = append([]string{override}, p.Detected...)
		}
	}
	return p
}

// fileExists returns true if path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// SkillMatchesProfile returns true if a skill is relevant for the given profile.
// A skill is relevant when:
//   - it has no language tags (generic, always loaded)
//   - at least one of its tags matches the profile's Primary language
func SkillMatchesProfile(skillTags []string, profile types.ProjectProfile) bool {
	if len(skillTags) == 0 {
		return true // generic skill, always include
	}
	if profile.IsZero() {
		return true // unknown project, include everything
	}
	for _, tag := range skillTags {
		if profile.Supports(tag) {
			return true
		}
	}
	return false
}

// ParseSkillTags extracts language tags from a skill name using naming conventions.
// This is a fallback when explicit SKILL.md tags are not present.
// Examples:
//   golang-pro          -> ["go"]
//   angular-component   -> ["typescript"]
//   nodejs-backend      -> ["typescript", "javascript"]
//   typescript-advanced -> ["typescript"]
//   rust-patterns       -> ["rust"]
//   python-testing      -> ["python"]
func ParseSkillTags(skillName string) []string {
	lower := strings.ToLower(skillName)

	// Direct prefix matches
	if strings.HasPrefix(lower, "golang-") || lower == "golang" {
		return []string{"go"}
	}
	if strings.HasPrefix(lower, "angular-") || lower == "angular" {
		return []string{"typescript"}
	}
	if strings.HasPrefix(lower, "typescript-") || lower == "typescript" {
		return []string{"typescript"}
	}
	if strings.HasPrefix(lower, "javascript-") || lower == "javascript" {
		return []string{"javascript", "typescript"}
	}
	if strings.HasPrefix(lower, "nodejs-") || lower == "nodejs" {
		return []string{"typescript", "javascript"}
	}
	if strings.HasPrefix(lower, "vitest") {
		return []string{"typescript", "javascript"}
	}
	if strings.HasPrefix(lower, "react-") || lower == "react" {
		return []string{"typescript", "javascript"}
	}
	if strings.HasPrefix(lower, "vue-") || lower == "vue" {
		return []string{"typescript", "javascript"}
	}
	if strings.HasPrefix(lower, "svelte-") || lower == "svelte" {
		return []string{"typescript", "javascript"}
	}
	if strings.HasPrefix(lower, "rust-") || lower == "rust" {
		return []string{"rust"}
	}
	if strings.HasPrefix(lower, "cargo-") || lower == "cargo" {
		return []string{"rust"}
	}
	if strings.HasPrefix(lower, "python-") || lower == "python" {
		return []string{"python"}
	}
	if strings.HasPrefix(lower, "django-") || lower == "django" {
		return []string{"python"}
	}
	if strings.HasPrefix(lower, "flask-") || lower == "flask" {
		return []string{"python"}
	}
	if strings.HasPrefix(lower, "ruby-") || lower == "ruby" {
		return []string{"ruby"}
	}
	if strings.HasPrefix(lower, "rails-") || lower == "rails" {
		return []string{"ruby"}
	}
	if strings.HasPrefix(lower, "php-") || lower == "php" {
		return []string{"php"}
	}
	if strings.HasPrefix(lower, "laravel-") || lower == "laravel" {
		return []string{"php"}
	}
	if strings.HasPrefix(lower, "java-") || lower == "java" {
		return []string{"java"}
	}
	if strings.HasPrefix(lower, "spring-") || lower == "spring" {
		return []string{"java"}
	}
	if strings.HasPrefix(lower, "elixir-") || lower == "elixir" {
		return []string{"elixir"}
	}
	if strings.HasPrefix(lower, "phoenix-") || lower == "phoenix" {
		return []string{"elixir"}
	}

	// No tags -> generic skill
	return nil
}
