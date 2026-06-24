// verify-exports compares PRD Exports against actual source code symbols.
//
// Usage:
//
//	go run tools/verify-exports/main.go --prd docs/PRDs/feature.md --lang go --source .
//
// Exit codes:
//
//	0 - all signatures match
//	1 - one or more mismatches
//	2 - PRD parse error
//	3 - source file not found
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

// --- PRD parsing ----------------------------------------------------------

type Export struct {
	Name           string   `json:"name"`
	Signature      string   `json:"signature"`
	TargetFile     string   `json:"target_file"`
	AllTargetFiles []string `json:"-"`
	IsMethod       bool     `json:"-"`
	RecvType       string   `json:"-"`
}

type Violation struct {
	Export          string `json:"export"`
	PRDSignature    string `json:"prd_signature"`
	ActualSignature string `json:"actual_signature"`
	File            string `json:"file"`
	Line            int    `json:"line"`
	Severity        string `json:"severity"`
	Message         string `json:"message"`
}

type Result struct {
	Passed     bool        `json:"passed"`
	Violations []Violation `json:"violations"`
	Verified   []Export    `json:"verified"`
}

// parsePRD extracts Exports blocks from markdown PRD.
// Looks for:
//
//	**Exports:**
//	- functionName(param Type) ReturnType
//
// and correlates with nearest preceding **Target Files:** entry.
func parsePRD(content string) ([]Export, error) {
	var exports []Export
	lines := strings.Split(content, "\n")

	var currentTargetFiles []string
	var inExports bool
	var inTargetFiles bool
	var reTargetFiles = regexp.MustCompile(`(?i)^\s*\*\*Target Files:\*\*\s*$`)

	for _, line := range lines {
		// Detect Target Files heading — files are on subsequent bullet lines
		if reTargetFiles.MatchString(line) {
			currentTargetFiles = nil
			inTargetFiles = true
			inExports = false
			continue
		}

		// Collect Target Files bullet lines
		if inTargetFiles {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
				currentTargetFiles = append(currentTargetFiles, parseTargetFiles(trimmed)...)
				continue
			} else if trimmed == "" {
				continue
			} else {
				inTargetFiles = false
			}
		}

		// Detect Exports heading
		if strings.Contains(line, "**Exports:**") || strings.Contains(line, "**Exports**") {
			inExports = true
			continue
		}

		// End of exports block (empty line or new heading)
		if inExports && (strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#")) {
			inExports = false
			continue
		}

		if !inExports {
			continue
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Remove leading bullet
		line = strings.TrimPrefix(line, "-")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)

		// Remove surrounding backticks
		if strings.HasPrefix(line, "`") && strings.HasSuffix(line, "`") {
			line = line[1 : len(line)-1]
		}

		// Skip non-function lines (e.g., "types.Audience and constants")
		if !strings.Contains(line, "(") {
			continue
		}

		// Parse: Name(params) returns
		// Also: (*Receiver) MethodName(params) returns
		// Strategy:
		// 1. Extract receiver if present: (*Type) MethodName(...)
		// 2. Find FIRST '(' after name — that's the params paren
		// 3. Returns may also have parens: (Type1, Type2)

		work := line
		recvType := ""
		isMethod := false

		// Extract receiver
		if strings.HasPrefix(work, "(") {
			recvClose := strings.Index(work, ")")
			if recvClose != -1 {
				recvType = strings.TrimSpace(work[1:recvClose])
				work = strings.TrimSpace(work[recvClose+1:])
				isMethod = true
			}
		}

		// Find FIRST '(' — params paren
		open := strings.Index(work, "(")
		if open == -1 {
			continue
		}

		// Find matching ')'
		close := -1
		depth := 0
		for i := open; i < len(work); i++ {
			if work[i] == '(' {
				depth++
			} else if work[i] == ')' {
				depth--
				if depth == 0 {
					close = i
					break
				}
			}
		}
		if close == -1 {
			continue
		}

		namePart := strings.TrimSpace(work[:open])
		params := work[open+1 : close]
		returns := strings.TrimSpace(work[close+1:])

		// Strip package prefix from name: package.Function → Function
		name := namePart
		if idx := strings.LastIndex(namePart, "."); idx != -1 {
			before := namePart[:idx]
			after := namePart[idx+1:]
			// If before starts with uppercase and has no dot, it's Type.Method
			if len(before) > 0 && before[0] >= 'A' && before[0] <= 'Z' && !strings.Contains(before, ".") {
				recvType = before
				isMethod = true
			}
			name = after
		}

		e := Export{
			Name:      name,
			Signature: fmt.Sprintf("%s(%s) %s", name, params, returns),
			IsMethod:  isMethod,
			RecvType:  recvType,
		}
		if len(currentTargetFiles) > 0 {
			e.TargetFile = currentTargetFiles[0]
			e.AllTargetFiles = currentTargetFiles
		}
		exports = append(exports, e)
	}

	return exports, nil
}

