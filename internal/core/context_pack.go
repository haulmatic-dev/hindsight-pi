// context_pack.go builds per-chain "context packs": the mechanical
// orientation data workers otherwise burn their first ~8 turns discovering
// (measured 2026-07: 6,336 bash + 2,299 read calls vs 58 edits across 967
// sessions). Built just-in-time at chain first-member spawn, stored on every
// chain member's bead via `br update --agent-context`, refreshed SHA-keyed at
// each member spawn, injected into the worker prompt TAIL only (the frozen
// cache-stable head must not move).
//
// Fail-open everywhere: the pack is an optimization. Any build/write/refresh
// error logs and the spawn proceeds unguided. Events (pack_created,
// pack_refreshed, pack_miss) go to the fail-open event stream and never gate
// the pipeline.
package core

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ContextPackVersion is the pack schema version. Bump on breaking schema
// change; the refresh path compares BaseSHA, not Version.
const ContextPackVersion = 1

// contextPackProvenance marks packs this daemon wrote, so a refresh can
// distinguish its own packs from a human's hand-written agent_context.
const contextPackProvenance = "hydra-context-pack"

// PackFileAnchor maps a requirement term to the file that implements it.
// Populated best-effort from the graphify symbol index; confidence <1 means
// the match was substring/fuzzy, not an exact symbol hit.
type PackFileAnchor struct {
	Requirement string  `json:"requirement"`
	Path        string  `json:"path"`
	Confidence  float64 `json:"confidence"`
}

// ContextPack is the agent_context payload written to every chain member.
type ContextPack struct {
	Version      int              `json:"version"`
	BaseSHA      string           `json:"base_sha"`
	GateCommands []string         `json:"gate_commands"`
	SpecPattern  string           `json:"spec_pattern,omitempty"`
	TargetDirs   []string         `json:"target_dirs,omitempty"`
	Files        []PackFileAnchor `json:"files,omitempty"`
	Provenance   string           `json:"provenance"`
}

// ParseContextPack unmarshals a bead's AgentContext string. Returns an error
// for empty or non-pack JSON; callers treat that as "no pack" (fail-open).
func ParseContextPack(raw string) (*ContextPack, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var p ContextPack
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	if p.Version == 0 || p.Provenance == "" {
		return nil, nil // not one of ours (e.g. a human's agent_context)
	}
	return &p, nil
}

// packDirsSkipped are directory names never walked for spec patterns or
// target-dir listings.
var packDirsSkipped = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, "target": true, "coverage": true, ".next": true,
	".hydra-worktrees": true, ".beads": true, "var": true,
}

// BuildContextPack assembles the mechanical sections. It never fails: every
// section is best-effort, and a section that cannot be derived is omitted
// rather than blocking the spawn.
func BuildContextPack(projectPath string, gates *GateSet, requirementTerms []string) *ContextPack {
	pack := &ContextPack{
		Version:      ContextPackVersion,
		GateCommands: gateCommandList(gates),
		Provenance:   contextPackProvenance,
	}
	pack.BaseSHA = currentGitHead(projectPath)
	pack.SpecPattern = scanSpecPattern(projectPath)
	pack.TargetDirs = listTargetDirs(projectPath, 2)
	pack.Files = graphFileAnchors(projectPath, requirementTerms)
	return pack
}

// scanSpecPattern derives the repo's dominant test-file convention by
// counting candidate glob patterns. Returns "" when no convention reaches
// the minimum count (arbitrary files, no signal).
func scanSpecPattern(projectPath string) string {
	type family struct {
		pattern   string
		match     func(dir, base string) bool
		colocated func(stem string, srcStems map[string]bool) bool
	}
	stems := map[string]map[string]bool{} // dir -> stems of non-test source files
	families := []family{
		{"*_test.go", func(_, b string) bool { return strings.HasSuffix(b, "_test.go") }, func(s string, m map[string]bool) bool { return m[strings.TrimSuffix(s, "_test")] }},
		{"*_test.rs", func(_, b string) bool { return strings.HasSuffix(b, "_test.rs") }, func(s string, m map[string]bool) bool { return m[strings.TrimSuffix(s, "_test")] }},
		{"test_*.py", func(_, b string) bool { return strings.HasPrefix(b, "test_") && strings.HasSuffix(b, ".py") }, func(s string, m map[string]bool) bool { return m[strings.TrimPrefix(s, "test_")] }},
		{"*.spec.ts", func(_, b string) bool { return strings.HasSuffix(b, ".spec.ts") }, func(s string, m map[string]bool) bool { return m[strings.TrimSuffix(s, ".spec")] }},
		{"*.spec.js", func(_, b string) bool { return strings.HasSuffix(b, ".spec.js") }, func(s string, m map[string]bool) bool { return m[strings.TrimSuffix(s, ".spec")] }},
		{"*.test.ts", func(_, b string) bool { return strings.HasSuffix(b, ".test.ts") }, func(s string, m map[string]bool) bool { return m[strings.TrimSuffix(s, ".test")] }},
		{"*_spec.rb", func(_, b string) bool { return strings.HasSuffix(b, "_spec.rb") }, func(s string, m map[string]bool) bool { return m[strings.TrimSuffix(s, "_spec")] }},
	}
	// First pass: collect non-test source stems per dir (for colocation).
	_ = filepath.WalkDir(projectPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if packDirsSkipped[d.Name()] || strings.HasPrefix(d.Name(), ".") {
				if path != projectPath {
					return filepath.SkipDir
				}
			}
			return nil
		}
		base := d.Name()
		isTest := strings.Contains(base, "_test.") || strings.HasPrefix(base, "test_") ||
			strings.Contains(base, ".spec.") || strings.Contains(base, ".test.") ||
			strings.HasSuffix(base, "_spec.rb")
		if !isTest && !strings.HasPrefix(base, ".") {
			dir := filepath.Dir(path)
			stem := strings.TrimSuffix(base, filepath.Ext(base))
			if stems[dir] == nil {
				stems[dir] = map[string]bool{}
			}
			stems[dir][stem] = true
		}
		return nil
	})
	counts := make([]int, len(families))
	coloc := make([]int, len(families))
	_ = filepath.WalkDir(projectPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if packDirsSkipped[d.Name()] || strings.HasPrefix(d.Name(), ".") {
				if path != projectPath {
					return filepath.SkipDir
				}
			}
			return nil
		}
		dir, base := filepath.Dir(path), d.Name()
		for i, f := range families {
			if f.match(dir, base) {
				counts[i]++
				if f.colocated(strings.TrimSuffix(base, filepath.Ext(base)), stems[dir]) {
					coloc[i]++
				}
			}
		}
		return nil
	})
	best, bestCount := -1, 1 // need at least 2 hits to call it a convention
	for i, c := range counts {
		if c > bestCount {
			best, bestCount = i, c
		}
	}
	if best < 0 {
		return ""
	}
	p := families[best].pattern
	if coloc[best]*2 >= counts[best] { // majority colocated
		p += " (colocated next to source)"
	}
	return p
}

