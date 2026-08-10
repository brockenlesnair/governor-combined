package deadcode

import (
	"strings"

	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
)

// ConfidenceScorer computes confidence scores for dead code candidates.
type ConfidenceScorer struct {
	graph *callgraph.Graph
}

// NewConfidenceScorer creates a new ConfidenceScorer.
func NewConfidenceScorer(cg *callgraph.Graph) *ConfidenceScorer {
	return &ConfidenceScorer{graph: cg}
}

// Score computes a confidence score for a dead code candidate.
// Returns 0.0 if the node is reachable.
//
// Score formula:
//   - If reachable: 0.0
//   - Base score: 0.7
//   - Unexported + 0 callers: 0.95
//   - Exported + 0 callers + main package: 0.9
//   - Exported in library package (not main/cmd): *= 0.6
//   - Method with interface-dispatch edges (Edge.CallType == "interface" && Edge.To == ref.ID): *= 0.5
//   - More callers: *= (1.0 - callers×0.1)
//   - Clamp to [0, 1.0]
func (cs *ConfidenceScorer) Score(ref *callgraph.Node, reachable bool, callerCount int) float64 {
	if reachable {
		return 0.0
	}

	score := 0.7

	if !ref.Exported && callerCount == 0 {
		score = 0.95
	} else if ref.Exported && callerCount == 0 && (ref.Package == "main" || strings.Contains(ref.Package, "/cmd/")) {
		score = 0.9
	} else if ref.Exported && callerCount > 0 {
		isLibrary := ref.Package != "main" && !strings.Contains(ref.Package, "/cmd/")
		if isLibrary {
			score *= 0.6
		}
	}

	// Check for interface-dispatch edges targeting this node
	if ref.Kind == callgraph.NodeKindMethod && cs.hasInterfaceDispatch(ref.ID) {
		score *= 0.5
	}

	// Reduce confidence based on caller count
	if callerCount > 0 {
		score *= (1.0 - float64(callerCount)*0.1)
	}

	// Clamp to [0, 1.0]
	if score < 0.0 {
		score = 0.0
	}
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// hasInterfaceDispatch checks if any edge in the graph targets the given node ID
// with a CallType of "interface".
func (cs *ConfidenceScorer) hasInterfaceDispatch(nodeID string) bool {
	for _, edge := range cs.graph.Edges {
		if edge.CallType == "interface" && edge.To == nodeID {
			return true
		}
	}
	return false
}
