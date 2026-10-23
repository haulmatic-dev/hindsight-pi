// Package beadfiles resolves the target files a bead declares. It is the
// shared, single-source resolver intended to back the close-path scope gate,
// the spawn scope check and the gate scoping logic so they can never drift.
// Adoption across internal/core is a later bead; for now nothing else imports
// it.
package beadfiles

import (
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// Resolve extracts a bead's declared target files. It reads
// Metadata["files"], then Metadata["target_files"], then a "Files:" line in
// the description. Returns nil when none are declared (caller then checks
// the whole repo).
//
// This mirrors internal/core's beadTargetFiles byte-for-byte:
//   - Metadata "files" has precedence over Metadata "target_files".
//   - Metadata is only consulted with a non-blank whitespace-trimmed value.
//   - Description fallback: the first line whose lowercase form starts with
//     "files:" — the remainder is passed to SplitFileList.
func Resolve(bead *types.Bead) []string {
	if bead == nil {
		return nil
	}
	if bead.Metadata != nil {
		// Workers and validators populate different keys; reconcile both
		// so the close-path gate never misses declared targets.
		for _, key := range []string{"files", "target_files"} {
			if v, ok := bead.Metadata[key]; ok && strings.TrimSpace(v) != "" {
				return SplitFileList(v)
			}
		}
	}
	for _, line := range strings.Split(bead.Description, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(l), "files:") {
			return SplitFileList(l[len("files:"):])
		}
	}
	return nil
}

// SplitFileList splits a comma-, semicolon-, space- or tab-separated file
// list into its non-empty entries, skipping entries equal to "TBD"
// (case-insensitive), which task templates use as a placeholder.
func SplitFileList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	}) {
		p = strings.TrimSpace(p)
		if p != "" && !strings.EqualFold(p, "TBD") {
			out = append(out, p)
		}
	}
	return out
}
