package validation

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// StaticChecker performs fast static analysis validation
type StaticChecker struct{}

// NewStaticChecker creates a new static checker
func NewStaticChecker() *StaticChecker {
	return &StaticChecker{}
}

// StaticResult represents the outcome of static validation
type StaticResult struct {
	Passed   bool
	Errors   []ValidationError
	Warnings []string
	Duration int64
}

// Validate performs all static checks on a bead
func (s *StaticChecker) Validate(ctx context.Context, bead types.Bead, projectPath string) *ValidationResult {
	result := &ValidationResult{
		Passed:   true,
		Phase:    "static",
		Errors:   make([]ValidationError, 0),
		Warnings: make([]string, 0),
	}

	// Extract target files from metadata
	targetFiles := extractTargetFiles(bead)
	if len(targetFiles) == 0 {
		result.Errors = append(result.Errors, ValidationError{
			Severity: SeverityCritical,
			Message:  "No target files specified in bead metadata",
			Category: "metadata",
		})
		result.Passed = false
		return result
	}

	// Check 1: File existence
	for _, file := range targetFiles {
		fullPath := filepath.Join(projectPath, file)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			result.Errors = append(result.Errors, ValidationError{
				Severity: SeverityCritical,
				Message:  fmt.Sprintf("Target file does not exist: %s", file),
				File:     file,
				Category: "file_existence",
			})
			result.Passed = false
		}
	}

	// Check 2: No panic("not implemented") stubs
	for _, file := range targetFiles {
		if strings.HasSuffix(file, ".go") {
			fullPath := filepath.Join(projectPath, file)
			stubs := s.findPanicStubs(fullPath)
			for _, stub := range stubs {
				result.Errors = append(result.Errors, ValidationError{
					Severity: SeverityCritical,
					Message:  fmt.Sprintf("Found panic(\"not implemented\") stub: %s", stub.Function),
					File:     file,
					Line:     stub.Line,
					Category: "implementation",
				})
				result.Passed = false
			}
		}
	}

	// Check 3: Test files exist
	testFiles := s.findTestFiles(targetFiles)
	for _, testFile := range testFiles {
		fullPath := filepath.Join(projectPath, testFile)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			result.Errors = append(result.Errors, ValidationError{
				Severity: SeverityCritical,
				Message:  fmt.Sprintf("Missing test file: %s", testFile),
				File:     testFile,
				Category: "testing",
			})
			result.Passed = false
		}
	}

	// Check 4: Compilation
	if err := s.checkCompilation(projectPath, targetFiles); err != nil {
		result.Errors = append(result.Errors, ValidationError{
			Severity: SeverityCritical,
			Message:  fmt.Sprintf("Compilation failed: %v", err),
			Category: "compilation",
		})
		result.Passed = false
	}

	// Check 5: Tests pass (if test files exist)
	if len(testFiles) > 0 {
		if err := s.checkTestsPass(projectPath, testFiles); err != nil {
			result.Errors = append(result.Errors, ValidationError{
				Severity: SeverityCritical,
				Message:  fmt.Sprintf("Tests failed: %v", err),
				Category: "testing",
			})
			result.Passed = false
		}
	}

	return result
}

// PanicStub represents a found panic stub
type PanicStub struct {
	Function string
	Line     int
}

// findPanicStubs searches for panic("not implemented") in Go files
func (s *StaticChecker) findPanicStubs(filePath string) []PanicStub {
	stubs := make([]PanicStub, 0)

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.AllErrors)
	if err != nil {
		return stubs
	}

	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Check if it's a panic call
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "panic" {
			return true
		}

		// Check if argument is "not implemented"
		if len(call.Args) == 1 {
			if lit, ok := call.Args[0].(*ast.BasicLit); ok {
				if strings.Contains(lit.Value, "not implemented") {
					pos := fset.Position(call.Pos())

					// Get function name
					funcName := s.getEnclosingFunction(node, call.Pos())

					stubs = append(stubs, PanicStub{
						Function: funcName,
						Line:     pos.Line,
					})
				}
			}
		}

		return true
	})

	return stubs
}

// getEnclosingFunction finds the function that contains the given position
func (s *StaticChecker) getEnclosingFunction(node *ast.File, pos token.Pos) string {
	var funcName string

	ast.Inspect(node, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok {
			if fn.Pos() <= pos && pos <= fn.End() {
				funcName = fn.Name.Name
				return false
			}
		}
		return true
	})

	return funcName
}

// findTestFiles generates expected test file names from target files
func (s *StaticChecker) findTestFiles(targetFiles []string) []string {
	testFiles := make([]string, 0)

	for _, file := range targetFiles {
		if strings.HasSuffix(file, ".go") && !strings.HasSuffix(file, "_test.go") {
			// Generate test file name
			dir := filepath.Dir(file)
			base := filepath.Base(file)
			ext := filepath.Ext(base)
			name := strings.TrimSuffix(base, ext)
			testFile := filepath.Join(dir, name+"_test.go")
			testFiles = append(testFiles, testFile)
		}
	}

	return testFiles
}

// checkCompilation attempts to compile the project
func (s *StaticChecker) checkCompilation(projectPath string, targetFiles []string) error {
	// Try to build the specific files
	cmd := exec.Command("go", "build", "-o", "/dev/null")
	cmd.Dir = projectPath
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build failed: %v\n%s", err, string(output))
	}

	return nil
}

// checkTestsPass runs tests for the given test files
func (s *StaticChecker) checkTestsPass(projectPath string, testFiles []string) error {
	// Run go test
	cmd := exec.Command("go", "test", "-v", "-count=1")
	cmd.Dir = projectPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tests failed: %v\n%s", err, string(output))
	}

	return nil
}

// extractTargetFiles extracts target files from bead metadata
func extractTargetFiles(bead types.Bead) []string {
	if bead.Metadata == nil {
		return nil
	}

	filesStr, ok := bead.Metadata["target_files"]
	if !ok || filesStr == "" {
		return nil
	}

	// Parse comma-separated list
	files := strings.Split(filesStr, ",")
	for i := range files {
		files[i] = strings.TrimSpace(files[i])
	}

	return files
}
