package pan

import (
	"strings"
	"testing"
)

// TestHealthDigestTextDegradations (FR-032): the digest renders a host
// degradation line when the fail-open counter snapshot has a degraded
// subsystem, and stays silent otherwise (PR #236 stable-digest rule).
func TestHealthDigestTextDegradations(t *testing.T) {
	d := HealthDigest{Degradations: []string{"pan-store-scan 0/4"}}
	out := d.Text()
	if !strings.Contains(out, "host: degradations=[pan-store-scan 0/4]") {
		t.Errorf("missing degradation line: %q", out)
	}
	if !strings.Contains(out, "0% success") {
		t.Errorf("degradation line should explain the condition: %q", out)
	}

	empty := HealthDigest{}
	if strings.Contains(empty.Text(), "degradations=") {
		t.Errorf("no degradations must render no line: %q", empty.Text())
	}
}
