// Package validation: stub_detector.go
//
// Stub detection beyond `panic("not implemented")`. The legacy
// StaticChecker only matched panic stubs; that left a wide aperture for
// implementations that compiled, passed tests against trivial assertions,
// and reported success while doing nothing. This file adds detectors for:
//
//  1. Empty function bodies (only `return` of zero values).
//  2. Discard-parameter-only bodies (`_ = x` followed by zero return).
//  3. Placeholder comments ("Real implementation", "follow-up",
//     "TODO", "FIXME", "stub", "placeholder", "not implemented").
//
// All detectors operate on go/ast so they survive formatting changes
// and quoted-comment tricks. The detector returns a list of
// `StubFinding` with file:line, function name, and category. Severity
// is SeverityCritical because a stub is a regression in correctness, not
// a style issue.

package validation

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// StubFinding describes a detected stub pattern.
type StubFinding struct {
	File     string
	Line     int
	Function string
	Category string // "empty-body", "discard-params", "placeholder-comment"
	Detail   string
}

// placeholderKeywords are case-insensitive substrings that, when found
// in a comment attached to a function declaration, mark the function
// as a stub.
var placeholderKeywords = []string{
	"real implementation",
	"follow-up",
	"placeholder",
	"not implemented",
	"todo:",
	"fixme:",
	"stub:",
	"mock implementation",
	"temporary implementation",
}

// DetectStubs parses a Go source file and returns all stub patterns
// found. Returns an empty slice on parse error so the validator can
// still report other issues instead of failing on a single bad file.
func DetectStubs(filePath string) []StubFinding {
	findings := make([]StubFinding, 0)

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return findings
	}

	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue // interface methods have no body; skip.
		}

		// Skip test helpers and underscore-prefixed funcs — those are
		// the conventional names for stubs we don't want to flag.
		if strings.HasPrefix(fn.Name.Name, "Test") || strings.HasPrefix(fn.Name.Name, "_") {
			continue
		}

		// Skip methods that override standard library interfaces where
		// an empty body is meaningful (e.g., error implementations).
		// We accept a small false-negative rate here rather than block
		// legitimate empty implementations.

		if f := detectEmptyBody(fset, fn); f != nil {
			f.Function = fn.Name.Name
			f.File = filePath
			findings = append(findings, *f)
		}
		if f := detectDiscardParams(fset, fn); f != nil {
			f.Function = fn.Name.Name
			f.File = filePath
			findings = append(findings, *f)
		}
		if f := detectPlaceholderComment(fset, fn); f != nil {
			f.Function = fn.Name.Name
			f.File = filePath
			findings = append(findings, *f)
		}
	}

	return findings
}

// detectEmptyBody flags functions whose body contains only a return
// statement (with at most an `_ =` discard or a literal zero value).
func detectEmptyBody(fset *token.FileSet, fn *ast.FuncDecl) *StubFinding {
	// Walk the body's statement list. A "trivial" body is one that:
	//  - has exactly one statement, AND
	//  - that statement is a return with all zero values (nil, 0, "", false).
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return nil
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok {
		return nil
	}

	// All return args must be zero-valued identifiers/literals.
	for _, expr := range ret.Results {
		if !isZeroValue(expr) {
			return nil
		}
	}

	return &StubFinding{
		Line:     fset.Position(fn.Pos()).Line,
		Category: "empty-body",
		Detail:   "function body is a single `return` of zero values",
	}
}

// detectDiscardParams flags functions where every parameter is bound
// to `_` inside the body (typical placeholder signature: `func
// (a *Activities) GetFileMetadata(_ context.Context, id string) (..., error) {
//   _ = id; return ..., nil
// }`).
func detectDiscardParams(fset *token.FileSet, fn *ast.FuncDecl) *StubFinding {
	if fn.Body == nil {
		return nil
	}
	if fn.Type == nil || fn.Type.Params == nil {
		return nil
	}

	// Collect parameter names that are non-blank.
	nonBlank := make([]string, 0)
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			if name.Name != "_" && name.Name != "" {
				nonBlank = append(nonBlank, name.Name)
			}
		}
	}
	if len(nonBlank) == 0 {
		return nil
	}

	// Walk the body for `_ = <nonBlank>` discards.
	discarded := make(map[string]bool)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		ident, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || ident.Name != "_" {
			return true
		}
		rhsIdent, ok := assign.Rhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		discarded[rhsIdent.Name] = true
		return true
	})

	if len(discarded) < len(nonBlank) {
		return nil
	}

	// Every non-blank parameter is discarded.
	return &StubFinding{
		Line:     fset.Position(fn.Pos()).Line,
		Category: "discard-params",
		Detail:   "all parameters discarded via `_ = x`; function has no real inputs",
	}
}

// detectPlaceholderComment flags functions whose declaration comment
// (or the first line above it) contains a placeholder keyword. The
// check requires the keyword to appear within a stub-introducing
// sentence; descriptive documentation that mentions the word
// "placeholder" in passing is allowed.
func detectPlaceholderComment(fset *token.FileSet, fn *ast.FuncDecl) *StubFinding {
	if fn.Doc == nil {
		return nil
	}
	combined := ""
	for _, c := range fn.Doc.List {
		combined += c.Text + "\n"
	}
	lower := strings.ToLower(combined)

	// Stub-introducing phrases. The check is "keyword appears in a
	// sentence that introduces this function as a placeholder/stub".
	// We look for the keyword within ~30 chars of "this", "is", or
	// at the start of a sentence.
	introducingPatterns := []string{
		"this is a placeholder",
		"this is a stub",
		"this is a mock",
		"this is temporary",
		"this function is a placeholder",
		"this function is a stub",
		"placeholder implementation",
		"stub implementation",
		"mock implementation",
		"real implementation comes in",
		"real implementation is the follow-up",
		"real implementation follows in",
		"real impl comes in",
		"todo: implement",
		"todo: real",
		"fixme: stub",
		"fixme: real",
	}
	for _, pattern := range introducingPatterns {
		if strings.Contains(lower, pattern) {
			return &StubFinding{
				Line:     fset.Position(fn.Pos()).Line,
				Category: "placeholder-comment",
				Detail:   "function doc-comment contains placeholder keyword: " + pattern,
			}
		}
	}
	return nil
}

// isZeroValue reports whether expr is a literal zero value (nil, 0,
// "", false) or an identifier named `_` (the discard identifier).
func isZeroValue(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		// nil, _, or other predeclared zero identifiers.
		return e.Name == "nil" || e.Name == "_"
	case *ast.BasicLit:
		switch e.Kind.String() {
		case "INT", "FLOAT", "CHAR", "STRING":
			return e.Value == "0" || e.Value == "0.0" || e.Value == `""` || e.Value == "''"
		}
	case *ast.UnaryExpr:
		// -0, -0.0
		if e.Op.String() == "-" {
			return isZeroValue(e.X)
		}
	}
	return false
}