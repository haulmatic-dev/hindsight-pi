package mirroraudit

// Fixture tests for the FR-1 audit package. Each case builds a temp store
// with an issues.jsonl fixture and a stub Comparer, then pins the exact
// classification contract (clean / db_ahead / jsonl_ahead) and the counts.

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// stubComparer returns BrListJSON output for a fixed dbJSON; err fails the
// shell-out path.
type stubComparer struct {
	dbJSON string
	err    error
}

func (s *stubComparer) BrListJSON(storeDir string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.dbJSON, nil
}

func writeFixture(t *testing.T, jsonl string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "issues.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAudit_CleanFixture(t *testing.T) {
	dir := writeFixture(t, "{\"id\":\"proj-001\",\"status\":\"open\"}\n{\"id\":\"proj-002\",\"status\":\"closed\"}\n")
	rep, err := Audit(dir, &stubComparer{dbJSON: `[{"id":"proj-001","status":"open"},{"id":"proj-002","status":"closed"}]`})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if rep.Drift() {
		t.Fatalf("expected clean, got %+v", rep)
	}
	if rep.Counts.Clean != 2 || rep.Counts.DBAhead != 0 || rep.Counts.JSONLAhead != 0 {
		t.Fatalf("counts mismatch: %+v", rep.Counts)
	}
	if rep.Checked != 2 {
		t.Fatalf("checked=%d want 2", rep.Checked)
	}
}

func TestAudit_StatusDisagreementIsJSONLAhead(t *testing.T) {
	dir := writeFixture(t, "proj-001 closed\nproj-002 open\n")
	rep, err := Audit(dir, &stubComparer{dbJSON: `[{"id":"proj-001","status":"open"},{"id":"proj-002","status":"open"}]`})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	// proj-001: jsonl=closed vs db=open → jsonl_ahead (stale mirror).
	// proj-002: jsonl=open vs db=open → clean (statuses agree).
	want := Counts{Clean: 1, DBAhead: 0, JSONLAhead: 1}
	if rep.Counts != want {
		t.Fatalf("counts=%+v want %+v", rep.Counts, want)
	}
	for _, r := range rep.Records {
		if r.BeadID == "proj-001" && r.Class != ClassJSONLAhead {
			t.Fatalf("record proj-001 class=%s want jsonl_ahead", r.Class)
		}
		if r.BeadID == "proj-002" && r.Class != ClassClean {
			t.Fatalf("record proj-002 class=%s want clean", r.Class)
		}
	}
	if !rep.Drift() {
		t.Fatalf("one stale record must flag drift")
	}
}

func TestAudit_DBOnlyRowIsDBAhead(t *testing.T) {
	dir := writeFixture(t, "{\"id\":\"proj-001\",\"status\":\"open\"}\n")
	rep, err := Audit(dir, &stubComparer{dbJSON: `[{"id":"proj-001","status":"open"},{"id":"proj-999","status":"closed"}]`})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !rep.Drift() || rep.Counts.DBAhead != 1 {
		t.Fatalf("expected 1 db_ahead, got %+v", rep.Counts)
	}
	for _, rec := range rep.Records {
		if rec.BeadID != "proj-999" {
			continue
		}
		if rec.JSONLStatus != "" || rec.Class != ClassDBAhead {
			t.Fatalf("db_ahead record mismatch: %+v", rec)
		}
		return
	}
	t.Fatalf("proj-999 db_ahead record missing from %+v", rep.Records)
}

func TestAudit_MirrorOnlyRowIsJSONLAhead(t *testing.T) {
	dir := writeFixture(t, "proj-001 open\nproj-ghost closed\n")
	rep, err := Audit(dir, &stubComparer{dbJSON: `[{"id":"proj-001","status":"open"}]`})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !rep.Drift() || rep.Counts.JSONLAhead != 1 {
		t.Fatalf("expected 1 jsonl_ahead, got %+v", rep.Counts)
	}
	if rec := rep.Records[len(rep.Records)-1]; rec.BeadID != "proj-ghost" || rec.DBStatus != "" {
		t.Fatalf("mirror-only record mismatch: %+v", rec)
	}
}

func TestAudit_MixedClassesCounts(t *testing.T) {
	dir := writeFixture(t,
		"proj-clean open\n"+
			"proj-drift closed\n"+ // jsonl_ahead: status disagreement
			"proj-only open\n") // jsonl_ahead: mirror-only
	rep, err := Audit(dir, &stubComparer{dbJSON: `[
		{"id":"proj-clean","status":"open"},
		{"id":"proj-drift","status":"open"},
		{"id":"proj-dbonly","status":"closed"}]`})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	want := Counts{Clean: 1, DBAhead: 1, JSONLAhead: 2}
	if !reflect.DeepEqual(rep.Counts, want) {
		t.Fatalf("counts=%+v want %+v", rep.Counts, want)
	}
	if rep.Checked != 3 {
		t.Fatalf("checked=%d want 3", rep.Checked)
	}
}

func TestAudit_MissingMirrorFileFailsClosed(t *testing.T) {
	rep, err := Audit(filepath.Join(t.TempDir(), "nope"), &stubComparer{})
	if err == nil {
		t.Fatalf("expected error for missing issues.jsonl, got %+v", rep)
	}
	if rep.Checked != 0 {
		t.Fatalf("checked must be 0 on error, got %d", rep.Checked)
	}
}

func TestAudit_BrShellOutFailure(t *testing.T) {
	dir := writeFixture(t, "proj-001 open\n")
	rep, err := Audit(dir, &stubComparer{err: errors.New("br list exit 3")})
	if err == nil {
		t.Fatalf("expected br failure error")
	}
	if rep.Checked != 1 || rep.Drift() {
		t.Fatalf("partial report must not report drift: %+v", rep)
	}
}

func TestAudit_BadDBJSONFailsClosed(t *testing.T) {
	dir := writeFixture(t, "proj-001 open\n")
	if _, err := Audit(dir, &stubComparer{dbJSON: "not json at all"}); err == nil {
		t.Fatalf("expected db parse error")
	}
}

func TestAudit_MalformedJSONLLineIsIgnoredNotFatal(t *testing.T) {
	// Documentation comment says malformed lines are never panic; the parser
	// skips lines without id+status tokens.
	dir := writeFixture(t, "proj-001 open\n\n   \nonlyonefield\n")
	rep, err := Audit(dir, &stubComparer{dbJSON: `[{"id":"proj-001","status":"open"}]`})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if rep.Checked != 1 || rep.Drift() {
		t.Fatalf("want clean with checked=1, got %+v", rep)
	}
}

func TestAudit_ParserAcceptsJSONAndWhitespaceForms(t *testing.T) {
	dir := writeFixture(t, "{\"id\":\"proj-001\",\"status\":\"open\"}\nproj-002 closed\n")
	rep, err := Audit(dir, &stubComparer{dbJSON: `[{"id":"proj-001","status":"open"},{"id":"proj-002","status":"closed"}]`})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if rep.Drift() {
		t.Fatalf("expected clean, got %+v", rep.Counts)
	}
	if rep.Checked != 2 {
		t.Fatalf("checked=%d want 2", rep.Checked)
	}
}
