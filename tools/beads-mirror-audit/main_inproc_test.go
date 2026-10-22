package main

// In-process GREEN tests for runAudit — cover the jsonl parsing, db-parsing,
// divergence matching, unavailable paths and the --json report contract that
// the subprocess tests exercise end-to-end. Runs against the brRunner seam
// so coverage lands on main.go's implementation code.

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func inprocStore(t *testing.T, jsonl string) string {
	t.Helper()
	dir := t.TempDir()
	writeJSONL(t, dir, jsonl)
	return dir
}

func TestInproc_CleanMirror(t *testing.T) {
	dir := inprocStore(t, "proj-001 open\nproj-002 closed\n")
	brRunner = func(dir string, args []string) (string, error) {
		return `[{"id":"proj-001","status":"open"},{"id":"proj-002","status":"closed"}]`, nil
	}
	rep, out, code := runAudit(dir, false)
	if code != exitClean || !strings.Contains(out, "mirror clean (2 beads)") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if rep.Checked != 2 || len(rep.Divergences) != 0 {
		t.Fatalf("report: %+v", rep)
	}
}

func TestInproc_Divergence(t *testing.T) {
	dir := inprocStore(t, "proj-001 open\nproj-002 closed\nproj-003 closed\n")
	brRunner = func(dir string, args []string) (string, error) {
		return `[{"id":"proj-001","status":"open"},{"id":"proj-002","status":"closed"},{"id":"proj-003","status":"open"}]`, nil
	}
	_, out, code := runAudit(dir, false)
	if code != exitDrift {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "proj-003 jsonl=closed db=open") {
		t.Fatalf("out=%q", out)
	}
}

func TestInproc_MissingInDB(t *testing.T) {
	dir := inprocStore(t, "proj-001 open\nproj-ghost closed\n")
	brRunner = func(dir string, args []string) (string, error) {
		return `[{"id":"proj-001","status":"open"}]`, nil
	}
	rep, out, code := runAudit(dir, false)
	if code != exitDrift {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "proj-ghost jsonl=closed db=") || rep.Checked != 2 {
		t.Fatalf("out=%q rep=%+v", out, rep)
	}
}

func TestInproc_JSONRecordLines(t *testing.T) {
	// Real br stores emit JSON objects per line; both formats must parse.
	dir := inprocStore(t, `{"id":"proj-001","status":"open"}`+"\n"+`{"id":"proj-002","status":"closed"}`+"\n")
	brRunner = func(dir string, args []string) (string, error) {
		return `[{"id":"proj-001","status":"open"},{"id":"proj-002","status":"open"}]`, nil
	}
	rep, out, code := runAudit(dir, true)
	if code != exitDrift || rep.Checked != 2 || len(rep.Divergences) != 1 || rep.Divergences[0].BeadID != "proj-002" {
		t.Fatalf("code=%d rep=%+v out=%q", code, rep, out)
	}
}

func TestInproc_BrFailure(t *testing.T) {
	dir := inprocStore(t, "proj-001 open\n")
	brRunner = func(dir string, args []string) (string, error) {
		return "", errors.New("br list exit 3")
	}
	_, out, code := runAudit(dir, false)
	if code != exitUnavail || !strings.HasPrefix(out, auditUnavailablePrefix) {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestInproc_BadDBOutput(t *testing.T) {
	dir := inprocStore(t, "proj-001 open\n")
	brRunner = func(dir string, args []string) (string, error) {
		return "not json at all", nil
	}
	_, out, code := runAudit(dir, false)
	if code != exitUnavail || !strings.HasPrefix(out, auditUnavailablePrefix) {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestInproc_StoreMissing(t *testing.T) {
	_, _, _ = runAudit(t.TempDir(), false) // no issues.jsonl → unavailable
	_, out, code := runAudit(filepath.Join(t.TempDir(), "nope"), false)
	if code != exitUnavail || !strings.HasPrefix(out, auditUnavailablePrefix) {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestInproc_PrintOut(t *testing.T) {
	dir := inprocStore(t, "proj-001 open\n")
	brRunner = func(dir string, args []string) (string, error) {
		return `[{"id":"proj-001","status":"closed"}]`, nil
	}
	_, out, code := runAudit(dir, false)
	if code != exitDrift || !strings.Contains(out, "proj-001 jsonl=open db=closed") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestInproc_ExitCodeFor(t *testing.T) {
	if got := exitCodeFor(Report{Checked: 3}); got != exitClean {
		t.Fatalf("clean map: got %d", got)
	}
	if got := exitCodeFor(Report{Divergences: []Divergence{{BeadID: "h-1", JSONLStatus: "open", DBStatus: "closed"}}}); got != exitDrift {
		t.Fatalf("drift map: got %d", got)
	}
}
