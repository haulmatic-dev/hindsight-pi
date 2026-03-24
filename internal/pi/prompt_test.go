package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptLoader_LoadTemplate(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	promptsDir := filepath.Join(tmpDir, "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatalf("Failed to create prompts dir: %v", err)
	}

	// Create a test template
	testTemplate := "# Task: {{.Title}}\n## Description\n{{.Description}}"
	templatePath := filepath.Join(promptsDir, "test.md")
	if err := os.WriteFile(templatePath, []byte(testTemplate), 0644); err != nil {
		t.Fatalf("Failed to write test template: %v", err)
	}

	// Create loader
	loader := NewPromptLoader(tmpDir)

	// Test loading existing template
	tmpl, err := loader.LoadTemplate(templatePath)
	if err != nil {
		t.Errorf("Failed to load template: %v", err)
	}
	if tmpl == nil {
		t.Error("Template should not be nil")
	}
}

func TestPromptLoader_RenderPrompt(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	promptsDir := filepath.Join(tmpDir, "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatalf("Failed to create prompts dir: %v", err)
	}

	// Create a test template
	testTemplate := "# Task: {{.Title}}\n## Description\n{{.Description}}"
	templatePath := filepath.Join(promptsDir, "test.md")
	if err := os.WriteFile(templatePath, []byte(testTemplate), 0644); err != nil {
		t.Fatalf("Failed to write test template: %v", err)
	}

	// Create loader
	loader := NewPromptLoader(tmpDir)

	// Test rendering
	data := TemplateData{
		Title:       "Test Task",
		Description: "This is a test description",
		Type:        "implementation",
	}

	result, err := loader.RenderPrompt(templatePath, data)
	if err != nil {
		t.Errorf("Failed to render prompt: %v", err)
	}

	expected := "# Task: Test Task\n## Description\nThis is a test description"
	if result != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
	}
}

func TestPromptLoader_GetPiAgentPrompt(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	promptsDir := filepath.Join(tmpDir, "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatalf("Failed to create prompts dir: %v", err)
	}

	// Create a pi-agent template
	testTemplate := `# Task: {{.Title}}

## Description
{{.Description}}

## Available Skills
{{range .Skills}}- **{{.Name}}**: {{.Description}}
{{end}}`

	templatePath := filepath.Join(promptsDir, "pi-agent.md")
	if err := os.WriteFile(templatePath, []byte(testTemplate), 0644); err != nil {
		t.Fatalf("Failed to write pi-agent template: %v", err)
	}

	// Create loader
	loader := NewPromptLoader(tmpDir)

	// Test getting Pi agent prompt
	data := TemplateData{
		Title:       "Implement Auth",
		Description: "Add JWT authentication",
		Skills: []SkillInfo{
			{Name: "golang-pro", Description: "Go development"},
		},
	}

	result, err := loader.GetPiAgentPrompt(data)
	if err != nil {
		t.Errorf("Failed to get Pi agent prompt: %v", err)
	}

	if !strings.Contains(result, "Implement Auth") {
		t.Error("Prompt should contain the task title")
	}
	if !strings.Contains(result, "Add JWT authentication") {
		t.Error("Prompt should contain the description")
	}
	if !strings.Contains(result, "golang-pro") {
		t.Error("Prompt should contain the skills")
	}
}

func TestPromptLoader_FallbackToDefault(t *testing.T) {
	// Create a temporary directory for testing (no templates)
	tmpDir := t.TempDir()

	// Create loader (no templates exist)
	loader := NewPromptLoader(tmpDir)

	// Test that loading a non-existent template returns error
	_, err := loader.LoadTemplate("/nonexistent/template.md")
	if err == nil {
		t.Error("Should return error when template not found")
	}
}

func TestDefaultPiPrompt(t *testing.T) {
	data := TemplateData{
		Title:       "Test Task",
		Description: "Test Description",
	}

	result := DefaultPiPrompt(data)

	if !strings.Contains(result, "Test Task") {
		t.Error("Default prompt should contain title")
	}
	if !strings.Contains(result, "Test Description") {
		t.Error("Default prompt should contain description")
	}
	if !strings.Contains(result, "code-search-and-intelligence") {
		t.Error("Default prompt should mention available skills")
	}
}

func TestDefaultOpencodePrompt(t *testing.T) {
	data := TemplateData{
		Title:       "Test Task",
		Description: "Test Description",
		Type:        "implementation",
		Files:       []string{"file1.go", "file2.go"},
	}

	result := DefaultOpencodePrompt(data)

	if !strings.Contains(result, "implementation: Test Task") {
		t.Error("Default prompt should contain type and title")
	}
	if !strings.Contains(result, "file1.go") {
		t.Error("Default prompt should contain files when provided")
	}
}
