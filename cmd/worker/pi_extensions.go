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

// solPiConfigJSON is the vendor's conservative pilot profile: only Action
// Fusion and ObservationPack — the two mechanisms that make no extra model
// calls and never stop an active run. Evidence-Preserving Reducer (extra
// remote model call) and Online Context Compact (starts a new turn after
// compacting, breaking the frozen-prefix prompt-cache program) stay off.
// See docs/operations/sol-pi-pilot.md.
const solPiConfigJSON = `{
  "version": 1,
  "actionFusion": true,
  "observationPack": true,
  "evidencePreservingReducer": false,
  "evidencePreservingReducerRemote": false,
  "onlineContextCompact": false,
  "cacheWriteReadRatio": 12.5
}
`

// hasExtension reports whether names contains name (case-insensitive).
func hasExtension(names []string, name string) bool {
	for _, n := range names {
		if strings.EqualFold(strings.TrimSpace(n), name) {
			return true
		}
	}
	return false
}

// solPiResolved reports whether "sol-pi" is allowlisted AND its package
// actually resolves to an entry point. Provisioning config (and granting
// per-run project trust) for an extension that is not installed would be a
// behaviour change for zero benefit.
func solPiResolved(agentDir string, names []string) bool {
	if !hasExtension(names, "sol-pi") {
		return false
	}
	return len(resolveExtensionPaths(agentDir, []string{"sol-pi"})) == 1
}

// provisionSolPi writes the conservative pilot config to
// <cwd>/.pi/sol-pi.json and excludes .pi/ from the worktree's git index via
// .git/info/exclude (worktree-local, never committed — same trick as the
// daemon's .git/info/attributes merge=union setup). An existing sol-pi.json
// is left untouched: a project that ships its own config wins over the
// pilot default.
//
// SoL-Pi only reads the project-level config when the project is trusted.
// Non-interactive modes (rpc included) never prompt, so the caller passes
// --approve for this run when this returns true.
func provisionSolPi(cwd string) bool {
	dir := filepath.Join(cwd, ".pi")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		log.Printf("[Worker:rpc] sol-pi: cannot create %s: %v", dir, err)
		return false
	}
	cfg := filepath.Join(dir, "sol-pi.json")
	if _, err := os.Stat(cfg); err == nil {
		log.Printf("[Worker:rpc] sol-pi: %s already exists, keeping project config", cfg)
	} else if err := os.WriteFile(cfg, []byte(solPiConfigJSON), 0o640); err != nil {
		log.Printf("[Worker:rpc] sol-pi: cannot write %s: %v", cfg, err)
		return false
	}
	// Keep the config (and anything else .pi/) out of worker commits in target
	// repos that do not gitignore it. Info/exclude is per-worktree metadata.
	if gitDir := filepath.Join(cwd, ".git"); isDir(gitDir) {
		ex := filepath.Join(gitDir, "info", "exclude")
		if err := os.MkdirAll(filepath.Dir(ex), 0o750); err == nil {
			if data, err := os.ReadFile(ex); err != nil || !strings.Contains(string(data), "\n.pi/") {
				f, err := os.OpenFile(ex, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
				if err == nil {
					_, err = f.WriteString("\n# hydra sol-pi pilot config — never commit\n.pi/\n")
					f.Close()
				}
				if err != nil {
					log.Printf("[Worker:rpc] sol-pi: cannot update %s: %v", ex, err)
				}
			}
		}
	}
	return true
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

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
	// sol-pi needs no alias: config may carry the package name directly.
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
