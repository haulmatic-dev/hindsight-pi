package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShouldResume_Disabled(t *testing.T) {
	p := NewSessionResumePilot(SessionResumeConfig{Enabled: false})
	ok, reason := p.ShouldResume("b", 1, true, "/tmp/foo", 0, 0, "abc", "abc")
	if ok {
		t.Fatal("want disabled")
	}
	if reason != "session resume disabled" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestShouldResume_FreshAttempt(t *testing.T) {
	p := NewSessionResumePilot(SessionResumeConfig{Enabled: true})
	ok, reason := p.ShouldResume("b", 0, true, t.TempDir(), 0, 0, "a", "a")
	if ok || reason != "not a retry attempt" {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}

func TestShouldResume_NotInfra(t *testing.T) {
	p := NewSessionResumePilot(SessionResumeConfig{Enabled: true, InfraOnly: true})
	ok, reason := p.ShouldResume("b", 1, false, t.TempDir(), 0, 0, "a", "a")
	if ok || reason != "last failure was not infrastructure" {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}

func TestShouldResume_SessionMissing(t *testing.T) {
	p := NewSessionResumePilot(SessionResumeConfig{Enabled: true})
	ok, reason := p.ShouldResume("b", 1, true, "/no/such/session", 0, 0, "a", "a")
	if ok || reason != "session directory missing" {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}

func TestShouldResume_SessionTooOld(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(f, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Ensure mtime is in the past.
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(f, past, past); err != nil {
		t.Fatal(err)
	}
	p := NewSessionResumePilot(SessionResumeConfig{Enabled: true, MaxSessionAge: time.Hour})
	ok, reason := p.ShouldResume("b", 1, true, dir, 2*time.Hour, 10, "a", "a")
	if ok || reason != "session too old" {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}

func TestShouldResume_SessionTooLarge(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := NewSessionResumePilot(SessionResumeConfig{Enabled: true, MaxSessionSize: 2})
	ok, reason := p.ShouldResume("b", 1, true, dir, 0, 100, "a", "a")
	if ok || reason != "session too large" {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}

func TestShouldResume_HeadMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := NewSessionResumePilot(SessionResumeConfig{Enabled: true, HeadCheck: true})
	ok, reason := p.ShouldResume("b", 1, true, dir, 0, 1, "head2", "head1")
	if ok || reason != "worktree HEAD changed since prior attempt" {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}

func TestShouldResume_OK(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := NewSessionResumePilot(SessionResumeConfig{Enabled: true, HeadCheck: true})
	ok, reason := p.ShouldResume("b", 1, true, dir, 0, 1, "head1", "head1")
	if !ok {
		t.Fatalf("want ok, reason=%q", reason)
	}
}

func TestSessionResumePilot_AutoDisable(t *testing.T) {
	p := NewSessionResumePilot(SessionResumeConfig{
		Enabled:          true,
		DryRun:           false,
		MinSamples:       3,
		FixRateThreshold: 0.5,
	})
	if !p.Enabled() {
		t.Fatal("expected enabled")
	}
	p.RecordFix(true)
	p.RecordFix(true)
	if !p.Enabled() {
		t.Fatal("should not disable before min samples")
	}
	p.RecordFix(true)
	if p.Enabled() {
		t.Fatal("expected auto-disable after 3/3 resumed fixes")
	}
}

func TestSessionResumePilot_DryRunDoesNotDisable(t *testing.T) {
	p := NewSessionResumePilot(SessionResumeConfig{
		Enabled:          true,
		DryRun:           true,
		MinSamples:       1,
		FixRateThreshold: 0.0,
	})
	p.RecordFix(true)
	if !p.Enabled() {
		t.Fatal("dry-run must not auto-disable")
	}
}

func TestSessionFileStats(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(f, []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}
	age, size, exists, err := SessionFileStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("expected exists")
	}
	if size != 11 {
		t.Fatalf("size=%d", size)
	}
	if age < 0 || age > time.Minute {
		t.Fatalf("age=%v", age)
	}
}

func TestPiSessionDir(t *testing.T) {
	dir := PiSessionDir("hydra/b-123", "/tmp/home")
	want := "/tmp/home/sessions/hydra-b-123"
	if dir != want {
		t.Fatalf("dir=%q want %q", dir, want)
	}
}
