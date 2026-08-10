package untested

import (
	"strings"

	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
)

// PriorityScorer assigns a risk priority score to untested functions.
type PriorityScorer struct {
	cg *callgraph.Graph
}

// NewPriorityScorer creates a scorer backed by the given call graph.
func NewPriorityScorer(cg *callgraph.Graph) *PriorityScorer {
	return &PriorityScorer{cg: cg}
}

// Score returns a value in [0, 1] representing how risky it is to leave
// this function untested.
func (s *PriorityScorer) Score(node *callgraph.Node) float64 {
	callerWeight := s.callerWeight(node)
	exportWeight := s.exportWeight(node)
	methodWeight := s.methodWeight(node)
	depthWeight := s.depthWeight(node)
	packageWeight := s.packageWeight(node)

	return callerWeight*0.35 +
		exportWeight*0.25 +
		methodWeight*0.15 +
		depthWeight*0.15 +
		packageWeight*0.10
}

// callerWeight normalises the caller count: callers/20, capped at 1.0.
func (s *PriorityScorer) callerWeight(node *callgraph.Node) float64 {
	callers := s.cg.Callers(node.ID)
	w := float64(len(callers)) / 20.0
	if w > 1.0 {
		w = 1.0
	}
	return w
}

// exportWeight returns 1.0 for exported symbols, 0.3 otherwise.
func (s *PriorityScorer) exportWeight(node *callgraph.Node) float64 {
	if node.Exported {
		return 1.0
	}
	return 0.3
}

// methodWeight returns the weight for a method receiver.
// 0.5 for ordinary methods, 1.0 for critical receivers.
func (s *PriorityScorer) methodWeight(node *callgraph.Node) float64 {
	if node.Kind != callgraph.NodeKindMethod {
		return 0.2
	}
	if isCriticalReceiver(node.Receiver) {
		return 1.0
	}
	return 0.5
}

// depthWeight measures proximity to entry points via TransitiveCallers.
// The closer to an entry point, the higher the weight.
func (s *PriorityScorer) depthWeight(node *callgraph.Node) float64 {
	if isEntryPoint(node) {
		return 1.0
	}
	callers := s.cg.TransitiveCallers(node.ID, 3)
	if len(callers) == 0 {
		return 0.1
	}
	// Heuristic: deeper caller chain = more important
	depth := len(callers)
	if depth > 6 {
		depth = 6
	}
	return float64(depth) / 6.0
}

// packageWeight scores based on which package the node lives in.
func (s *PriorityScorer) packageWeight(node *callgraph.Node) float64 {
	pkg := strings.ToLower(node.Package)
	if strings.Contains(pkg, "cmd") ||
		strings.Contains(pkg, "main") ||
		strings.Contains(pkg, "handler") ||
		strings.Contains(pkg, "server") ||
		strings.Contains(pkg, "api") ||
		strings.Contains(pkg, "gateway") {
		return 1.0
	}
	if strings.Contains(pkg, "internal") || strings.Contains(pkg, "pkg") {
		return 0.6
	}
	return 0.3
}

// isCriticalReceiver checks if the receiver type is from a well-known
// interface family (error, io.Reader, io.Writer, http.Handler, etc.).
func isCriticalReceiver(receiver string) bool {
	r := strings.ToLower(receiver)
	critical := []string{
		"error",
		"reader", "writer", "readcloser", "writecloser", "readwriter",
		"handler",
		"roundtripper",
		"transport",
		"database", "sql", "tx",
		"conn", "listener",
	}
	for _, kw := range critical {
		if strings.Contains(r, kw) {
			return true
		}
	}
	return false
}

// isEntryPoint returns true for main.main or functions with HTTP handler
// signatures (contains http.ResponseWriter and *http.Request).
func isEntryPoint(node *callgraph.Node) bool {
	if node.Name == "main.main" {
		return true
	}
	sig := strings.ToLower(node.Signature)
	if strings.Contains(sig, "http.responsewriter") && strings.Contains(sig, "*http.request") {
		return true
	}
	return false
}