func parseTargetFiles(line string) []string {
	var files []string
	// Match backtick-wrapped paths or bare paths
	re := regexp.MustCompile("`([^`]+)`")
	matches := re.FindAllStringSubmatch(line, -1)
	for _, m := range matches {
		files = append(files, m[1])
	}
	return files
}

// --- Go AST symbol extraction ---------------------------------------------

type GoSymbol struct {
	Name       string
	Signature  string
	File       string
	Line       int
	IsFunction bool
	IsMethod   bool
	RecvType   string // for methods
}

func extractGoSymbols(filePath string) ([]GoSymbol, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filePath, err)
	}

	var symbols []GoSymbol
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			pos := fset.Position(d.Pos())
			sig := funcDeclSignature(d)
			sym := GoSymbol{
				Name:       name,
				Signature:  sig,
				File:       filePath,
				Line:       pos.Line,
				IsFunction: d.Recv == nil,
				IsMethod:   d.Recv != nil,
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				sym.RecvType = exprToString(d.Recv.List[0].Type)
			}
			symbols = append(symbols, sym)
		}
	}
	return symbols, nil
}

func funcDeclSignature(d *ast.FuncDecl) string {
	var parts []string
	parts = append(parts, d.Name.Name)
	parts = append(parts, "(")
	if d.Type.Params != nil {
		for i, f := range d.Type.Params.List {
			if i > 0 {
				parts = append(parts, ", ")
			}
			for j, n := range f.Names {
				if j > 0 {
					parts = append(parts, ", ")
				}
				parts = append(parts, n.Name, " ")
			}
			parts = append(parts, exprToString(f.Type))
		}
	}
	parts = append(parts, ")")
	if d.Type.Results != nil && len(d.Type.Results.List) > 0 {
		parts = append(parts, " ")
		if len(d.Type.Results.List) > 1 {
			parts = append(parts, "(")
		}
		for i, f := range d.Type.Results.List {
			if i > 0 {
				parts = append(parts, ", ")
			}
			// Named return values
			for _, n := range f.Names {
				parts = append(parts, n.Name, " ")
			}
			parts = append(parts, exprToString(f.Type))
		}
		if len(d.Type.Results.List) > 1 {
			parts = append(parts, ")")
		}
	}
	return strings.Join(parts, "")
}

func exprToString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return "*" + exprToString(v.X)
	case *ast.ArrayType:
		if v.Len == nil {
			return "[]" + exprToString(v.Elt)
		}
		return fmt.Sprintf("[%s]%s", exprToString(v.Len), exprToString(v.Elt))
	case *ast.SelectorExpr:
		return exprToString(v.X) + "." + v.Sel.Name
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", exprToString(v.Key), exprToString(v.Value))
	case *ast.ChanType:
		return "chan " + exprToString(v.Value)
	case *ast.FuncType:
		return "func(...)"
	case *ast.BasicLit:
		return v.Value
	case *ast.Ellipsis:
		return "..." + exprToString(v.Elt)
	default:
		return fmt.Sprintf("%T", e)
	}
}

// --- Signature comparison -------------------------------------------------

// normalizeSignature strips spaces and lowercases for fuzzy comparison.
func normalizeSignature(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\t", "")
	return strings.ToLower(s)
}

// signaturesMatch compares PRD signature against actual Go signature.
// Returns true if they match, and a human-readable diff if not.
func signaturesMatch(prdSig, actualSig string) (bool, string) {
	prdNorm := normalizeSignature(prdSig)
	actNorm := normalizeSignature(actualSig)

	if prdNorm == actNorm {
		return true, ""
	}

	// Try without parameter names (PRD may omit names or use different ones)
	prdNoNames := stripParamNames(prdNorm)
	actNoNames := stripParamNames(actNorm)
	if prdNoNames == actNoNames {
		return true, ""
	}

	return false, fmt.Sprintf("prd=%q actual=%q", prdSig, actualSig)
}

