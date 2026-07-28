package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// writePkg creates a fake installed extension package with the given declared
// entry file, mirroring the real layout: <agentDir>/npm/node_modules/<pkg>/.
func writePkg(t *testing.T, agentDir, pkg, manifest, entryRel string) string {
	t.Helper()
	dir := filepath.Join(agentDir, "npm", "node_modules", filepath.FromSlash(pkg))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if entryRel != "" {
		entry := filepath.Join(dir, filepath.FromSlash(entryRel))
		if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(entry, []byte("export default () => {}"), 0o644); err != nil {
			t.Fatal(err)
		}
		return entry
	}
	return ""
}

func TestParseExtensionList(t *testing.T) {
	cases := map[string][]string{
		"":                          nil,
		"rtk":                       {"rtk"},
		"rtk,caveman,context-mode":  {"rtk", "caveman", "context-mode"},
		" rtk , caveman ":           {"rtk", "caveman"},
		"rtk,,caveman":              {"rtk", "caveman"},
		",":                         nil,
		"@tomooshi/caveman-milk-pi": {"@tomooshi/caveman-milk-pi"},
	}
	for raw, want := range cases {
		got := parseExtensionList(raw)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("parseExtensionList(%q) = %v, want %v", raw, got, want)
		}
	}
}

// TestResolveExtensionPaths_UsesDeclaredEntry: Pi's -e wants a file, not a
// package, so the resolver has to follow package.json "pi".extensions.
func TestResolveExtensionPaths_UsesDeclaredEntry(t *testing.T) {
	agent := t.TempDir()
	want := writePkg(t, agent, "pi-rtk-optimizer",
		`{"main":"./index.ts","pi":{"extensions":["./index.ts"]}}`, "index.ts")

	got := resolveExtensionPaths(agent, []string{"rtk"})
	if len(got) != 1 || got[0] != absOrSelf(want) {
		t.Errorf("resolveExtensionPaths = %v, want [%s]", got, absOrSelf(want))
	}
}

// TestResolveExtensionPaths_AliasAndScopedName: config carries friendly names;
// scoped npm names must also work directly.
func TestResolveExtensionPaths_AliasAndScopedName(t *testing.T) {
	agent := t.TempDir()
	writePkg(t, agent, "@tomooshi/caveman-milk-pi", `{"pi":{"extensions":["./dist/main.js"]}}`, "dist/main.js")

	for _, name := range []string{"caveman", "@tomooshi/caveman-milk-pi"} {
		got := resolveExtensionPaths(agent, []string{name})
		if len(got) != 1 {
			t.Errorf("resolveExtensionPaths(%q) = %v, want one path", name, got)
			continue
		}
		if !strings.HasSuffix(got[0], filepath.Join("dist", "main.js")) {
			t.Errorf("resolveExtensionPaths(%q) = %v, want the declared entry", name, got)
		}
	}
}

func TestResolveExtensionPaths_FallsBackToMain(t *testing.T) {
	agent := t.TempDir()
	writePkg(t, agent, "context-mode", `{"main":"./lib/entry.js"}`, "lib/entry.js")
	if got := resolveExtensionPaths(agent, []string{"context-mode"}); len(got) != 1 {
		t.Errorf("a package with only \"main\" must still resolve, got %v", got)
	}
}

// TestResolveExtensionPaths_SkipsUnresolvable is the fail-open contract: a
// mistyped or uninstalled extension costs that extension, not the bead.
func TestResolveExtensionPaths_SkipsUnresolvable(t *testing.T) {
	agent := t.TempDir()
	writePkg(t, agent, "pi-rtk-optimizer", `{"pi":{"extensions":["./index.ts"]}}`, "index.ts")
	// Declared entry file does not exist on disk.
	writePkg(t, agent, "pi-broken", `{"pi":{"extensions":["./missing.ts"]}}`, "")

	got := resolveExtensionPaths(agent, []string{"rtk", "not-installed", "pi-broken"})
	if len(got) != 1 {
		t.Errorf("expected only the resolvable extension, got %v", got)
	}
}

func TestResolveExtensionPaths_EmptyInputs(t *testing.T) {
	if got := resolveExtensionPaths("", []string{"rtk"}); got != nil {
		t.Errorf("no agent dir must yield no paths, got %v", got)
	}
	if got := resolveExtensionPaths(t.TempDir(), nil); got != nil {
		t.Errorf("no names must yield no paths, got %v", got)
	}
}

// TestExtensionArgs_DisablesDiscovery pins the flag shape that produced the
// measured saving: --no-extensions turns discovery off while explicit -e paths
// still load. Loading everything cost 48,315 tokens on one turn versus 12,059
// for rtk alone.
func TestExtensionArgs_DisablesDiscovery(t *testing.T) {
	if got := extensionArgs(nil); got != nil {
		t.Errorf("no paths must produce no flags (extensions stay off), got %v", got)
	}

	got := extensionArgs([]string{"/a/one.ts", "/b/two.js"})
	want := []string{"--no-extensions", "-e", "/a/one.ts", "-e", "/b/two.js"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("extensionArgs = %v, want %v", got, want)
	}
}

// TestBuildPiCommand_ExtensionsOnlyInRPCMode: text mode has no extension host,
// so the flags must not be emitted there — they would be pure noise.
func TestBuildPiCommand_ExtensionsOnlyInRPCMode(t *testing.T) {
	agent := t.TempDir()
	// Make the config dir a package root so resolvePiAgentDir picks it.
	if err := os.WriteFile(filepath.Join(agent, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePkg(t, agent, "pi-rtk-optimizer", `{"pi":{"extensions":["./index.ts"]}}`, "index.ts")

	bead := types.Bead{ID: types.BeadID("b1"), Title: "t", Type: "task"}

	rpcCmd := buildPiCommand(bead, PiConfig{
		Mode: "rpc", Provider: "p", Model: "m", ConfigDir: agent, Extensions: []string{"rtk"},
	}, t.TempDir(), nil)
	rpcArgs := strings.Join(rpcCmd.Args, " ")
	if !strings.Contains(rpcArgs, "--no-extensions") || !strings.Contains(rpcArgs, "-e ") {
		t.Errorf("rpc mode must load the allowlist, got: %s", rpcArgs)
	}

	textCmd := buildPiCommand(bead, PiConfig{
		Provider: "p", Model: "m", ConfigDir: agent, Extensions: []string{"rtk"},
	}, t.TempDir(), nil)
	if strings.Contains(strings.Join(textCmd.Args, " "), "--no-extensions") {
		t.Errorf("text mode has no extension host; flags must be omitted, got: %s", strings.Join(textCmd.Args, " "))
	}
}
