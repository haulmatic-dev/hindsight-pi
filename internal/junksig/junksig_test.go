package junksig

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// junksigRequireImpl enforces the RED-phase contract: every behavior test
// skips loudly by default (daemon suite stays green pre-GREEN) and fails
// under JUNKSIG_ENFORCE=1 — the failure reason is the missing feature.
func junksigRequireImpl(t *testing.T) {
	t.Helper()
	if junksigImplemented {
		return
	}
	if os.Getenv("JUNKSIG_ENFORCE") == "1" {
		t.Fatal("RED: junk signature contract not yet implemented (IsJunkPath/IgnorePatterns are inert stubs)")
	}
	t.Skip("RED: junk signature contract not yet implemented — set JUNKSIG_ENFORCE=1 to fail loudly")
}

// junkFixture is one probe path plus the classification the matcher must
// return. Shared by the matcher tables and the git check-ignore property
// test (as a filesystem fixture).
type junkFixture struct {
	path string
	junk bool
	why  string
}

// observedIncidentJunk is every real-world junk path shape observed in the
// clean-hands incident (docs/PRDs/clean-hands-prd-20261029.md: 2,407 files
// under src/src/.tmp-jest/**, 620 under src/.jest-cache/**, 198 under
// src/.tmp/jest-cache/, plus the ad-hoc root .tmp-* scaffolding).
func observedIncidentJunk() []junkFixture {
	return []junkFixture{
		{path: "src/src/.tmp-jest/r3/cache/haste-map-9f2c1a", junk: true, why: "incident: .tmp-jest segment + haste-map basename"},
		{path: "src/.jest-cache/0a/cache.dat", junk: true, why: "incident: .jest-cache directory anywhere"},
		{path: "src/.tmp/jest-cache/transform-1.js", junk: true, why: "incident: .tmp segment (src/.tmp/jest-cache/)"},
		{path: ".tmp-cov-full.txt", junk: true, why: "incident: root .tmp-* file"},
		{path: ".tmp-full-retry.sh", junk: true, why: "incident: root .tmp-* file"},
		{path: ".tmp2/jest/worker.js", junk: true, why: "incident: .tmp2 segment"},
		{path: ".hydra-preflight-ok", junk: true, why: "incident: hydra preflight marker"},
		{path: ".pi/task-breakdown/cli-x/log.jsonl", junk: true, why: "incident: .pi/task-breakdown tree"},
	}
}

// negativeProbes are load-bearing: real repo source files and suffix-shaped
// paths that must NEVER be classified as junk. Every path merely containing
// "cache" or ".tmp" outside a junk segment position lands here.
func negativeProbes() []junkFixture {
	return []junkFixture{
		{path: "src/modules/agent/services/conversation/thread-cache.service.ts", junk: false, why: "real live-repo source file"},
		{path: "src/modules/agent/services/conversation/thread-cache.service.spec.ts", junk: false, why: "real live-repo spec file"},
		{path: "src/cache/redis.module.ts", junk: false, why: "cache-named source dir under src/"},
		{path: "src/modules/x/data-cache.service.ts", junk: false, why: "cache-named source file under src/"},
		{path: "src/utils/cache.ts", junk: false, why: "generic cache-named source file"},
		{path: "template.tmp", junk: false, why: "'.tmp' only as a suffix — structural match must be segment-prefix"},
		{path: "snapshot.tmp.png", junk: false, why: "'.tmp' inside the segment, not leading it"},
		{path: "src/pipeline/task-breakdown/step.ts", junk: false, why: "task-breakdown without the .pi root is real source"},
		{path: ".pi/keep-config.json", junk: false, why: ".pi files outside task-breakdown are not junk"},
	}
}

// TestJunksig_RealWorldIncidentPaths pins AC1: every observed real-world
// junk path from the incident matches.
func TestJunksig_RealWorldIncidentPaths(t *testing.T) {
	junksigRequireImpl(t)
	for _, f := range observedIncidentJunk() {
		if !IsJunkPath(f.path) {
			t.Errorf("IsJunkPath(%q) = false, want true (%s)", f.path, f.why)
		}
	}
}

// TestJunksig_NegativeProbes pins AC2: the thread-cache.service.ts family
// and every *cache*-named source file under src/ must NOT match.
func TestJunksig_NegativeProbes(t *testing.T) {
	junksigRequireImpl(t)
	for _, f := range negativeProbes() {
		if IsJunkPath(f.path) {
			t.Errorf("IsJunkPath(%q) = true, want false (%s)", f.path, f.why)
		}
	}
}

