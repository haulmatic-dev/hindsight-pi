package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFingerprintStable(t *testing.T) {
	entry := Entry{Kind: "signature", Key: "empty-diff", Severity: "critical"}

	fp1 := Fingerprint(entry)
	fp2 := Fingerprint(entry)

	if fp1 != fp2 {
		t.Errorf("Fingerprint not stable for identical entry: %q != %q", fp1, fp2)
	}
	if len(fp1) != 16 {
		t.Errorf("Fingerprint length = %d, want 16", len(fp1))
	}
	if !isHex(fp1) {
		t.Errorf("Fingerprint is not a 16-character hex string: %q", fp1)
	}
}

func TestFingerprintIgnoresEvidence(t *testing.T) {
	base := Entry{Kind: "signature", Key: "empty-diff", Severity: "critical"}
	withEvidence1 := base
	withEvidence1.Evidence = "first line"
	withEvidence2 := base
	withEvidence2.Evidence = "second line"

	fp1 := Fingerprint(withEvidence1)
	fp2 := Fingerprint(withEvidence2)
	if len(fp1) == 0 || len(fp2) == 0 {
		t.Fatalf("Fingerprint returned empty string; expected 16-character hex value")
	}
	if fp1 != fp2 {
		t.Errorf("Fingerprint changed when only evidence differed: %q != %q", fp1, fp2)
	}
}

func TestWriteBacklogTwoEntries(t *testing.T) {
	dir := t.TempDir()
	entries := []Entry{
		{Kind: "signature", Key: "empty-diff", Severity: "critical", Evidence: "empty-diff evidence"},
		{Kind: "signature", Key: "stuck-worker", Severity: "warning", Evidence: "stuck-worker evidence"},
	}

	written, err := WriteBacklog(dir, entries)
	if err != nil {
		t.Fatalf("WriteBacklog returned error: %v", err)
	}
	if written != 2 {
		t.Errorf("written = %d, want 2", written)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatalf("Glob failed: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("found %d .md files, want 2", len(files))
	}
}

func TestWriteBacklogDedupe(t *testing.T) {
	dir := t.TempDir()
	entries := []Entry{
		{Kind: "signature", Key: "empty-diff", Severity: "critical", Evidence: "empty-diff evidence"},
		{Kind: "signature", Key: "stuck-worker", Severity: "warning", Evidence: "stuck-worker evidence"},
	}

	if _, err := WriteBacklog(dir, entries); err != nil {
		t.Fatalf("first WriteBacklog returned error: %v", err)
	}

	written, err := WriteBacklog(dir, entries)
	if err != nil {
		t.Fatalf("second WriteBacklog returned error: %v", err)
	}
	if written != 0 {
		t.Errorf("second written = %d, want 0", written)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatalf("Glob failed: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("found %d .md files after second call, want 2", len(files))
	}
}

func TestWriteBacklogFileContent(t *testing.T) {
	dir := t.TempDir()
	evidence := "2026/07/22 14:03:01 [VerifiedClose] Bead X rejected (empty-diff)"
	entries := []Entry{
		{Kind: "signature", Key: "empty-diff", Severity: "critical", Evidence: evidence},
	}

	if _, err := WriteBacklog(dir, entries); err != nil {
		t.Fatalf("WriteBacklog returned error: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatalf("Glob failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 .md file, got %d", len(files))
	}

	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	got := string(content)

	wantTitle := "# Observer: empty-diff (critical)"
	if !strings.Contains(got, wantTitle) {
		t.Errorf("file content missing title %q:\n%s", wantTitle, got)
	}
	if !strings.Contains(got, evidence) {
		t.Errorf("file content missing evidence %q:\n%s", evidence, got)
	}
}

func TestWriteBacklogCreatesNestedDir(t *testing.T) {
	base := t.TempDir()
	nested := filepath.Join(base, "a", "b", "c")
	entries := []Entry{
		{Kind: "signature", Key: "panic", Severity: "critical", Evidence: "panic: boom"},
	}

	written, err := WriteBacklog(nested, entries)
	if err != nil {
		t.Fatalf("WriteBacklog returned error: %v", err)
	}
	if written != 1 {
		t.Errorf("written = %d, want 1", written)
	}

	if _, err := os.Stat(nested); err != nil {
		t.Errorf("nested dir was not created: %v", err)
	}
}

func isHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}
