// prompt.go - Template-based prompt loading for Pi agent

package pi

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

// TemplateData holds the variables available in prompt templates
type TemplateData struct {
	Title       string
	Description string
	Type        string
	Skills      []SkillInfo
	Files       []string
	Context     string
	Project     string
	BeadID      string
}

// SkillInfo represents a skill available to the agent
type SkillInfo struct {
	Name        string
	Description string
}

// Loader handles loading and rendering prompt templates
type Loader struct {
	baseDir         string
	piTemplate      string
	defaultTemplate string
}

// NewPromptLoader creates a new prompt loader with the specified base directory
func NewPromptLoader(baseDir string) *Loader {
	promptDir := filepath.Join(baseDir, "prompts")
	return &Loader{
		baseDir:         baseDir,
		piTemplate:      filepath.Join(promptDir, "pi-agent.md"),
		defaultTemplate: filepath.Join(promptDir, "default.md"),
	}
}

// LoadTemplate loads and parses a template file, falling back to default if not found
func (l *Loader) LoadTemplate(templatePath string) (*template.Template, error) {
	// Check if specific template exists
	if _, err := os.Stat(templatePath); err == nil {
		content, err := os.ReadFile(templatePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read template %s: %w", templatePath, err)
		}
		tmpl, err := template.New("prompt").Parse(string(content))
		if err != nil {
			return nil, fmt.Errorf("failed to parse template %s: %w", templatePath, err)
		}
		return tmpl, nil
	}

	// Fall back to default template
	if templatePath != l.defaultTemplate {
		return l.LoadTemplate(l.defaultTemplate)
	}

	return nil, fmt.Errorf("no template found at %s or default", templatePath)
}

// RenderPrompt renders a prompt template with the given data
func (l *Loader) RenderPrompt(templatePath string, data TemplateData) (string, error) {
	tmpl, err := l.LoadTemplate(templatePath)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render template: %w", err)
	}

	return buf.String(), nil
}

// GetPiAgentPrompt renders the Pi agent prompt template
func (l *Loader) GetPiAgentPrompt(data TemplateData) (string, error) {
	return l.RenderPrompt(l.piTemplate, data)
}

// DefaultPiPrompt returns the hardcoded default Pi agent prompt (fallback)
func DefaultPiPrompt(data TemplateData) string {
	return fmt.Sprintf(`# Task: %s

## Description
%s

## Instructions
Please complete this task based on the description above.
When finished, ensure all files are saved and the solution is complete.

## Available Skills
You have access to the code-search-and-intelligence skill to help with implementation:

- **code-search-and-intelligence**: Query institutional knowledge (cass_memory) and search code semantically (TLDR)
  Use this skill to find patterns, anti-patterns, and understand the codebase before implementing.

**Use this skill as needed based on your judgment.**`, data.Title, data.Description)
}


