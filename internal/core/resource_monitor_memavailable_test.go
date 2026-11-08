package core

import (
	"errors"
	"testing"
)

// resource_monitor_memavailable_test.go — pins the absolute MemAvailable
// spawn floor (hydra-3ocq): a worker spawn is refused while host free memory
// is below the floor, even when the percentage check would pass, and probe
// errors fail open. The 2026-10-15 OOM kills (22.1G peak on 22G, 10 workers)
// outran a percentage-only gate because percentage ignores free headroom.

func TestResourceMonitor_MemAvailableFloor_BlocksSpawnBelowFloor(t *testing.T) {
	rm := NewResourceMonitor(0, 0) // percent checks disabled
	rm.SetMinMemAvailableMB(2500)
	rm.SetMemAvailableProbe(func() (int, error) { return 2000, nil })
	if rm.CanSpawnWorker() {
		t.Fatalf("spawn allowed with MemAvailable 2000MB below floor 2500MB — OOM guard did not fire")
	}
}

func TestResourceMonitor_MemAvailableFloor_AllowsSpawnAboveFloor(t *testing.T) {
	rm := NewResourceMonitor(0, 0)
	rm.SetMinMemAvailableMB(2500)
	rm.SetMemAvailableProbe(func() (int, error) { return 4000, nil })
	if !rm.CanSpawnWorker() {
		t.Fatalf("spawn blocked with MemAvailable 4000MB above floor 2500MB")
	}
}

func TestResourceMonitor_MemAvailableFloor_FailsOpenOnProbeError(t *testing.T) {
	rm := NewResourceMonitor(0, 0)
	rm.SetMinMemAvailableMB(2500)
	rm.SetMemAvailableProbe(func() (int, error) { return 0, errors.New("boom") })
	if !rm.CanSpawnWorker() {
		t.Fatalf("probe error must fail open (allow spawn), not wedge the fleet")
	}
}

func TestResourceMonitor_MemAvailableFloor_DisabledAtZeroOrNegative(t *testing.T) {
	for _, mb := range []int{0, -1} {
		rm := NewResourceMonitor(0, 0)
		rm.SetMinMemAvailableMB(mb)
		// nil probe would panic if the check ran — proves it is skipped
		rm.SetMemAvailableProbe(nil)
		if !rm.CanSpawnWorker() {
			t.Fatalf("floor %d must disable the check", mb)
		}
	}
}

func TestReadMemAvailableMB_ParsesProcMeminfo(t *testing.T) {
	// Real file on this host: must parse to a plausible positive value.
	mb, err := readMemAvailableMB()
	if err != nil {
		t.Fatalf("readMemAvailableMB on real /proc/meminfo: %v", err)
	}
	if mb <= 0 || mb > 4_000_000 {
		t.Fatalf("implausible MemAvailable %dMB", mb)
	}
}
