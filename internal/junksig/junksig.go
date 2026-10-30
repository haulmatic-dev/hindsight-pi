// Package junksig classifies repository paths as build-tool junk using
// structural signatures — path-segment shapes — never substrings.
//
// It exists because the clean-hands incident (docs/PRDs/clean-hands-prd-20261029.md)
// saw thousands of jest/cache scratch artifacts committed under ad-hoc paths
// (src/src/.tmp-jest/**, src/.jest-cache/**, src/.tmp/jest-cache/, root
// .tmp-* files), while real source files with "cache" in their names
// (thread-cache.service.ts and friends) must never be touched.
//
// RED-phase inert stub: the contract tests in junksig_test.go skip loudly
// until the GREEN phase lands the real matcher (const flag below, enforced
// under JUNKSIG_ENFORCE=1).
package junksig

// junksigImplemented gates the RED-phase contract tests. It stays false
// until GREEN implements the matcher, so `go test ./...` stays green for
// the daemon gate (tests skip loudly) while JUNKSIG_ENFORCE=1 makes every
// contract test fail loudly — proving RED detects the missing feature.
const junksigImplemented = false

// IsJunkPath reports whether p is a junk artifact path.
//
// Contract (pinned by TestJunksig_* in junksig_test.go): structural
// signatures only — any path segment starting with ".tmp" (.tmp, .tmp-jest,
// .tmp2 …), the directory segment ".jest-cache", basenames with the
// "haste-map-" or "jest-transform-cache-" prefix, the exact basename
// ".hydra-preflight-ok", and everything under the root ".pi/task-breakdown/"
// tree. Real source files whose names merely contain "cache" or ".tmp" as a
// suffix (thread-cache.service.ts, template.tmp) must NOT match.
//
// RED stub: inert, always false.
func IsJunkPath(p string) bool {
	_ = p
	return false
}

// IgnorePatterns returns the .gitignore lines exactly equivalent to
// IsJunkPath: for every path p, git check-ignore must ignore p if and only
// if IsJunkPath(p) is true (pinned by TestJunksig_IgnorePatternsReproduceMatcher).
//
// RED stub: empty.
func IgnorePatterns() []string {
	return nil
}