// TestJunksig_StructuralNotSubstring pins the core signature property:
// matching is on path-segment structure, not substrings anywhere in the path.
func TestJunksig_StructuralNotSubstring(t *testing.T) {
	junksigRequireImpl(t)
	notJunk := []string{
		"my.jest-cache.ts",         // segment contains ".jest-cache" but is not the directory signature
		"src/thread-cache/keep.go", // segment "thread-cache" is not ".jest-cache"
		"report.tmp2.final.md",     // ".tmp" inside a segment, not a segment prefix
	}
	for _, p := range notJunk {
		if IsJunkPath(p) {
			t.Errorf("IsJunkPath(%q) = true, want false (substring, not structural)", p)
		}
	}
	junk := []string{
		".tmpz/scratch.bin", // ".tmpz" is a ".tmp*" segment per the signature
		".tmp-x/y",          // ".tmp-x" segment
	}
	for _, p := range junk {
		if !IsJunkPath(p) {
			t.Errorf("IsJunkPath(%q) = false, want true (.tmp* segment prefix)", p)
		}
	}
}

// TestJunksig_RenamedParentStillMatches pins AC3: patterns survive directory
// renames of the same content — foo/.jest-cache/x matches wherever it sits.
func TestJunksig_RenamedParentStillMatches(t *testing.T) {
	junksigRequireImpl(t)
	for _, p := range []string{
		"renamed/.jest-cache/blob",
		"renamed/.tmp-jest/b",
		"foo/bar/.tmp/deep.log",
		"a/b/c/.jest-cache/0f/entry",
		"anywhere/.hydra-preflight-ok",
	} {
		if !IsJunkPath(p) {
			t.Errorf("IsJunkPath(%q) = false, want true (renamed parent must not defeat the signature)", p)
		}
	}
}

// TestJunksig_BasenameSignatures pins the basename-prefix signatures
// haste-map-* and jest-transform-cache-* and their precision.
func TestJunksig_BasenameSignatures(t *testing.T) {
	junksigRequireImpl(t)
	junk := []string{
		"x/haste-map-abc123",
		"src/.jest-cache/0a/haste-map-def",
		"jest-transform-cache-77ff.bin",
		"deep/a/b/jest-transform-cache-99/entry",
	}
	for _, p := range junk {
		if !IsJunkPath(p) {
			t.Errorf("IsJunkPath(%q) = false, want true (basename signature)", p)
		}
	}
	notJunk := []string{
		"haste-map",            // prefix is "haste-map-", bare "haste-map" is not a signature
		"src/haste-map.md",     // hmm: basename "haste-map.md" lacks the "-" right after the prefix
		"myhaste-map-x.ts",     // prefix must start the basename
		"jest-transform-cache", // bare name without the trailing dash
	}
	for _, p := range notJunk {
		if IsJunkPath(p) {
			t.Errorf("IsJunkPath(%q) = true, want false (basename signature must be the exact prefix haste-map- / jest-transform-cache-)", p)
		}
	}
}

// TestJunksig_HydraPreflightExact pins the exact-basename signature
// .hydra-preflight-ok (any depth, exact name).
func TestJunksig_HydraPreflightExact(t *testing.T) {
	junksigRequireImpl(t)
	junk := []string{".hydra-preflight-ok", "nested/.hydra-preflight-ok"}
	for _, p := range junk {
		if !IsJunkPath(p) {
			t.Errorf("IsJunkPath(%q) = false, want true (exact marker basename)", p)
		}
	}
	notJunk := []string{".hydra-preflight-ok.bak", "hydra-preflight-ok"}
	for _, p := range notJunk {
		if IsJunkPath(p) {
			t.Errorf("IsJunkPath(%q) = true, want false (signature is the exact basename .hydra-preflight-ok)", p)
		}
	}
}

// TestJunksig_PiTaskBreakdownPrefix pins the .pi/task-breakdown tree
// signature.
func TestJunksig_PiTaskBreakdownPrefix(t *testing.T) {
	junksigRequireImpl(t)
	junk := []string{
		".pi/task-breakdown/cli-x/log.jsonl",
		".pi/task-breakdown/x",
	}
	for _, p := range junk {
		if !IsJunkPath(p) {
			t.Errorf("IsJunkPath(%q) = false, want true (.pi/task-breakdown tree)", p)
		}
	}
}

