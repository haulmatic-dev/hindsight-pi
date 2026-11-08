// Package junksig classifies repository paths as build-tool junk using
// structural signatures — path-segment shapes — never substrings.
//
// It exists because the clean-hands incident (docs/PRDs/clean-hands-prd-20261029.md)
// saw thousands of jest/cache scratch artifacts committed under ad-hoc paths
// (src/src/.tmp-jest/**, src/.jest-cache/**, src/.tmp/jest-cache/, root
// .tmp-* files), while real source files with "cache" in their names
// (thread-cache.service.ts and friends) must never be touched.
package junksig

import (
	"path/filepath"
	"strings"
)

// junksigImplemented gates the RED-phase contract tests. GREEN flipped it
// to true: the matcher below is real and every TestJunksig_* contract test
// now runs (and passes) in the default daemon gate.
const junksigImplemented = true

// ignoreLinePrefix is the gitignore idiom that matches a basename pattern at
// ANY depth (including the repo root): a leading "**/" component matches
// zero or more directories, so "**/.tmp*" covers both ".tmp-cov-full.txt"
// at the root and "src/.tmp2/jest/worker.js" deep in the tree.
const ignoreLinePrefix = "**/"

// junkExactSegments are signatures matched by exact segment equality.
var junkExactSegments = map[string]bool{
	".jest-cache":         true, // jest cache dir
	".hydra-preflight-ok": true, // hydra preflight marker, any depth
}

// junkSegmentPrefixes are signatures matched as a segment (or basename)
// prefix — structural prefix, never a substring anywhere in the name.
var junkSegmentPrefixes = []string{
	".tmp",                  // scratch segments: .tmp, .tmp-jest, .tmp2, .tmp-* files
	"haste-map-",            // haste cache entry basenames
	"jest-transform-cache-", // jest-transform cache entry basenames
}

// isJunkSegment reports whether the segment at index i of segs matches a
// junk signature, including the two-segment ".pi/task-breakdown/<anything>"
// tree rule (which needs lookahead into the next segments).
func isJunkSegment(segs []string, i int) bool {
	s := segs[i]
	for _, prefix := range junkSegmentPrefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	if junkExactSegments[s] {
		return true
	}
	// pi task-breakdown scratch tree: ".pi/task-breakdown/<anything>".
	return s == ".pi" && i+1 < len(segs) && segs[i+1] == "task-breakdown" && len(segs) > i+2
}

// IsJunkPath reports whether p is a junk artifact path.
//
// Contract (pinned by TestJunksig_* in junksig_test.go): structural
// signatures only, evaluated per path segment (the basename counts as the
// last segment) — never substrings:
//
//   - any segment starting with ".tmp" (.tmp, .tmp-jest, .tmp2, .tmpz, and
//     root scratch files like .tmp-cov-full.txt);
//   - the directory segment ".jest-cache" exactly;
//   - basenames starting with "haste-map-" or "jest-transform-cache-";
//   - the exact basename ".hydra-preflight-ok" at any depth;
//   - everything strictly under a ".pi/task-breakdown/" tree.
//
// Real source files whose names merely contain "cache" or carry ".tmp" as a
// suffix (thread-cache.service.ts, template.tmp, snapshot.tmp.png) must NOT
// match. jest's default coverage/ output dir is deliberately NOT a
// signature (accepted residual — some repos commit coverage artifacts);
// coverage routing is handled by scratch guidance, not this gate.
func IsJunkPath(p string) bool {
	if p == "" {
		return false
	}
	segs := strings.Split(filepath.ToSlash(p), "/")
	for i, s := range segs {
		if s != "" && isJunkSegment(segs, i) {
			return true
		}
	}
	return false
}

// IgnorePatterns returns the .gitignore lines exactly equivalent to
// IsJunkPath: for every path p, git check-ignore must ignore p if and only
// if IsJunkPath(p) is true (pinned by TestJunksig_IgnorePatternsReproduceMatcher
// in a hermetic repo with GIT_CONFIG_GLOBAL/SYSTEM=/dev/null).
//
// Every line is a bare add (no negations, no blanks), and the "**/" prefix
// makes each basename signature depth-independent — which is what makes the
// lines survive directory renames the same way the matcher does.
func IgnorePatterns() []string {
	return []string{
		ignoreLinePrefix + ".tmp*",
		ignoreLinePrefix + ".jest-cache",
		ignoreLinePrefix + "haste-map-*",
		ignoreLinePrefix + "jest-transform-cache-*",
		ignoreLinePrefix + ".hydra-preflight-ok",
		// "everything strictly under the tree": "/**" matches any depth
		// below, never the ".pi/task-breakdown" dir itself — mirroring the
		// matcher, which requires a segment after task-breakdown.
		ignoreLinePrefix + ".pi/task-breakdown/**",
	}
}
