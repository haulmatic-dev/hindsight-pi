package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
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
	"hindsight":    "@walodayeet/hindsight-pi",
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
			// Git-installed packages live at git/<host>/<org>/<repo>, not in
			// npm/node_modules. Match by repo segment so an org rename or fork
			// (haulmatic-dev/hindsight-pi vs walodayeet/hindsight-pi) does not
			// silently unload the extension.
			entry, err = gitExtensionEntryPoint(agentDir, path.Base(pkg))
			if err != nil {
				log.Printf("[Worker:rpc] skipping extension %q: %v", name, err)
				continue
			}
		}
		paths = append(paths, entry)
	}
	return paths
}

// gitExtensionEntryPoint resolves a git-installed Pi package by repo name.
// Sorted glob = deterministic; first match with a valid entry point wins.
func gitExtensionEntryPoint(agentDir, repo string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(agentDir, "git", "*", "*", repo))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no git-installed package named %s", repo)
	}
	sort.Strings(matches)
	for _, dir := range matches {
		if entry, err := extensionEntryPoint(dir); err == nil {
			if len(matches) > 1 {
				log.Printf("[Worker:rpc] extension %q: multiple git installs, using %s", repo, dir)
			}
			return entry, nil
		}
	}
	return "", fmt.Errorf("git-installed package %s has no valid entry point", repo)
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
	info, err := os.Stat(entry)
	if err != nil {
		return "", err
	}
	// A declared entry may be a directory (pi convention: extensions/*/index.ts).
	// Pi's -e wants a file, so resolve directories to their index file.
	if info.IsDir() {
		for _, idx := range []string{"index.ts", "index.js"} {
			cand := filepath.Join(entry, idx)
			if _, err := os.Stat(cand); err == nil {
				return absOrSelf(cand), nil
			}
		}
		return "", fmt.Errorf("declared entry %s is a directory without index.ts/index.js", entry)
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
