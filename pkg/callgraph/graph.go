package callgraph

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// GraphBuilder builds a call graph from source code.
type GraphBuilder struct {
	graph       *Graph
	parser      SourceParser
	analyzer    *Analyzer
	visited     map[string]bool
	mu          sync.Mutex
	packages    map[string]*Package
	fileToPkg   map[string]string
}

// NewGraphBuilder creates a new graph builder. If parser is nil, a GoParser is used.
func NewGraphBuilder(parser ...SourceParser) *GraphBuilder {
	var p SourceParser
	if len(parser) > 0 && parser[0] != nil {
		p = parser[0]
	} else {
		p = NewGoParser()
	}
	return &GraphBuilder{
		graph:     NewGraph(),
		parser:    p,
		analyzer:  NewAnalyzer(),
		visited:   make(map[string]bool),
		packages:  make(map[string]*Package),
		fileToPkg: make(map[string]string),
	}
}

// Build builds the call graph for the given package path.
func (b *GraphBuilder) Build(ctx context.Context, rootPath string) (*Graph, error) {
	// Parse all packages
	pkgs, err := b.parser.ParsePackages(ctx, rootPath)
	if err != nil {
		return nil, fmt.Errorf("parse packages: %w", err)
	}

	// Build package map
	for _, pkg := range pkgs {
		b.packages[pkg.Path] = &Package{
			Name:    pkg.Name,
			Path:    pkg.Path,
			Files:   pkg.Files,
			Imports: pkg.Imports,
		}
		for _, file := range pkg.Files {
			b.fileToPkg[file] = pkg.Path
		}
	}

	// Extract nodes and edges from each package
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			nodes, edges, err := b.parser.ParseFile(ctx, file, pkg)
			if err != nil {
				// Log error but continue
				continue
			}

			for _, node := range nodes {
				b.graph.AddNode(node)
			}
			for _, edge := range edges {
				b.graph.AddEdge(edge.From, edge.To, edge.CallType)
			}
		}
	}

	// Resolve interface calls
	b.resolveInterfaceCalls()

	// Run analysis
	b.analyzer.Analyze(b.graph)

	return b.graph, nil
}

// resolveInterfaceCalls resolves interface method calls to concrete implementations.
func (b *GraphBuilder) resolveInterfaceCalls() {
	// For now, we'll use a simple heuristic: if a call target is an interface method,
	// find all types that have a method with the same name and signature
	for _, edge := range b.graph.Edges {
		if edge.CallType == "interface" {
			toNode := b.graph.GetNode(edge.To)
			if toNode != nil && toNode.Kind == NodeKindMethod {
				// Find implementations
				impls := b.findInterfaceImplementations(toNode.Name, toNode.Receiver)
				if len(impls) > 0 {
					// Add edges to implementations
					for _, implID := range impls {
						b.graph.AddEdge(edge.From, implID, "interface_impl")
					}
				}
			}
		}
	}
}

// findInterfaceImplementations finds types that implement a given method.
func (b *GraphBuilder) findInterfaceImplementations(methodName, receiver string) []string {
	var impls []string
	for _, node := range b.graph.Nodes {
		if node.Kind == NodeKindMethod && node.Name == methodName && node.Receiver != receiver {
			impls = append(impls, node.ID)
		}
	}
	return impls
}

// GetGraph returns the built graph.
func (b *GraphBuilder) GetGraph() *Graph {
	return b.graph
}

// TransitiveClosure computes the transitive closure of the graph.
func (g *Graph) TransitiveClosure() map[string]map[string]bool {
	closure := make(map[string]map[string]bool)

	// Initialize with direct edges
	for _, edge := range g.Edges {
		if closure[edge.From] == nil {
			closure[edge.From] = make(map[string]bool)
		}
		closure[edge.From][edge.To] = true
	}

	// Floyd-Warshall-like algorithm for transitive closure
	changed := true
	for changed {
		changed = false
		for from, targets := range closure {
			for target := range targets {
				if closure[target] != nil {
					for transitiveTarget := range closure[target] {
						if !closure[from][transitiveTarget] {
							closure[from][transitiveTarget] = true
							changed = true
						}
					}
				}
			}
		}
	}

	return closure
}

