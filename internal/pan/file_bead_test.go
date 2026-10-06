package pan

import (
	"strings"
	"testing"
)

func newFileBeadShepherd(t *testing.T) (*Shepherd, *[]string) {
	t.Helper()
	store := &fakeStore{}
	s := New(Config{Enabled: true}, store, nil)
	var filed []string
	s.SetBeadFiler(func(beadID, title, description string) error {
		filed = append(filed, beadID+"|"+title)
		return nil
	})
	return s, &filed
}

func TestFileBeadFilesOncePerSignature(t *testing.T) {
	s, filed := newFileBeadShepherd(t)
	s.handleFileBead("bead-a", "Fix scheduler wedge\nDetails here")
	s.handleFileBead("bead-b", "Fix scheduler wedge\nDetails here")
	if len(*filed) != 1 {
		t.Fatalf("filed %d beads, want 1 (signature dedupe): %v", len(*filed), *filed)
	}
}

func TestFileBeadDifferentTitleFiles(t *testing.T) {
	s, filed := newFileBeadShepherd(t)
	s.handleFileBead("bead-a", "Fix scheduler wedge\nDetails")
	s.handleFileBead("bead-a", "Fix publisher wedge\nDetails")
	if len(*filed) != 2 {
		t.Fatalf("filed %d beads, want 2: %v", len(*filed), *filed)
	}
}

func TestFileBeadDedupesPerProject(t *testing.T) {
	s, filed := newFileBeadShepherd(t)
	s.SetProjectScope(func(beadID string) string {
		if strings.HasSuffix(beadID, "-campaign-anomaly") {
			return "/proj/webapp"
		}
		return beadID
	})
	s.handleFileBead("webapp-campaign-anomaly", "Investigate chain discipline\nDetails")
	// Same project, different pseudo ID suffix path but same normalized title.
	s.handleFileBead("webapp-campaign-anomaly", "Investigate chain discipline\nMore detail")
	if len(*filed) != 1 {
		t.Fatalf("filed %d beads, want 1 (per-project dedupe): %v", len(*filed), *filed)
	}
}

func TestFileBeadNormalizesVolatileTitle(t *testing.T) {
	s, filed := newFileBeadShepherd(t)
	s.handleFileBead("bead-a", "Worker hung for 94h27m")
	s.handleFileBead("bead-a", "Worker hung for 12h3m")
	if len(*filed) != 1 {
		t.Fatalf("filed %d beads, want 1 (volatile duration normalized): %v", len(*filed), *filed)
	}
}

func TestFileBeadNoFilerEmitsError(t *testing.T) {
	store := &fakeStore{}
	s := New(Config{Enabled: true}, store, nil)
	var events []string
	s.OnRepairEvent = func(beadID, action, result, detail string) {
		events = append(events, action+"/"+result)
	}
	s.handleFileBead("bead-a", "Title\nDesc")
	if len(events) != 1 || events[0] != "file-bead/error" {
		t.Fatalf("events %v, want [file-bead/error]", events)
	}
}

func TestFileBeadEmptyPayloadRejected(t *testing.T) {
	s, filed := newFileBeadShepherd(t)
	var events []string
	s.OnRepairEvent = func(beadID, action, result, detail string) {
		events = append(events, action+"/"+result)
	}
	s.handleFileBead("bead-a", "   ")
	if len(*filed) != 0 {
		t.Fatalf("filed %d beads, want 0", len(*filed))
	}
	if len(events) != 1 || events[0] != "file-bead/error" {
		t.Fatalf("events %v, want [file-bead/error]", events)
	}
}

func TestFileBeadFilerErrorDropsSignature(t *testing.T) {
	s, _ := newFileBeadShepherd(t)
	fail := true
	s.SetBeadFiler(func(beadID, title, description string) error {
		if fail {
			return errTestFilerDown
		}
		return nil
	})
	s.handleFileBead("bead-a", "Same title")
	fail = false
	s.handleFileBead("bead-a", "Same title")
	// Signature was dropped on error, so the retry files.
	if n := len(s.filedSigs); n != 1 {
		t.Fatalf("filedSigs %d, want 1", n)
	}
}

func TestFileBeadAllowedAction(t *testing.T) {
	if !isAllowedAction(ActFileBead) {
		t.Fatal("file-bead must be an allowed action")
	}
}

func TestRepairWhitelistShrunkToHygieneSet(t *testing.T) {
	for _, name := range []string{"repair-worktree", "reap-placeholder", "reset-retries", "repair-bead-store", "clear-hard-park", "clear-escalation"} {
		if !repairWhitelist[name] {
			t.Errorf("hygiene verb %q missing from whitelist", name)
		}
	}
	for _, name := range []string{"park-campaign", "unpark-campaign", "log-improvement", "integrate-stranded", "re-push-integration", "prune-worktree-registration"} {
		if repairWhitelist[name] {
			t.Errorf("non-hygiene verb %q still whitelisted", name)
		}
	}
}

var errTestFilerDown = &filerError{"filer down"}

type filerError struct{ msg string }

func (e *filerError) Error() string { return e.msg }

func TestFileBeadDuplicateFilerResultSkips(t *testing.T) {
	store := &fakeStore{}
	s := New(Config{Enabled: true}, store, nil)
	var calls int
	s.SetBeadFiler(func(beadID, title, description string) error {
		calls++
		return ErrFileBeadDuplicate
	})
	s.handleFileBead("bead-dup", "Some finding\nbody")
	if calls != 1 {
		t.Fatalf("filer calls = %d, want 1", calls)
	}
	// Benign skip: signature retained, so a repeat is skipped in-session too.
	s.handleFileBead("bead-dup", "Some finding\nbody")
	if calls != 1 {
		t.Fatalf("repeat should have hit signature dedupe; filer calls = %d", calls)
	}
}

// TestFileBeadPremiseStaleFilerResultSkips: the filer's stale-premise refusal
// is a benign skip — signature retained, one filer call, no repair error.
func TestFileBeadPremiseStaleFilerResultSkips(t *testing.T) {
	store := &fakeStore{}
	s := New(Config{Enabled: true}, store, nil)
	var calls int
	s.SetBeadFiler(func(beadID, title, description string) error {
		calls++
		return ErrFileBeadPremiseStale
	})
	s.handleFileBead("bead-stale", "Wedge found\nbody")
	if calls != 1 {
		t.Fatalf("filer calls = %d, want 1", calls)
	}
	s.handleFileBead("bead-stale", "Wedge found\nbody")
	if calls != 1 {
		t.Fatalf("premise-stale skip must retain signature; filer calls = %d", calls)
	}
}
