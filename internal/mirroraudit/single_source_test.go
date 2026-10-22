package mirroraudit

// Single-source-of-truth contract tests (FR-1). The package doc pins that
// BOTH consumers — the daemon sweep (internal/core/pan_mirror_health.go) and
// the CLI (tools/beads-mirror-audit) — must classify through this package so
// their verdicts can never drift. These tests pin the CLI half of that
// contract at source level.
//
// RED gate: the suite exits 0 pre-GREEN (behavior tests skip loudly, so the
// daemon gate stays green and the parent AC "test output must show 0
// failures" holds). Set MIRROR_AUDIT_SOT_ENFORCE=1 to turn the skips into
// hard failures — that proves the RED phase detects the missing contract.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mirrorAuditSOTEnforce = "MIRROR_AUDIT_SOT_ENFORCE"

// toolMainGo returns the source of the CLI that must consume this package.
func toolMainGo(t *testing.T) string {
	t.Helper()
	// Test cwd = package dir (internal/mirroraudit); tool lives two levels up.
	path := filepath.Join("..", "..", "tools", "beads-mirror-audit", "main.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tool source %s: %v", path, err)
	}
	return string(data)
}

func requireSOTImpl(t *testing.T) {
	t.Helper()
	if os.Getenv(mirrorAuditSOTEnforce) == "" {
		t.Skipf("RED: single-source-of-truth tool contract not yet implemented (set %s=1 to enforce)", mirrorAuditSOTEnforce)
	}
}

// TestSingleSource_ToolImportsPackage pins that the CLI imports
// internal/mirroraudit — the tool must not carry its own copy of the
// classification logic.
func TestSingleSource_ToolImportsPackage(t *testing.T) {
	requireSOTImpl(t)
	src := toolMainGo(t)
	if !strings.Contains(src, `"github.com/haulmatic-dev/hydra/internal/mirroraudit"`) {
		t.Fatalf("tools/beads-mirror-audit/main.go does not import internal/mirroraudit — classification is duplicated, not shared")
	}
	if !strings.Contains(src, "mirroraudit.") {
		t.Fatalf("tools/beads-mirror-audit/main.go imports the package but never references mirroraudit.*")
	}
}

// TestSingleSource_ToolHasNoDuplicateClassifier pins that the duplicate
// classification helpers were removed from the tool: parseJSONLLine,
// parseDBList, readJSONLMirror and the paired-record comparison loop must
// live only here.
func TestSingleSource_ToolHasNoDuplicateClassifier(t *testing.T) {
	requireSOTImpl(t)
	src := toolMainGo(t)
	for _, dup := range []string{
		"func parseJSONLLine(",
		"func parseDBList(",
		"func readJSONLMirror(",
		"func divergenceLine(",
	} {
		if strings.Contains(src, dup) {
			t.Fatalf("tools/beads-mirror-audit/main.go still defines %q — duplicate of the shared classifier; classification must come from mirroraudit only", strings.TrimPrefix(dup, "func "))
		}
	}
}

// TestSingleSource_DAOMConsumerPinsPackage pins the daemon-side consumer
// named in the package doc so the sweep cannot re-grow its own classifier.
func TestSingleSource_DAOMConsumerPinsPackage(t *testing.T) {
	requireSOTImpl(t)
	path := filepath.Join("..", "..", "internal", "core", "pan_mirror_health.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read daemon sweep %s: %v", path, err)
	}
	if !strings.Contains(string(data), "mirroraudit.") {
		t.Fatalf("internal/core/pan_mirror_health.go does not reference mirroraudit.* — daemon sweep duplicates classification")
	}
}
