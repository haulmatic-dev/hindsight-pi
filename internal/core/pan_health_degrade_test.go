package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/internal/degrade"
)

// TestCollectHealthDigestDegradations (FR-032): the digest snapshots the
// fail-open degradation registry and renders a host line for any subsystem
// at 0% success within the window.
func TestCollectHealthDigestDegradations(t *testing.T) {
	tw := setupTestOverseer(t)
	o := tw.overseer
	degrade.Reset() // isolate the process-global registry

	// Healthy store scans: successes recorded, nothing degraded.
	d := o.collectHealthDigest()
	for _, s := range degrade.Snapshot() {
		if s.Name == "pan-store-scan" && s.Degraded() {
			t.Fatalf("healthy scan must not be degraded: %+v", s)
		}
	}
	if len(d.Degradations) != 0 {
		t.Fatalf("healthy fleet must render no degradation line, got %v", d.Degradations)
	}
	if strings.Contains(d.Text(), "degradations=") {
		t.Errorf("digest text must stay silent without degradations (PR #236 stable-digest rule): %q", d.Text())
	}

	// Drive pan-store-scan to 0% success over the minimum attempt count.
	degrade.Reset() // fresh window: only the failure streak inside it counts
	for i := 0; i < degrade.MinAttemptsForDegraded(); i++ {
		tw.mockBeads.listErr = errors.New(fmt.Sprintf("br list failure %d", i))
		o.collectHealthDigest()
	}
	d2 := o.collectHealthDigest()
	found := false
	for _, frag := range d2.Degradations {
		if strings.HasPrefix(frag, "pan-store-scan 0/") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected pan-store-scan degradation fragment, got %v", d2.Degradations)
	}
	if !strings.Contains(d2.Text(), "host: degradations=[pan-store-scan 0/") {
		t.Errorf("digest text missing degradation line: %q", d2.Text())
	}

	// Recovery: a successful scan clears the degradation next digest.
	tw.mockBeads.listErr = nil
	o.collectHealthDigest()
	d3 := o.collectHealthDigest()
	for _, frag := range d3.Degradations {
		if strings.HasPrefix(frag, "pan-store-scan 0/") {
			t.Fatalf("degradation should clear after recovery, got %v", d3.Degradations)
		}
	}
}

func TestDegradeWindowConfig(t *testing.T) {
	if degrade.DefaultWindow != 24*time.Hour {
		t.Fatalf("FR-032 default window must be 24h, got %s", degrade.DefaultWindow)
	}
}
