package types

// ProjectProfile describes the technology stack detected from a project root.
// It is used to load relevant skills and inject context into agent prompts.
type ProjectProfile struct {
	// Primary language/framework, e.g. "go", "typescript", "rust", "python".
	// Determined by manifest priority or explicit config override.
	Primary string `json:"primary"`

	// Detected lists all languages found (for polyglot/brownfield projects).
	Detected []string `json:"detected"`

	// Manifest is the primary manifest file name, e.g. "go.mod", "package.json".
	Manifest string `json:"manifest"`

	// BuildCmd is the idiomatic build command for the primary language.
	BuildCmd string `json:"build_cmd"`

	// TestCmd is the idiomatic test command.
	TestCmd string `json:"test_cmd"`

	// Extension is the primary source file extension (".go", ".ts", etc).
	Extension string `json:"extension"`
}

// IsZero returns true if the profile has not been populated.
func (p ProjectProfile) IsZero() bool {
	return p.Primary == "" && len(p.Detected) == 0
}

// Supports returns true if the profile indicates the given language.
func (p ProjectProfile) Supports(lang string) bool {
	if p.Primary == lang {
		return true
	}
	for _, d := range p.Detected {
		if d == lang {
			return true
		}
	}
	return false
}