// TestJunksig_IgnorePatternsReproduceMatcher pins AC4: the IgnorePatterns()
// lines exactly reproduce the matcher — verified behaviorally in a hermetic
// repo (t.TempDir + git init + .gitignore = IgnorePatterns() + git
// check-ignore on every fixture) rather than by string equality.
func TestJunksig_IgnorePatternsReproduceMatcher(t *testing.T) {
	junksigRequireImpl(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not in PATH: %v", err)
	}
	patterns := IgnorePatterns()
	if len(patterns) == 0 {
		t.Fatal("IgnorePatterns() returned no lines — must return the .gitignore equivalent of the matcher")
	}

	dir := t.TempDir()
	gitOut := func(args ...string) int {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		// Hermetic: host global/system git config (core.excludesFile!) must
		// not leak into the verdicts.
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0")
		var buf bytes.Buffer
		cmd.Stdout, cmd.Stderr = &buf, &buf
		if err := cmd.Run(); err != nil {
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("git %v: %v (%s)", args, err, strings.TrimSpace(buf.String()))
			}
			t.Logf("git %v -> exit %d: %s", args, ee.ExitCode(), strings.TrimSpace(buf.String()))
			return ee.ExitCode()
		}
		return 0
	}
	if rc := gitOut("init", "-q"); rc != 0 {
		t.Fatalf("git init failed, rc=%d", rc)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(strings.Join(patterns, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	fixtures := append(append([]junkFixture{}, observedIncidentJunk()...), negativeProbes()...)
	fixtures = append(fixtures,
		junkFixture{path: "renamed/.jest-cache/blob", junk: true, why: "renamed parent"},
		junkFixture{path: "renamed/.tmp-jest/b", junk: true, why: "renamed parent"},
		junkFixture{path: "x/haste-map-abc123", junk: true, why: "basename signature"},
		junkFixture{path: "nested/.hydra-preflight-ok", junk: true, why: "exact marker at depth"},
	)
	for _, f := range fixtures {
		if f.junk {
			p := filepath.Join(dir, filepath.FromSlash(f.path))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatalf("mkdir for %s: %v", f.path, err)
			}
			if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
				t.Fatalf("write %s: %v", f.path, err)
			}
		}
	}
	for _, f := range fixtures {
		rc := gitOut("check-ignore", "-q", "--", f.path)
		if rc != 0 && rc != 1 {
			t.Fatalf("git check-ignore %q: unexpected rc=%d", f.path, rc)
		}
		if got, want := rc == 0, IsJunkPath(f.path); got != want {
			t.Errorf("path %q: git check-ignore=%v but IsJunkPath=%v — IgnorePatterns() must exactly reproduce the matcher (%s)",
				f.path, got, want, f.why)
		}
	}
}

// TestJunksig_CoverageResidualNotSignature records the accepted residual
// from the PRD: jest's default coverage/ output dir is deliberately NOT a
// signature (some repos commit lcov/coverage artifacts); coverage routing is
// handled by scratch guidance, not this gate.
func TestJunksig_CoverageResidualNotSignature(t *testing.T) {
	junksigRequireImpl(t)
	for _, p := range []string{"coverage/lcov.info", "coverage/coverage-summary.json", "src/coverage/keep.go"} {
		if IsJunkPath(p) {
			t.Errorf("IsJunkPath(%q) = true, want false (accepted residual: coverage/ is deliberately not a signature)", p)
		}
	}
}

// TestJunksig_IgnorePatternsWellFormed pins guardrails on the ignore-line
// contract: non-empty, no blank lines, no negations (a "!" line would flip
// verdicts away from the matcher).
func TestJunksig_IgnorePatternsWellFormed(t *testing.T) {
	junksigRequireImpl(t)
	patterns := IgnorePatterns()
	if len(patterns) == 0 {
		t.Fatal("IgnorePatterns() is empty — must carry the matcher's .gitignore equivalent")
	}
	for i, line := range patterns {
		if strings.TrimSpace(line) == "" {
			t.Errorf("IgnorePatterns()[%d] is blank", i)
		}
		if strings.HasPrefix(strings.TrimSpace(line), "!") {
			t.Errorf("IgnorePatterns()[%d] = %q is a negation — patterns must only ever add matches", i, line)
		}
	}
}
