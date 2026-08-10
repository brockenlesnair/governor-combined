package deadcode

import (
	"context"
	"strings"

	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
)

// ReachabilityAnalyzer performs reachability analysis on a call graph.
type ReachabilityAnalyzer struct {
	graph *callgraph.Graph
}

// NewReachabilityAnalyzer creates a new ReachabilityAnalyzer.
func NewReachabilityAnalyzer(cg *callgraph.Graph) *ReachabilityAnalyzer {
	return &ReachabilityAnalyzer{graph: cg}
}

// ReachableSet computes the set of reachable node IDs from the given entry points
// using BFS. Respects context cancellation.
func (ra *ReachabilityAnalyzer) ReachableSet(ctx context.Context, entryPoints []string) map[string]bool {
	reachable := make(map[string]bool)
	if len(entryPoints) == 0 {
		return reachable
	}

	queue := make([]string, 0, len(entryPoints))
	for _, ep := range entryPoints {
		if !reachable[ep] {
			reachable[ep] = true
			queue = append(queue, ep)
		}
	}

	for len(queue) > 0 {
		select {
		case <-ctx.Done():
			return reachable
		default:
		}

		current := queue[0]
		queue = queue[1:]

		for _, succ := range ra.graph.Successors(current) {
			if !reachable[succ] {
				reachable[succ] = true
				queue = append(queue, succ)
			}
		}
	}

	return reachable
}

// FindEntryPoints identifies entry points in the call graph:
// 1. main.main (name == "main" or name == "main.main")
// 2. init functions (name == "init" or starts with "init." or "init#")
// 3. Exported functions in packages containing "/cmd/" or == "main"
// 4. HTTP handler signatures (Signature contains "http.ResponseWriter" AND "*http.Request")
func (ra *ReachabilityAnalyzer) FindEntryPoints() []string {
	var entryPoints []string
	seen := make(map[string]bool)

	for _, node := range ra.graph.Nodes {
		if seen[node.ID] {
			continue
		}

		name := node.Name
	funcName := shortName(name)

	// 1. main.main
	if name == "main" || name == "main.main" || funcName == "main" {
		entryPoints = append(entryPoints, node.ID)
		seen[node.ID] = true
		continue
	}

	// 2. init functions — check full name for .init patterns
	if name == "init" || strings.Contains(name, ".init") || strings.HasPrefix(name, "init.") || strings.HasPrefix(name, "init#") {
		entryPoints = append(entryPoints, node.ID)
		seen[node.ID] = true
		continue
	}

	// 3. Exported functions in cmd/main packages
	if node.Exported && (strings.Contains(node.Package, "/cmd/") || strings.HasPrefix(node.Package, "cmd") || node.Package == "main") {
		entryPoints = append(entryPoints, node.ID)
		seen[node.ID] = true
		continue
	}

		// 4. HTTP handler signatures
		if strings.Contains(node.Signature, "http.ResponseWriter") && strings.Contains(node.Signature, "*http.Request") {
			entryPoints = append(entryPoints, node.ID)
			seen[node.ID] = true
			continue
		}
	}

	return entryPoints
}

// shortName extracts the function name after the last dot from a qualified name.
// e.g. "pkgA.init.foo" -> "init.foo", "pkgA.init" -> "init"
func shortName(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}
