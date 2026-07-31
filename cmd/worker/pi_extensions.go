package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// extensionAliases maps the short names used in hydra.yaml to the npm package
// that provides them. Config should not have to carry scoped npm names.
var extensionAliases = map[string]string{
	"rtk":          "pi-rtk-optimizer",
	"caveman":      "@tomooshi/caveman-milk-pi",
	"context-mode": "context-mode",
	"guardrails":   "@aliou/pi-guardrails",
	"subagents":    "pi-subagents",
	"web-access":   "pi-web-access",
}

// parseExtensionList splits the comma-separated HYDRA_PI_EXTENSIONS value.
func parseExtensionList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if name := strings.TrimSpace(part); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// resolveExtensionPaths turns allowlist names into absolute extension entry
// files that Pi's `-e` flag accepts.
//
// Pi resolves `-e` against a file, not a package, so each name has to be walked
// to the entry point its package.json declares under "pi".extensions (falling
// back to "main"). Unresolvable names are logged and skipped rather than failing
// the bead: a mistyped or uninstalled extension should cost that extension, not
// the work.
func resolveExtensionPaths(agentDir string, names []string) []string {
	if agentDir == "" || len(names) == 0 {
		return nil
	}
	modules := filepath.Join(agentDir, "npm", "node_modules")

	var paths []string
	for _, name := range names {
		pkg := name
		if alias, ok := extensionAliases[strings.ToLower(name)]; ok {
			pkg = alias
		}
		entry, err := extensionEntryPoint(filepath.Join(modules, filepath.FromSlash(pkg)))
		if err != nil {
			log.Printf("[Worker:rpc] skipping extension %q: %v", name, err)
			continue
		}
		paths = append(paths, entry)
	}
	return paths
}

// extensionEntryPoint reads a package's declared Pi extension entry file.
func extensionEntryPoint(pkgDir string) (string, error) {
	manifest, err := os.ReadFile(filepath.Join(pkgDir, "package.json"))
	if err != nil {
		return "", err
	}
	var pkg struct {
		Main string `json:"main"`
		Pi   struct {
			Extensions []string `json:"extensions"`
		} `json:"pi"`
	}
	if err := json.Unmarshal(manifest, &pkg); err != nil {
		return "", err
	}

	rel := pkg.Main
	if len(pkg.Pi.Extensions) > 0 && pkg.Pi.Extensions[0] != "" {
		rel = pkg.Pi.Extensions[0]
	}
	if rel == "" {
		rel = "index.js"
	}

	entry := filepath.Clean(filepath.Join(pkgDir, filepath.FromSlash(rel)))
	if _, err := os.Stat(entry); err != nil {
		return "", err
	}
	return absOrSelf(entry), nil
}

// extensionArgs builds the Pi flags that load exactly the allowlist and nothing
// else. --no-extensions disables discovery while explicit -e paths still load,
// which is what keeps the cost down: measured on one identical turn, loading
// every installed extension cost 48,315 tokens against 12,059 for rtk alone,
// and the extras (themes, editor widgets, subagent UI) are unusable by a
// headless worker anyway.
func extensionArgs(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	args := []string{"--no-extensions"}
	for _, p := range paths {
		args = append(args, "-e", p)
	}
	return args
}