// stripParamNames removes parameter names from a normalized signature,
// keeping only types. E.g. "newserver(db*sql.db,queue*jobqueue)" becomes
// "newserver(*sql.db,*jobqueue)".
func stripParamNames(sig string) string {
	// This is a best-effort heuristic for normalized (no-space) signatures.
	// Find the opening paren, then split params by comma, then for each param
	// find the last word before the type.
	open := strings.Index(sig, "(")
	if open == -1 {
		return sig
	}
	close := strings.LastIndex(sig, ")")
	if close == -1 {
		close = len(sig)
	}

	name := sig[:open]
	paramsStr := sig[open+1 : close]
	returns := ""
	if close+1 < len(sig) {
		returns = sig[close+1:]
	}

	var params []string
	for _, p := range strings.Split(paramsStr, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Heuristic: the type is the last "word" in the param.
		// For Go, types can be *T, []T, map[K]V, chan T, func(...)
		// We look for the first occurrence of a type indicator.
		ty := extractTypeFromParam(p)
		if ty != "" {
			params = append(params, ty)
		} else {
			params = append(params, p)
		}
	}

	return name + "(" + strings.Join(params, ",") + ")" + returns
}

// extractTypeFromParam finds the type portion of a parameter string.
// Handles: *Type, []Type, map[K]V, chan T, func(...), interface{}
func extractTypeFromParam(p string) string {
	// If starts with ..., strip it and look at the rest
	if strings.HasPrefix(p, "...") {
		rest := p[3:]
		ty := extractTypeFromParam(rest)
		if ty != "" {
			return "..." + ty
		}
	}

	// If starts with *, the type is everything after *
	if strings.HasPrefix(p, "*") {
		return "*" + extractTypeFromParam(p[1:])
	}

	// If starts with [], the type is inside
	if strings.HasPrefix(p, "[]") {
		return "[]" + extractTypeFromParam(p[2:])
	}

	// If starts with map[, find matching ]
	if strings.HasPrefix(p, "map[") {
		// Find the ] that separates key and value
		depth := 0
		for i := 4; i < len(p); i++ {
			if p[i] == '[' {
				depth++
			} else if p[i] == ']' {
				if depth == 0 {
					// This is the key/value separator
					key := p[4:i]
					val := p[i+1:]
					return "map[" + extractTypeFromParam(key) + "]" + extractTypeFromParam(val)
				}
				depth--
			}
		}
	}

	// If starts with chan, strip and recurse
	if strings.HasPrefix(p, "chan") {
		rest := strings.TrimPrefix(p, "chan")
		return "chan" + extractTypeFromParam(rest)
	}

	// If starts with func, treat as func(...)
	if strings.HasPrefix(p, "func") {
		return "func(...)"
	}

	// If starts with interface, treat as interface{}
	if strings.HasPrefix(p, "interface") {
		return "interface{}"
	}

	// Default: find the last word boundary.
	// Split on common type indicators. The type is usually the last capitalized
	// word or a dot-separated package.Type.
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '.' {
			// Found package.Type — type is everything from here to end
			return p[i+1:]
		}
	}

	// If all uppercase/lowercase mixed, the last "word" is the type
	words := splitWords(p)
	if len(words) > 1 {
		return words[len(words)-1]
	}
	return p
}

func splitWords(s string) []string {
	var words []string
	var start int
	for i := 1; i < len(s); i++ {
		if isWordBoundary(s[i-1], s[i]) {
			words = append(words, s[start:i])
			start = i
		}
	}
	words = append(words, s[start:])
	return words
}

func isWordBoundary(prev, curr byte) bool {
	// Transition from lowercase to uppercase, or from letter to non-letter
	return (prev >= 'a' && prev <= 'z' && curr >= 'A' && curr <= 'Z') ||
		((prev >= 'A' && prev <= 'Z' || prev >= 'a' && prev <= 'z') &&
			!(curr >= 'A' && curr <= 'Z' || curr >= 'a' && curr <= 'z' || curr >= '0' && curr <= '9'))
}

// normalizeRecvType strips package prefix and pointer from receiver type
// for fuzzy matching. E.g. "*connectors.HubSpotClient" → "HubSpotClient"
func normalizeRecvType(rt string) string {
	rt = strings.TrimSpace(rt)
	rt = strings.TrimPrefix(rt, "*")
	if idx := strings.LastIndex(rt, "."); idx != -1 {
		rt = rt[idx+1:]
	}
	return rt
}

// --- Main -----------------------------------------------------------------

