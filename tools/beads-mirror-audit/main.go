// beads-mirror-audit compares a br bead store's issues.jsonl mirror against
// the live store reported by `br`, printing one line per divergence.
//
// Usage:
//
//	go run ./tools/beads-mirror-audit --store <dir> [--json]
//
// Exit codes:
//
//	0 - mirror clean (N beads)
//	1 - divergences found (see output)
//	2 - audit unavailable (br missing or br failure) — fail-open reporting
//
// This is a RED-phase inert stub: the audit contract is pinned by
// main_test.go; behavior is implemented in the GREEN phase.
package main

import (
	"fmt"
	"os"
)

// Divergence is one bead whose status differs between the jsonl mirror
// and the live store.
type Divergence struct {
	BeadID      string `json:"bead_id"`
	JSONLStatus string `json:"jsonl"`
	DBStatus    string `json:"db"`
}

// Report is the machine-readable audit result (--json shape).
type Report struct {
	Divergences []Divergence `json:"divergences"`
	Checked     int          `json:"checked"`
}

// exit codes per the contract.
const (
	exitClean   = 0
	exitDrift   = 1
	exitUnavail = 2
)

// auditUnavailablePrefix is the fail-open reporting line printed on br
// absence/failure.
const auditUnavailablePrefix = "audit unavailable: "

// enforceEnv gates the GREEN-behavior contract tests; set to 1 to fail
// the suite when the implementation is still missing (RED discipline).
const enforceEnv = "BEADS_MIRROR_AUDIT_ENFORCE"

// auditImplemented is the RED-phase gate: false while the tool is an
// inert stub. The GREEN phase flips it to true and implements runAudit.
const auditImplemented = false

// brRunner is the injectable seam the GREEN implementation uses to invoke
// `br` (tests substitute a fake double).
var brRunner = func(dir string, args []string) (stdout string, err error) {
	return "", fmt.Errorf("RED: beads mirror audit not implemented")
}

// runAudit compares <storeDir>/issues.jsonl against the live store via br
// and returns the report plus the process exit code
// (0 clean / 1 drift / 2 audit-unavailable). RED stub: never runs.
func runAudit(storeDir string, jsonOut bool) (Report, string, int) {
	return Report{}, "", exitUnavail
}

func main() {
	// RED stub: no behavior yet.
	if len(os.Args) > 1 && os.Args[1] == "--help" {
		fmt.Println("beads-mirror-audit --store <dir> [--json]")
	}
	os.Exit(exitUnavail)
}