// FindCycles finds all cycles in the graph using Tarjan's algorithm.
func (g *Graph) FindCycles() [][]string {
	index := 0
	stack := make([]string, 0)
	onStack := make(map[string]bool)
	indices := make(map[string]int)
	lowlink := make(map[string]int)
	var cycles [][]string

	var strongConnect func(v string)
	strongConnect = func(v string) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range g.Successors(v) {
			if _, ok := indices[w]; !ok {
				strongConnect(w)
				lowlink[v] = min(lowlink[v], lowlink[w])
			} else if onStack[w] {
				lowlink[v] = min(lowlink[v], indices[w])
			}
		}

		if lowlink[v] == indices[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			if len(scc) > 1 {
				cycles = append(cycles, scc)
			}
		}
	}

	for v := range g.Nodes {
		if _, ok := indices[v]; !ok {
			strongConnect(v)
		}
	}

	return cycles
}

// TopologicalSort returns a topological ordering of the graph nodes.
// Returns an error if the graph has cycles.
func (g *Graph) TopologicalSort() ([]string, error) {
	visited := make(map[string]bool)
	temp := make(map[string]bool)
	result := make([]string, 0)

	var visit func(string) error
	visit = func(n string) error {
		if temp[n] {
			return fmt.Errorf("cycle detected at node %s", n)
		}
		if visited[n] {
			return nil
		}
		temp[n] = true
		for _, succ := range g.Successors(n) {
			if err := visit(succ); err != nil {
				return err
			}
		}
		temp[n] = false
		visited[n] = true
		result = append(result, n)
		return nil
	}

	// Sort nodes for deterministic output
	nodes := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		nodes = append(nodes, id)
	}
	sort.Strings(nodes)

	for _, n := range nodes {
		if !visited[n] {
			if err := visit(n); err != nil {
				return nil, err
			}
		}
	}

	// Reverse for topological order (dependencies first)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result, nil
}

// ReachableFrom returns all nodes reachable from the given node (including itself).
func (g *Graph) ReachableFrom(start string) []string {
	visited := make(map[string]bool)
	var result []string

	var dfs func(string)
	dfs = func(n string) {
		if visited[n] {
			return
		}
		visited[n] = true
		result = append(result, n)
		for _, succ := range g.Successors(n) {
			dfs(succ)
		}
	}

	dfs(start)
	return result
}

// TransitiveCallers returns all callers up to the given depth using BFS.
// depth=1 means direct callers only; depth<=0 means unlimited (capped at 10).
func (g *Graph) TransitiveCallers(targetID string, depth int) []*Edge {
	if depth <= 0 {
		depth = 10
	}
	return g.bfsDirection(targetID, depth, true)
}

// TransitiveCallees returns all callees up to the given depth using BFS.
// depth=1 means direct callees only; depth<=0 means unlimited (capped at 10).
func (g *Graph) TransitiveCallees(targetID string, depth int) []*Edge {
	if depth <= 0 {
		depth = 10
	}
	return g.bfsDirection(targetID, depth, false)
}

// bfsDirection performs bounded BFS in caller or callee direction.
// If reverse=true, traverses callers (reverseAdj); if false, traverses callees (adjacency).
func (g *Graph) bfsDirection(startID string, depth int, reverse bool) []*Edge {
	visited := make(map[string]bool)
	visited[startID] = true

	var result []*Edge
	type nodeDepth struct {
		id    string
		depth int
	}
	queue := []nodeDepth{{startID, 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth >= depth {
			continue
		}

		var neighbors []string
		var edges []*Edge
		if reverse {
			neighbors = g.Predecessors(current.id)
		} else {
			neighbors = g.Successors(current.id)
		}

		for _, neighborID := range neighbors {
			if visited[neighborID] {
				continue
			}
			visited[neighborID] = true

			edge := &Edge{
				From: current.id,
				To:   neighborID,
			}
			if reverse {
				edge.From = neighborID
				edge.To = current.id
			}
			edges = append(edges, edge)
			result = append(result, edge)
			queue = append(queue, nodeDepth{neighborID, current.depth + 1})
		}

		_ = edges // used for tracking
	}

	return result
}

// Callers returns the direct callers of an entity.
func (g *Graph) Callers(entityID string) []string {
	return g.Predecessors(entityID)
}

// Callees returns the direct callees of an entity.
func (g *Graph) Callees(entityID string) []string {
	return g.Successors(entityID)
}

// Reaching returns all nodes that can reach the given node (including itself).
func (g *Graph) Reaching(target string) []string {
	visited := make(map[string]bool)
	var result []string

	var dfs func(string)
	dfs = func(n string) {
		if visited[n] {
			return
		}
		visited[n] = true
		result = append(result, n)
		for _, pred := range g.Predecessors(n) {
			dfs(pred)
		}
	}

	dfs(target)
	return result
}