package core

import (
	"os"
	"strings"
	"testing"
)

// RED gate mirroring staleFilingRequireImpl: while the audit-sweep GREEN is
// absent these tests skip loudly; MIRROR_HEALTH_ENFORCE=1 fails them.
func mirrorHealthRequireImpl(t *testing.T) {
	t.Helper()
	if !mirrorResyncImplemented && os.Getenv("MIRROR_HEALTH_ENFORCE") == "" {
		t.Skip("RED: mirror-health audit sweep contract not yet implemented (set MIRROR_HEALTH_ENFORCE=1 to fail)")
	}
}

// FR-2 contract pin: pan_mirror_health.go imports the shared FR-1
// mirroraudit package (single source of truth — no second parser), models the
// sweep on the pan_stale_filing interval-throttle pattern, and names the
// config knobs mirror_health.enabled / mirror_health.audit_interval.
func TestMirrorHealthSweep_UsesSharedAuditPackage(t *testing.T) {
	data, err := os.ReadFile("pan_mirror_health.go")
	if err != nil {
		t.Fatalf("read contract file: %v", err)
	}
	src := string(data)
	if !strings.Contains(src, "\"github.com/haulmatic-dev/hydra/internal/mirroraudit\"") {
		t.Fatalf("pan_mirror_health.go must import internal/mirroraudit (FR-1 single source of truth)")
	}
	if !strings.Contains(src, "mirror_drift_detected") || !strings.Contains(src, "mirror_audit_clean") {
		t.Fatalf("sweep must emit the typed events mirror_drift_detected and mirror_audit_clean")
	}
	if !strings.Contains(src, "audit_interval") {
		t.Fatalf("sweep must reference the mirror_health.audit_interval knob")
	}
}