func main() {
	var (
		prdPath    = flag.String("prd", "", "Path to PRD markdown file")
		lang       = flag.String("lang", "go", "Language: go, ts, py, rs")
		sourceRoot = flag.String("source", ".", "Source root directory")
		jsonOut    = flag.Bool("json", true, "Output JSON")
	)
	flag.Parse()

	if *prdPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: verify-exports --prd <prd.md> [--lang go] [--source .]")
		os.Exit(2)
	}

	_ = lang // TODO: route to language-specific parser

	prdContent, err := os.ReadFile(*prdPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading PRD: %v\n", err)
		os.Exit(2)
	}

	exports, err := parsePRD(string(prdContent))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing PRD: %v\n", err)
		os.Exit(2)
	}

	if len(exports) == 0 {
		fmt.Fprintln(os.Stderr, "Warning: no Exports found in PRD")
		// Not an error — PRD may use Reads From only
		result := Result{Passed: true}
		if *jsonOut {
			json.NewEncoder(os.Stdout).Encode(result)
		}
		os.Exit(0)
	}

	result := Result{
		Passed:     true,
		Violations: []Violation{},
		Verified:   []Export{},
	}

	// For Go: build a map of file -> symbols to avoid re-parsing
	fileSymbols := make(map[string][]GoSymbol)

	for _, exp := range exports {
		// Build list of candidate files to search
		var candidates []string
		if len(exp.AllTargetFiles) > 0 {
			for _, tf := range exp.AllTargetFiles {
				candidates = append(candidates, filepath.Join(*sourceRoot, tf))
			}
		} else if exp.TargetFile != "" {
			candidates = append(candidates, filepath.Join(*sourceRoot, exp.TargetFile))
		}

		if len(candidates) == 0 {
			result.Violations = append(result.Violations, Violation{
				Export:       exp.Name,
				PRDSignature: exp.Signature,
				Severity:     "warning",
				Message:      "No Target File found for this export — cannot verify",
			})
			result.Passed = false
			continue
		}

		var matched *GoSymbol
		var matchedFile string
		var foundWrongReceiver *GoSymbol
		var foundWrongReceiverFile string

		for _, filePath := range candidates {
			// Skip non-existent files
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				continue
			}

			// Skip non-Go files (SQL, YAML, etc.)
			if !strings.HasSuffix(filePath, ".go") {
				continue
			}

			// Parse file if not already cached
			if _, ok := fileSymbols[filePath]; !ok {
				syms, err := extractGoSymbols(filePath)
				if err != nil {
					continue // skip unparseable files, try next candidate
				}
				fileSymbols[filePath] = syms
			}

			// Find matching symbol — function or method
			for i := range fileSymbols[filePath] {
				s := &fileSymbols[filePath][i]
				if s.Name == exp.Name {
					// If PRD says it's a method, verify receiver matches
					if exp.IsMethod {
						// Normalize receiver type for comparison
						prdRecv := normalizeRecvType(exp.RecvType)
						actualRecv := normalizeRecvType(s.RecvType)
						if prdRecv != "" && actualRecv != "" && prdRecv != actualRecv {
							foundWrongReceiver = s
							foundWrongReceiverFile = filePath
							continue // receiver mismatch, keep searching
						}
					} else if s.IsMethod {
						continue // PRD says function, but this is a method
					}
					matched = s
					matchedFile = filePath
					break
				}
			}
			if matched != nil {
				break
			}
		}

		if matched == nil {
			msg := fmt.Sprintf("Function/method %s not found in any target file", exp.Name)
			if foundWrongReceiver != nil {
				msg = fmt.Sprintf("Method %s found on *%s, but PRD expects *%s",
					exp.Name, normalizeRecvType(foundWrongReceiver.RecvType), normalizeRecvType(exp.RecvType))
			}
			result.Violations = append(result.Violations, Violation{
				Export:       exp.Name,
				PRDSignature: exp.Signature,
				File:         foundWrongReceiverFile,
				Severity:     "error",
				Message:      msg,
			})
			result.Passed = false
			continue
		}

		ok, diff := signaturesMatch(exp.Signature, matched.Signature)
		if !ok {
			result.Violations = append(result.Violations, Violation{
				Export:          exp.Name,
				PRDSignature:    exp.Signature,
				ActualSignature: matched.Signature,
				File:            matchedFile,
				Line:            matched.Line,
				Severity:        "error",
				Message:         diff,
			})
			result.Passed = false
			continue
		}

		result.Verified = append(result.Verified, exp)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(result)
	} else {
		if result.Passed {
			fmt.Printf("✓ All %d exports verified\n", len(result.Verified))
		} else {
			fmt.Printf("✗ %d violations found\n", len(result.Violations))
			for _, v := range result.Violations {
				fmt.Printf("  [%s] %s: %s\n", v.Severity, v.Export, v.Message)
				if v.File != "" {
					fmt.Printf("    at %s:%d\n", v.File, v.Line)
				}
			}
		}
	}

	if !result.Passed {
		os.Exit(1)
	}
}
