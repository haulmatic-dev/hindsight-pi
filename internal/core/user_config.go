package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// UserConfig represents a user's Hydra configuration.
// Lives at /home/<user>/.hydra/config.yaml
type UserConfig struct {
	User     UserInfo       `yaml:"user"`
	Git      GitCredentials `yaml:"git"`
	Projects []UserProject  `yaml:"projects,omitempty"`
}

// UserInfo contains the user's identity.
type UserInfo struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

// GitCredentials configures how Hydra authenticates with Git providers.
type GitCredentials struct {
	Provider   string `yaml:"provider"`    // "github" or "gitlab"
	Username   string `yaml:"username"`
	TokenFile  string `yaml:"token_file"`  // Path to file containing PAT
	TokenEnv   string `yaml:"token_env"`   // Env var name (fallback)
	Token      string `yaml:"token"`       // Inline token (least secure)
}

// UserProject defines a project to watch for a specific user.
type UserProject struct {
	Name         string       `yaml:"name"`
	Path         string       `yaml:"path"`
	Type         string       `yaml:"type,omitempty"`         // "single" or "multi"
	Repos        []RepoConfig `yaml:"repos,omitempty"`        // Explicit repos for multi
	RepoURL      string       `yaml:"repo_url,omitempty"`     // Remote URL
	PRDPath      string       `yaml:"prd_path,omitempty"`     // e.g. docs/PRDs/current.md
	Branch       string       `yaml:"branch,omitempty"`       // Default: main
	AutoDiscover bool         `yaml:"auto_discover,omitempty"`// Discover repos if type=multi
}

// UserContext holds a loaded user config with resolved paths and tokens.
type UserContext struct {
	Config      UserConfig
	Username    string
	HomeDir     string
	HydraDir    string
	Token       string // Resolved PAT
}

// LoadUserConfig reads and parses a user's Hydra config.
func LoadUserConfig(homeDir string) (*UserContext, error) {
	username := filepath.Base(homeDir)
	hydraDir := filepath.Join(homeDir, ".hydra")
	configPath := filepath.Join(hydraDir, "config.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no user config found at %s", configPath)
		}
		return nil, fmt.Errorf("read user config: %w", err)
	}

	var cfg UserConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse user config: %w", err)
	}

	// Resolve token (use homeDir for path expansion, not hydraDir)
	token, err := resolveToken(cfg.Git, homeDir)
	if err != nil {
		return nil, fmt.Errorf("resolve git token: %w", err)
	}

	// Expand project paths
	for i := range cfg.Projects {
		cfg.Projects[i].Path = expandPath(cfg.Projects[i].Path, homeDir)
		if cfg.Projects[i].PRDPath == "" {
			cfg.Projects[i].PRDPath = "docs/PRDs/current.md"
		}
		if cfg.Projects[i].Branch == "" {
			cfg.Projects[i].Branch = "main"
		}
	}

	return &UserContext{
		Config:   cfg,
		Username: username,
		HomeDir:  homeDir,
		HydraDir: hydraDir,
		Token:    token,
	}, nil
}

// resolveToken finds the Git PAT from file, env, or inline.
func resolveToken(git GitCredentials, hydraDir string) (string, error) {
	if git.TokenFile != "" {
		path := expandPath(git.TokenFile, hydraDir)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read token file %s: %w", path, err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	if git.TokenEnv != "" {
		token := os.Getenv(git.TokenEnv)
		if token == "" {
			return "", fmt.Errorf("env var %s not set", git.TokenEnv)
		}
		return token, nil
	}

	if git.Token != "" {
		return git.Token, nil
	}

	return "", fmt.Errorf("no token configured (token_file, token_env, or token required)")
}

// expandPath expands ~ and relative paths.
func expandPath(path, homeDir string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:])
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(homeDir, path)
}

// GitAuthor returns the git user.name and user.email for this user.
func (u *UserContext) GitAuthor() (name, email string) {
	if u.Config.User.Name != "" {
		name = u.Config.User.Name
	} else {
		name = u.Config.Git.Username
	}
	if u.Config.User.Email != "" {
		email = u.Config.User.Email
	} else {
		email = fmt.Sprintf("%s@users.noreply.github.com", u.Config.Git.Username)
	}
	return name, email
}

// GitRemoteURL returns the authenticated remote URL with embedded PAT.
func (u *UserContext) GitRemoteURL(repoURL string) string {
	if u.Token == "" {
		return repoURL
	}
	// Convert https://github.com/user/repo to https://user:token@github.com/user/repo
	if strings.HasPrefix(repoURL, "https://") {
		return strings.Replace(repoURL, "https://", fmt.Sprintf("https://%s:%s@", u.Config.Git.Username, u.Token), 1)
	}
	return repoURL
}
