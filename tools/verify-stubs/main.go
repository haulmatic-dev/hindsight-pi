// verify-stubs ensures beads marked "stub only" still have stub behavior.
//
// Usage:
//
//	go run tools/verify-stubs/main.go --source . --max-lines 5
//
// Exit codes:
//
//	0 - all stubs verified
//	1 - stub overrun detected
//	2 - config error
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Violation struct {
	Function       string `json:"function"`
	File           string `json:"file"`
	Line           int    `json:"line"`
	LinesOfCode    int    `json:"lines_of_code"`
	Threshold      int    `json:"threshold"`
	Issue          string `json:"issue"` // "too_many_lines" | "has_sql" | "has_http" | "has_loops" | "no_stub_test"
	Recommendation string `json:"recommendation"`
}

type Result struct {
	Passed     bool        `json:"passed"`
	Violations []Violation `json:"violations"`
	Verified   []string    `json:"verified,omitempty"`
}

func main() {
	var (
		source   = flag.String("source", ".", "Source root directory")
		maxLines = flag.Int("max-lines", 5, "Max lines for a stub function")
		jsonOut  = flag.Bool("json", true, "Output JSON")
		verbose  = flag.Bool("verbose", false, "Print scan progress")
	)
	flag.Parse()

	result := Result{
		Passed:     true,
		Violations: []Violation{},
		Verified:   []string{},
	}

	// Scan all Go files for functions that look like stubs
	// Heuristic: functions with "Stub", "stub", "TODO", "FIXME" in comments
	// Or functions explicitly named in test assertions as returning nil
	err := filepath.WalkDir(*source, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		violations, verified, err := checkFile(path, *maxLines)
		if err != nil {
			return nil
		}

		result.Violations = append(result.Violations, violations...)
		result.Verified = append(result.Verified, verified...)
		if len(violations) > 0 {
			result.Passed = false
		}

		if *verbose && (len(violations) > 0 || len(verified) > 0) {
			fmt.Fprintf(os.Stderr, "Scanned %s: %d violations, %d verified\n", path, len(violations), len(verified))
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Walk error: %v\n", err)
		os.Exit(2)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(result)
	} else {
		if result.Passed {
			fmt.Printf("✓ All %d stubs verified\n", len(result.Verified))
		} else {
			fmt.Printf("✗ %d stub violations found\n", len(result.Violations))
			for _, v := range result.Violations {
				fmt.Printf("  [%s] %s at %s:%d (%d lines)\n",
					v.Issue, v.Function, v.File, v.Line, v.LinesOfCode)
				fmt.Printf("    %s\n", v.Recommendation)
			}
		}
	}

	if !result.Passed {
		os.Exit(1)
	}
}

func checkFile(filePath string, maxLines int) ([]Violation, []string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}

	var violations []Violation
	var verified []string

	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		funcName := fn.Name.Name
		pos := fset.Position(fn.Pos())
		end := fset.Position(fn.End())
		linesOfCode := end.Line - pos.Line + 1

		// Skip if comment says "stub" or "placeholder"
		isStubComment := hasStubComment(fn.Doc)

		// Heuristic: small functions (<= maxLines) with simple returns are stubs
		isSimpleStub := linesOfCode <= maxLines && isSimpleReturnBody(fn.Body)

		// If comment says stub but body is large, flag it
		if isStubComment && !isSimpleStub {
			issue := "too_many_lines"
			recommendation := fmt.Sprintf("Reduce to stub (%d lines > %d threshold) or remove 'stub' comment", linesOfCode, maxLines)

			// Check for specific patterns that indicate full implementation
			if hasSQL(fn.Body) {
				issue = "has_sql"
				recommendation = "Function contains SQL queries — not a stub. Remove 'stub' comment or revert to stub."
			} else if hasHTTP(fn.Body) {
				issue = "has_http"
				recommendation = "Function contains HTTP calls — not a stub. Remove 'stub' comment or revert to stub."
			} else if hasLoops(fn.Body) {
				issue = "has_loops"
				recommendation = "Function contains loops — not a stub. Remove 'stub' comment or revert to stub."
			}

			violations = append(violations, Violation{
				Function:       funcName,
				File:           filePath,
				Line:           pos.Line,
				LinesOfCode:    linesOfCode,
				Threshold:      maxLines,
				Issue:          issue,
				Recommendation: recommendation,
			})
			continue
		}

		// Only track verified stubs for functions explicitly marked as stubs
		if isStubComment && isSimpleStub {
			verified = append(verified, fmt.Sprintf("%s:%s", filePath, funcName))
		}
	}

	return violations, verified, nil
}

func hasStubComment(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	stubRe := regexp.MustCompile(`(?i)\bstub\b|todo.*implement|fixme.*implement|not implemented|not yet implemented`)
	for _, c := range doc.List {
		if stubRe.MatchString(c.Text) {
			return true
		}
	}
	return false
}

func isSimpleReturnBody(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) == 0 {
		return true // empty body is a stub
	}
	// Count non-comment statements
	stmtCount := 0
	for _, stmt := range body.List {
		switch stmt.(type) {
		case *ast.ReturnStmt:
			stmtCount++
		case *ast.ExprStmt:
			// e.g., panic("not implemented")
			stmtCount++
		case *ast.AssignStmt:
			stmtCount++
		default:
			return false
		}
	}
	return stmtCount <= 2
}

func hasSQL(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch x := n.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				lower := strings.ToLower(x.Value)
				if strings.Contains(lower, "select") || strings.Contains(lower, "insert") ||
					strings.Contains(lower, "update") || strings.Contains(lower, "delete") ||
					strings.Contains(lower, "create table") {
					found = true
					return false
				}
			}
		}
		return true
	})
	return found
}

func hasHTTP(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		// Look for http.Get, http.Post, client.Do, etc.
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			name := fun.Sel.Name
			if name == "Get" || name == "Post" || name == "Do" || name == "NewRequest" {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func hasLoops(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			found = true
			return false
		}
		return true
	})
	return found
}