// listTargetDirs returns repo directories up to depth levels deep, relative
// to projectPath, sorted, capped. Skips hidden and vendored dirs.
func listTargetDirs(projectPath string, depth int) []string {
	var out []string
	walk := func(dir string, lvl int) {}
	walk = func(dir string, lvl int) {
		if lvl > depth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || packDirsSkipped[e.Name()] {
				continue
			}
			rel, err := filepath.Rel(projectPath, filepath.Join(dir, e.Name()))
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			out = append(out, rel)
			walk(filepath.Join(dir, e.Name()), lvl+1)
		}
	}
	walk(projectPath, 1)
	sort.Strings(out)
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

// graphFileAnchors maps requirement terms to files via the graphify symbol
// index. Fail-open: a missing or malformed graphify-out/graph.json yields
// nil (the section is omitted, never an error).
func graphFileAnchors(projectPath string, terms []string) []PackFileAnchor {
	if len(terms) == 0 {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(projectPath, "graphify-out", "graph.json"))
	if err != nil {
		return nil
	}
	// Tolerant decode: graphify versions differ in node shape, so pull every
	// string field that looks like a symbol name or a file path.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	type node struct {
		Name, Symbol, Kind, File, Path string
	}
	var nodes []node
	for _, key := range []string{"nodes", "symbols"} {
		var list []node
		if err := json.Unmarshal(raw[key], &list); err == nil && len(list) > 0 {
			nodes = append(nodes, list...)
		}
	}
	if len(nodes) == 0 {
		// Alternate shape: {"symbols": {"name": {"file": ...}}}
		var symMap map[string]map[string]string
		if err := json.Unmarshal(raw["symbols"], &symMap); err == nil {
			for name, m := range symMap {
				nodes = append(nodes, node{Name: name, File: m["file"], Path: m["path"]})
			}
		}
	}
	if len(nodes) == 0 {
		return nil
	}
	var anchors []PackFileAnchor
	seen := map[string]bool{}
	for _, term := range terms {
		t := strings.ToLower(strings.TrimSpace(term))
		if t == "" {
			continue
		}
		for _, n := range nodes {
			name := strings.ToLower(n.Name + n.Symbol)
			if name == "" {
				continue
			}
			conf := 0.0
			if name == t {
				conf = 1.0
			} else if strings.Contains(name, t) {
				conf = 0.6
			} else {
				continue
			}
			path := n.File
			if path == "" {
				path = n.Path
			}
			if path == "" || seen[term+"\x00"+path] {
				continue
			}
			seen[term+"\x00"+path] = true
			anchors = append(anchors, PackFileAnchor{Requirement: term, Path: path, Confidence: conf})
			break // best (first) match per term
		}
		if len(anchors) >= 20 {
			break
		}
	}
	return anchors
}

// ValidatePackAnchors re-validates agentic sections: every file anchor must
// still exist under projectPath. Broken anchors are dropped and returned so
// the caller can log one pack_miss event per drop.
func ValidatePackAnchors(pack *ContextPack, projectPath string) []PackFileAnchor {
	var dropped []PackFileAnchor
	kept := pack.Files[:0]
	for _, a := range pack.Files {
		if _, err := os.Stat(filepath.Join(projectPath, a.Path)); err == nil {
			kept = append(kept, a)
		} else {
			dropped = append(dropped, a)
		}
	}
	pack.Files = kept
	return dropped
}

// RequirementTermsForBead extracts identifier-like terms from a bead title
// for graph anchoring. CamelCase words and long lowercase tokens only —
// stopwords and short tokens match everything in a symbol index.
func RequirementTermsForBead(title string) []string {
	var terms []string
	for _, w := range strings.FieldsFunc(title, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_')
	}) {
		if len(w) >= 4 && w != strings.ToUpper(w) {
			terms = append(terms, w)
		}
	}
	if len(terms) > 8 {
		terms = terms[:8]
	}
	return terms
}

// logPackMiss emits one pack_miss event per dropped anchor and a summary
// line. Nil-sink-safe; never gates.
func logPackMiss(sink *EventSink, project, bead string, dropped []PackFileAnchor) {
	for _, a := range dropped {
		if sink != nil {
			sink.Emit(Event{Type: "pack_miss", Project: project, Bead: bead,
				Reason: "anchor_gone",
				Detail: "pack anchor dropped, file no longer exists: " + a.Path})
		}
		log.Printf("[ContextPack] pack_miss: anchor gone for bead %s: %s", bead, a.Path)
	}
}
