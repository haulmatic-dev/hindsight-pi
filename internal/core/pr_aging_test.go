package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPRWaitingLedger_RecordUpsertAndRemove(t *testing.T) {
	home := t.TempDir()
	l := NewPRWaitingLedger(home)
	if l == nil {
		t.Fatal("expected non-nil ledger")
	}
	l.Record(PRWaitingRecord{BeadID: "hydra-rrtq", ProjectPath: "/tmp/repo", Owner: "hydra", URL: "https://github.com/x/y/pull/1", Since: time.Now()})
	// Upsert: same bead, new URL — no duplicate.
	l.Record(PRWaitingRecord{BeadID: "hydra-rrtq", ProjectPath: "/tmp/repo", Owner: "hydra", URL: "https://github.com/x/y/pull/2", Since: time.Now()})
	l.Record(PRWaitingRecord{BeadID: "hydra-aaaa", ProjectPath: "/tmp/repo", Owner: "hydra", URL: "https://github.com/x/y/pull/3", Since: time.Now()})
	got := l.List()
	if len(got) != 2 {
		t.Fatalf("want 2 records after upsert, got %d", len(got))
	}
	if got[0].URL != "https://github.com/x/y/pull/2" {
		t.Fatalf("upsert did not refresh URL: %s", got[0].URL)
	}
	// Persistence across ledger instances.
	l2 := NewPRWaitingLedger(home)
	if len(l2.List()) != 2 {
		t.Fatal("ledger did not persist")
	}
	l2.Remove("hydra-rrtq")
	if got = l2.List(); len(got) != 1 || got[0].BeadID != "hydra-aaaa" {
		t.Fatalf("remove failed: %+v", got)
	}
}

func TestPRWaitingLedger_InertWithoutHome(t *testing.T) {
	if NewPRWaitingLedger("") != nil {
		t.Fatal("empty home must yield nil (inert) ledger")
	}
	var l *PRWaitingLedger
	l.Record(PRWaitingRecord{BeadID: "x"}) // must not panic
	l.Remove("x")                          // must not panic
	if got := l.List(); len(got) != 0 {
		t.Fatalf("nil ledger List must be empty, got %d", len(got))
	}
}

func TestPRWaitingLedger_CorruptFileStartsEmpty(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "pr-waiting.json"), []byte("{not json"), 0o644)
	l := NewPRWaitingLedger(home)
	if got := l.List(); got != nil {
		t.Fatalf("corrupt ledger should fail open to empty, got %+v", got)
	}
	l.Record(PRWaitingRecord{BeadID: "b1", URL: "u"})
	if got := l.List(); len(got) != 1 {
		t.Fatalf("record after corrupt read failed: %+v", got)
	}
}

// prAgingTestSink returns an EventSink writing into a temp central dir plus a
// reader for the emitted pr_aging_digest events.
func prAgingTestSink(t *testing.T) (*EventSink, func() []Event) {
	t.Helper()
	dir := t.TempDir()
	sink := NewEventSink(dir)
	return sink, func() []Event {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		var evs []Event
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				if line == "" {
					continue
				}
				var ev Event
				if json.Unmarshal([]byte(line), &ev) == nil && ev.Type == "pr_aging_digest" {
					evs = append(evs, ev)
				}
			}
		}
		return evs
	}
}

