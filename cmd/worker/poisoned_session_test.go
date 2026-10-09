package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQuarantinePoisonedSessions(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "hydra-test1")
	if err := os.MkdirAll(sessionDir, 0750); err != nil {
		t.Fatal(err)
	}

	// Small session file — must stay.
	small := filepath.Join(sessionDir, "small.jsonl")
	if err := os.WriteFile(small, []byte(`{"ok":true}`), 0640); err != nil {
		t.Fatal(err)
	}
	// Oversize session file — must be quarantined.
	big := filepath.Join(sessionDir, "big.jsonl")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(poisonedSessionThresholdBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	// Non-jsonl oversize file — must stay (not a session transcript).
	other := filepath.Join(sessionDir, "notes.bin")
	g, _ := os.Create(other)
	g.Truncate(poisonedSessionThresholdBytes + 1)
	g.Close()

	quarantinePoisonedSessions(sessionDir)

	if _, err := os.Stat(small); err != nil {
		t.Errorf("small session file must remain: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("non-jsonl file must remain: %v", err)
	}
	if _, err := os.Stat(big); !os.IsNotExist(err) {
		t.Errorf("oversize jsonl must be moved out, stat err=%v", err)
	}
	q := filepath.Join(root, ".poisoned-quarantine", "hydra-test1_big.jsonl")
	if _, err := os.Stat(q); err != nil {
		t.Errorf("quarantined file missing at %s: %v", q, err)
	}
}

func TestQuarantinePoisonedSessionsMissingDir(t *testing.T) {
	// Fail-open: nonexistent dir must not panic or error.
	quarantinePoisonedSessions(filepath.Join(t.TempDir(), "does-not-exist"))
}
