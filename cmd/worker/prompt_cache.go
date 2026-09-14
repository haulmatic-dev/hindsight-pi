package main

// Prompt cache measurement (2026-09-14): Kimi caches server-side by prefix
// hash, so the digests below are the unit of account for the prefix-
// stability programme. Logged per spawn with the bead id; when a
// multi-account router lands, the log line gains an account tag without a
// format break (the digest fields are account-agnostic).

import (
	"crypto/sha256"
	"encoding/hex"
)

// promptHeadBytes is the head window hashed for the cross-bead stability
// metric: large enough to cover the invariant doctrine blocks, small enough
// to compare cheaply.
const promptHeadBytes = 8192

// promptPrefixDigest returns sha256 over the whole prompt and over the
// first promptHeadBytes bytes. Both are pure functions of the input, so
// identical prompts always yield identical digests.
func promptPrefixDigest(prompt string) (full, head string) {
	sum := sha256.Sum256([]byte(prompt))
	full = hex.EncodeToString(sum[:])
	if len(prompt) <= promptHeadBytes {
		return full, full
	}
	hs := sha256.Sum256([]byte(prompt[:promptHeadBytes]))
	return full, hex.EncodeToString(hs[:])
}