func TestPRAgingScanner_DigestClassification(t *testing.T) {
	home := t.TempDir()
	ledger := NewPRWaitingLedger(home)
	// One aged PR in a human-merge-queue repo, one aged in an auto-merge repo,
	// one young PR, one merged PR.
	old := time.Now().Add(-10 * 24 * time.Hour)
	young := time.Now().Add(-1 * time.Hour)
	ledger.Record(PRWaitingRecord{BeadID: "b-human", ProjectPath: "/tmp/product", Owner: "alice", URL: "https://gh/p/1", Since: old})
	ledger.Record(PRWaitingRecord{BeadID: "b-stuck", ProjectPath: "/tmp/automerge", Owner: "hydra", URL: "https://gh/p/2", Since: old})
	ledger.Record(PRWaitingRecord{BeadID: "b-young", ProjectPath: "/tmp/product", Owner: "alice", URL: "https://gh/p/3", Since: young})
	ledger.Record(PRWaitingRecord{BeadID: "b-merged", ProjectPath: "/tmp/product", Owner: "alice", URL: "https://gh/p/4", Since: old})

	// resolvePublishIdentityFor: no resolver installed → daemon identity, which
	// may or may not resolve; Owner comes from the record on identity failure,
	// so pin ownership via the record path (identity error keeps rec.Owner).
	oldResolver := publishIdentityResolver
	SetPublishIdentityResolver(func(string) (PublishIdentity, error) {
		return PublishIdentity{}, ErrPublishIdentityMissing
	})
	t.Cleanup(func() { SetPublishIdentityResolver(oldResolver) })

	prStateProbeFn = func(url, _ string) (string, error) {
		if url == "https://gh/p/4" {
			return "MERGED", nil
		}
		return "OPEN", nil
	}
	t.Cleanup(func() { prStateProbeFn = nil })

	sink, read := prAgingTestSink(t)
	s := NewPRAgingScanner(ledger, nil, func(p string) bool { return p == "/tmp/automerge" }, 7, sink)
	s.scan()

	if got := ledger.List(); len(got) != 3 {
		t.Fatalf("merged PR not removed from ledger: %+v", got)
	}
	events := read()
	if len(events) != 2 {
		t.Fatalf("want one digest per owner (alice, hydra), got %d: %+v", len(events), events)
	}
	byOwner := map[string]map[string]any{}
	for _, ev := range events {
		owner, _ := ev.Fields["owner"].(string)
		byOwner[owner] = ev.Fields
	}
	f, ok := byOwner["alice"]
	if !ok || f["human_merge_queue"].(float64) != 1 || f["stuck_auto_merge"].(float64) != 0 {
		t.Fatalf("alice digest wrong: %+v", f)
	}
	f, ok = byOwner["hydra"]
	if !ok || f["stuck_auto_merge"].(float64) != 1 {
		t.Fatalf("hydra (auto-merge repo) digest wrong: %+v", f)
	}
}

func TestPRAgingScanner_ProbeFailureFailOpen(t *testing.T) {
	home := t.TempDir()
	ledger := NewPRWaitingLedger(home)
	ledger.Record(PRWaitingRecord{BeadID: "b1", ProjectPath: "/tmp/r", Owner: "hydra", URL: "https://gh/p/9", Since: time.Now().Add(-30 * 24 * time.Hour)})
	prStateProbeFn = func(url, token string) (string, error) { return "", os.ErrPermission }
	t.Cleanup(func() { prStateProbeFn = nil })

	sink, read := prAgingTestSink(t)
	s := NewPRAgingScanner(ledger, nil, nil, 7, sink)
	s.scan() // must not panic, no digest, record retained
	if events := read(); len(events) != 0 {
		t.Fatalf("probe failure must skip digest, got %d", len(events))
	}
	if got := ledger.List(); len(got) != 1 {
		t.Fatal("failed probe must retain record")
	}
}

func TestPRAgingScanner_DisabledAndNilSafe(t *testing.T) {
	s := NewPRAgingScanner(nil, nil, nil, 0, nil)
	s.scan() // disabled: no-op
	s.Start()
	s.Stop()
}

// BeadCloser records a waiting PR via the recorder (nil-safe).
func TestBeadCloser_SetPRWaitingRecorder(t *testing.T) {
	var c BeadCloser
	c.recordPRWaitingCapture(nil, "", "u") // nil-safe no-op
	var got []PRWaitingRecord
	c.SetPRWaitingRecorder(func(r PRWaitingRecord) { got = append(got, r) })
	c.recordPRWaitingCapture(nil, "/tmp/r", "u") // nil bead skipped
	if len(got) != 0 {
		t.Fatalf("nil bead must not record, got %+v", got)
	}
}
