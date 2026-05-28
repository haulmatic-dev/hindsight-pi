package core

import "github.com/haulmatic-dev/hydra/pkg/types"

// Type aliases for backward compatibility with code that references
// these types from the core package before they were moved to pkg/types.

type IntelligenceBundle = types.IntelligenceBundle
type Pattern = types.Pattern
type ContextEntry = types.ContextEntry
type ImpactAnalysis = types.ImpactAnalysis
type CallGraph = types.CallGraph
type CallNode = types.CallNode
type CallEdge = types.CallEdge
