package validation

import "github.com/haulmatic-dev/hydra/pkg/types"

// beadEvidenceImplemented reports whether BeadEvidence and
// ValidateTDDChainEvidence carry real behavior. The contract tests consult it
// (via the BEAD_EVIDENCE_ENFORCE gate in the test helpers) to decide whether
// to skip loudly while the RED phase pins the contract. GREEN flips this to
// true and implements both entry points per
// docs/PRDs/validator-truth-prd-20261022.md FR-003.
const beadEvidenceImplemented = false

// Evidence is the per-bead commit/dirty evidence collected from the repo.
// Files holds repo-relative paths touched by every commit whose message
// contains the whole bead id, plus — when includeDirty was set — the dirty
// set of the repo (excluding .beads/). BlobIDs maps committed paths to their
// git blob ids. Fingerprint is sha256 over the sorted `path:blob-id` pairs.
type Evidence struct {
	Files       []string
	BlobIDs     map[string]string
	Fingerprint string
}

// ChainEvidenceInput carries the RED/GREEN pair, their located flags, their
// Evidence, and the repo root used to read file contents for assertion and
// stub checks.
type ChainEvidenceInput struct {
	RedBead       types.Bead
	GreenBead     types.Bead
	RedLocated    bool
	GreenLocated  bool
	RedEvidence   Evidence
	GreenEvidence Evidence
	RepoRoot      string
}

// TDDResult is the result of evidence-based chain validation. It aliases the
// existing TDDValidationResult so the close-path consumers stay compatible.
type TDDResult = TDDValidationResult

// BeadEvidence collects the evidence for one bead id inside repoRoot.
// RED-phase stub: always empty evidence, nil error (fail-open contract).
func BeadEvidence(repoRoot, beadID string, includeDirty bool) (Evidence, error) {
	_ = repoRoot
	_ = beadID
	_ = includeDirty
	return Evidence{}, nil
}

// ValidateTDDChainEvidence validates a RED/GREEN pair against their per-bead
// evidence. RED-phase stub: always passes, no gaps.
func ValidateTDDChainEvidence(in ChainEvidenceInput) TDDResult {
	_ = in
	return TDDResult{Passed: true, RedPassed: true, GreenPassed: true, Gaps: []TDDGap{}}
}
