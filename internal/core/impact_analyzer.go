package core

import (
	"fmt"

	"github.com/haulmatic-dev/hydra/internal/intelligence"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

// ImpactAnalyzer performs impact analysis using TLDR service.
type ImpactAnalyzer struct {
	findProjectPath func(bead *types.Bead) string
}

// NewImpactAnalyzer creates a new impact analyzer.
func NewImpactAnalyzer(findProjectPath func(bead *types.Bead) string) *ImpactAnalyzer {
	return &ImpactAnalyzer{findProjectPath: findProjectPath}
}

// Analyze performs impact analysis for a bead's files.
func (a *ImpactAnalyzer) Analyze(client *intelligence.TLDRClient, bead *types.Bead, files []string) (*ImpactAnalysis, error) {
	projectPath := a.findProjectPath(bead)
	if projectPath == "" {
		return nil, fmt.Errorf("could not find project path for bead %s", bead.ID)
	}

	client.SetSearchPath(projectPath)

	if len(files) == 0 {
		return nil, fmt.Errorf("no files to analyze")
	}

	resp, err := client.ImpactAnalysis(files[0], 2)
	if err != nil {
		return nil, fmt.Errorf("TLDR impact analysis failed: %w", err)
	}

	analysis := &ImpactAnalysis{
		TargetFiles: files,
	}

	if len(resp.Impact.Callers) > 0 || len(resp.Impact.Callees) > 0 {
		callGraph := &CallGraph{}

		for _, caller := range resp.Impact.Callers {
			nodeID := fmt.Sprintf("%s:%s", caller.File, caller.Function)
			callGraph.Nodes = append(callGraph.Nodes, CallNode{
				ID:       nodeID,
				Name:     caller.Function,
				File:     caller.File,
				NodeType: "caller",
			})
		}

		for _, callee := range resp.Impact.Callees {
			nodeID := fmt.Sprintf("%s:%s", callee.File, callee.Function)
			callGraph.Nodes = append(callGraph.Nodes, CallNode{
				ID:       nodeID,
				Name:     callee.Function,
				File:     callee.File,
				NodeType: "callee",
			})
		}

		analysis.CallGraph = callGraph
	}

	return analysis, nil
}
