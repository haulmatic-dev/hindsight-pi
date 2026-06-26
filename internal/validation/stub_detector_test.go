package validation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDetectStubs_EmptyBody(t *testing.T) {
	src := `package foo
type S struct{}
func (s *S) DoNothing() error { return nil }
func (s *S) ReturnZero() (int, error) { return 0, nil }
`
	path := writeTempFile(t, "x.go", src)
	stubs := DetectStubs(path)
	if len(stubs) == 0 {
		t.Fatalf("expected at least one stub finding, got none")
	}
	for _, s := range stubs {
		if s.Category != "empty-body" {
			continue
		}
		return
	}
	t.Errorf("expected an empty-body finding; got %+v", stubs)
}

func TestDetectStubs_DiscardParams(t *testing.T) {
	// All non-blank parameters are immediately discarded.
	src := `package foo
type Activities struct{}
func (a *Activities) GetThing(_ context.Context, id string) (string, error) {
	_ = id
	return "", nil
}
`
	// We need the `context` import to be syntactically valid; the
	// detector doesn't type-check, but the parser will accept it.
	src = strings.Replace(src, "context.Context", "interface{}", 1)
	path := writeTempFile(t, "x.go", src)
	stubs := DetectStubs(path)
	foundDiscard := false
	for _, s := range stubs {
		if s.Category == "discard-params" {
			foundDiscard = true
			break
		}
	}
	if !foundDiscard {
		t.Errorf("expected discard-params finding; got %+v", stubs)
	}
}

func TestDetectStubs_PlaceholderComment(t *testing.T) {
	src := `package foo
type Activities struct{}

// GetThing returns a stub. Real implementation is the follow-up
// bead; today this is a placeholder.
func (a *Activities) GetThing() error {
	return nil
}
`
	path := writeTempFile(t, "x.go", src)
	stubs := DetectStubs(path)
	foundPlaceholder := false
	for _, s := range stubs {
		if s.Category == "placeholder-comment" {
			foundPlaceholder = true
			break
		}
	}
	if !foundPlaceholder {
		t.Errorf("expected placeholder-comment finding; got %+v", stubs)
	}
}

func TestDetectStubs_RealImplementation(t *testing.T) {
	// A function that actually does work should NOT be flagged.
	src := `package foo
import "fmt"
type S struct{ value int }
func (s *S) DoSomething(x int) int {
	total := s.value + x
	fmt.Println(total)
	return total
}
`
	path := writeTempFile(t, "x.go", src)
	stubs := DetectStubs(path)
	if len(stubs) != 0 {
		t.Errorf("expected zero stubs for real implementation, got %+v", stubs)
	}
}

func TestDetectStubs_TestHelpersIgnored(t *testing.T) {
	// Test functions and underscore-prefixed functions should be ignored.
	src := `package foo
func TestSomething(t *testing.T) {}
func _internalHelper() int { return 0 }
`
	path := writeTempFile(t, "x.go", src)
	stubs := DetectStubs(path)
	for _, s := range stubs {
		if strings.HasPrefix(s.Function, "Test") || strings.HasPrefix(s.Function, "_") {
			t.Errorf("test/internal helper flagged as stub: %s", s.Function)
		}
	}
}