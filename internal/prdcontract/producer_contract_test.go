// Package prdcontract pins the producer-side contract documented in
// .agents/skills/create-prd/SKILL.md §3.1 (Core Compiler Contract).
//
// The create-prd skill is "source code for a deterministic compiler": the
// task-breakdown analyzer (agents + task-breakdown/analyzer.mjs) classifies
// each requirement into a taskPattern. These tests pin the documented
// contract so documentation drift fails a test instead of silently
// degrading decomposition.
package prdcontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var skillRelPath = filepath.Join(".agents", "skills", "create-prd", "SKILL.md")

// skillDoc locates the repo root (the directory containing .agents/) starting
// from this test file's location and returns the SKILL.md contents.
func skillDoc(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, skillRelPath)
		if _, err := os.Stat(candidate); err == nil {
			data, err := os.ReadFile(candidate)
			if err != nil {
				t.Fatalf("read %s: %v", candidate, err)
			}
			return string(data)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate %s from %s", skillRelPath, "..")
	return ""
}

// systemTypeEnumValues are the five values of the System Type enum. The
// enum tells the task compiler which execution pattern and surface rules
// apply to the whole PRD.
var systemTypeEnumValues = []string{
	"backend-api",
	"web-app",
	"cli-tool",
	"library",
	"full-stack",
}

// TestSystemTypeEnumDocumented pins parent AC 1: SKILL.md contains the
// System Type enum with all five values.
func TestSystemTypeEnumDocumented(t *testing.T) {
	doc := skillDoc(t)
	enumLine := "**System Type:**"
	idx := strings.Index(doc, enumLine)
	if idx < 0 {
		t.Fatalf("SKILL.md does not contain %q (System Type enum missing)", enumLine)
	}
	// Scan a window after the enum label; the enum is a single
	// pipe-delimited line.
	windowEnd := idx + 200
	if windowEnd > len(doc) {
		windowEnd = len(doc)
	}
	window := doc[idx:windowEnd]
	for _, v := range systemTypeEnumValues {
		if !strings.Contains(window, v) {
			t.Errorf("System Type enum does not document value %q near %q", v, enumLine)
		}
	}
}

// TestSystemTypePrefixesDocumented pins parent AC 2: SKILL.md documents all
// five new prefixes. Each System Type value is also a valid requirement
// **Type:** prefix; when a requirement carries one, the compiler honors it
// directly instead of inferring.
func TestSystemTypePrefixesDocumented(t *testing.T) {
	doc := skillDoc(t)
	for _, v := range systemTypeEnumValues {
		if !strings.Contains(doc, v) {
			t.Errorf("SKILL.md does not document prefix %q", v)
		}
	}
}

// TestClassificationPrecedenceDocumented pins parent AC 3: SKILL.md states
// the classification precedence order — explicit Type prefix beats
// target-file shape, which beats the PRD-level System Type, which beats the
// tdd_chain default.
func TestClassificationPrecedenceDocumented(t *testing.T) {
	doc := skillDoc(t)
	for _, term := range []string{"precedence", "target-file shape", "tdd_chain"} {
		if !strings.Contains(strings.ToLower(doc), strings.ToLower(term)) {
			t.Errorf("SKILL.md does not state classification precedence (missing term %q)", term)
		}
	}
	// The precedence must be stated in order: prefix > target-file shape >
	// System Type > tdd_chain.
	iPrefix := strings.Index(doc, "prefix")
	iShape := strings.Index(doc, "target-file shape")
	iSys := strings.LastIndex(doc, "System Type")
	iChain := strings.Index(doc, "tdd_chain")
	if iPrefix < 0 || iShape < 0 || iSys < 0 || iChain < 0 {
		t.Fatalf("one or more precedence terms missing: prefix=%d shape=%d sys=%d chain=%d", iPrefix, iShape, iSys, iChain)
	}
	if !(iPrefix < iShape && iShape < iChain && iSys > iShape) {
		t.Errorf("precedence order not stated as prefix > target-file shape > System Type > tdd_chain (positions: prefix=%d, shape=%d, chain=%d, systemType=%d)", iPrefix, iShape, iChain, iSys)
	}
}
