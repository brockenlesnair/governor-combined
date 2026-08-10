package callgraph

import (
	"fmt"
	"sort"
)

// Analyzer performs analysis on the call graph.
type Analyzer struct{}

// NewAnalyzer creates a new analyzer.
func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

// Analyze runs all analysis passes on the graph.
func (a *Analyzer) Analyze(g *Graph) {
	a.computeMetrics(g)
	a.detectPatterns(g)
}

// computeMetrics computes various graph metrics.
func (a *Analyzer) computeMetrics(g *Graph) {
	for _, node := range g.Nodes {
		// Compute fan-in (number of callers)
		node.FanIn = len(g.Predecessors(node.ID))

		// Compute fan-out (number of callees)
		node.FanOut = len(g.Successors(node.ID))
	}
}

// detectPatterns detects common patterns in the call graph.
func (a *Analyzer) detectPatterns(g *Graph) {
	// Mark nodes that are part of cycles
	cycles := g.FindCycles()
	for _, cycle := range cycles {
		for _, nodeID := range cycle {
			if node := g.GetNode(nodeID); node != nil {
				node.InCycle = true
			}
		}
	}

	// Mark leaf nodes (no outgoing edges)
	for _, node := range g.Nodes {
		if len(g.Successors(node.ID)) == 0 {
			node.IsLeaf = true
		}
	}

	// Mark root nodes (no incoming edges)
	for _, node := range g.Nodes {
		if len(g.Predecessors(node.ID)) == 0 {
			node.IsRoot = true
		}
	}
}

// ImpactAnalysis analyzes the impact of changing a node.
type ImpactAnalysis struct {
	Target      string   `json:"target"`
	DirectDeps  []string `json:"directDeps"`
	TransDeps   []string `json:"transDeps"`
	Affected    []string `json:"affected"`
	RiskLevel   string   `json:"riskLevel"`
	Description string   `json:"description"`
}

// AnalyzeImpact computes the impact of changing a given node.
func (a *Analyzer) AnalyzeImpact(g *Graph, nodeID string) *ImpactAnalysis {
	node := g.GetNode(nodeID)
	if node == nil {
		return &ImpactAnalysis{
			Target:      nodeID,
			Description: "Node not found in graph",
		}
	}

	// Direct dependents (nodes that call this node)
	directDeps := g.Predecessors(nodeID)

	// Transitive dependents (all nodes that reach this node)
	transDeps := g.Reaching(nodeID)

	// All affected nodes (transitive closure)
	affected := make([]string, 0, len(transDeps))
	for _, dep := range transDeps {
		if dep != nodeID {
			affected = append(affected, dep)
		}
	}

	// Compute risk level based on number of affected nodes
	riskLevel := "low"
	if len(affected) > 50 {
		riskLevel = "critical"
	} else if len(affected) > 20 {
		riskLevel = "high"
	} else if len(affected) > 10 {
		riskLevel = "medium"
	}

	desc := fmt.Sprintf("Changing %s (%s) affects %d functions directly and %d transitively",
		node.Name, node.Kind, len(directDeps), len(affected))

	return &ImpactAnalysis{
		Target:      nodeID,
		DirectDeps:  directDeps,
		TransDeps:   transDeps,
		Affected:    affected,
		RiskLevel:   riskLevel,
		Description: desc,
	}
}

// CentralityMetrics computes centrality metrics for nodes.
type CentralityMetrics struct {
	Betweenness map[string]float64 `json:"betweenness"`
	Closeness   map[string]float64 `json:"closeness"`
	Degree      map[string]int     `json:"degree"`
}

// ComputeCentrality computes centrality metrics for all nodes.
func (a *Analyzer) ComputeCentrality(g *Graph) *CentralityMetrics {
	betweenness := make(map[string]float64)
	closeness := make(map[string]float64)
	degree := make(map[string]int)

	// Degree centrality (in + out)
	for id, node := range g.Nodes {
		degree[id] = node.FanIn + node.FanOut
	}

	// Betweenness centrality (simplified)
	// For each pair of nodes, count how many shortest paths go through each node
	allNodes := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		allNodes = append(allNodes, id)
	}
	sort.Strings(allNodes)

	// For large graphs, this is expensive. Use a sampling approach.
	sampleSize := min(len(allNodes), 100)
	for i := 0; i < sampleSize; i++ {
		source := allNodes[i]
		paths := a.findAllShortestPaths(g, source)
		for target, path := range paths {
			if source == target {
				continue
			}
			for _, intermediate := range path[1 : len(path)-1] {
				betweenness[intermediate]++
			}
		}
	}

	// Normalize
	maxBetweenness := 0.0
	for _, v := range betweenness {
		if v > maxBetweenness {
			maxBetweenness = v
		}
	}
	if maxBetweenness > 0 {
		for k := range betweenness {
			betweenness[k] /= maxBetweenness
		}
	}

	// Closeness centrality (average distance to all other nodes)
	for _, source := range allNodes {
		distances := a.computeDistances(g, source)
		sum := 0.0
		count := 0
		for _, d := range distances {
			if d > 0 {
				sum += float64(d)
				count++
			}
		}
		if count > 0 {
			closeness[source] = float64(count) / sum
		}
	}

	return &CentralityMetrics{
		Betweenness: betweenness,
		Closeness:   closeness,
		Degree:      degree,
	}
}

// findAllShortestPaths finds shortest paths from source to all reachable nodes.
func (a *Analyzer) findAllShortestPaths(g *Graph, source string) map[string][]string {
	dist := make(map[string]int)
	prev := make(map[string]string)
	queue := []string{source}
	dist[source] = 0

	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]

		for _, v := range g.Successors(u) {
			if _, ok := dist[v]; !ok {
				dist[v] = dist[u] + 1
				prev[v] = u
				queue = append(queue, v)
			}
		}
	}

	paths := make(map[string][]string)
	for target := range dist {
		if target == source {
			continue
		}
		var path []string
		curr := target
		for curr != source {
			path = append([]string{curr}, path...)
			curr = prev[curr]
		}
		path = append([]string{source}, path...)
		paths[target] = path
	}

	return paths
}

// computeDistances computes distances from source to all reachable nodes.
func (a *Analyzer) computeDistances(g *Graph, source string) map[string]int {
	dist := make(map[string]int)
	queue := []string{source}
	dist[source] = 0

	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]

		for _, v := range g.Successors(u) {
			if _, ok := dist[v]; !ok {
				dist[v] = dist[u] + 1
				queue = append(queue, v)
			}
		}
	}

	return dist
}

// DeadCodeAnalysis finds potentially dead code.
type DeadCodeAnalysis struct {
	Unreachable   []string `json:"unreachable"`
	UnusedExports []string `json:"unusedExports"`
	Orphaned      []string `json:"orphaned"`
}

// AnalyzeDeadCode finds potentially dead code in the graph.
func (a *Analyzer) AnalyzeDeadCode(g *Graph) *DeadCodeAnalysis {
	result := &DeadCodeAnalysis{
		Unreachable:   make([]string, 0),
		UnusedExports: make([]string, 0),
		Orphaned:      make([]string, 0),
	}

	// Find unreachable nodes (not reachable from any exported function)
	exportedRoots := make([]string, 0)
	for _, node := range g.Nodes {
		if node.Exported && (node.Kind == NodeKindFunction || node.Kind == NodeKindMethod) {
			exportedRoots = append(exportedRoots, node.ID)
		}
	}

	reachable := make(map[string]bool)
	for _, root := range exportedRoots {
		for _, node := range g.ReachableFrom(root) {
			reachable[node] = true
		}
	}

	for _, node := range g.Nodes {
		if !reachable[node.ID] && (node.Kind == NodeKindFunction || node.Kind == NodeKindMethod) {
			if !node.Exported {
				result.Unreachable = append(result.Unreachable, node.ID)
			} else if node.Kind == NodeKindFunction || (node.Kind == NodeKindMethod && node.Exported) {
				// Exported but not reachable from any entry point
				result.UnusedExports = append(result.UnusedExports, node.ID)
			}
		}
	}

	// Find orphaned nodes (no incoming or outgoing edges)
	for _, node := range g.Nodes {
		if len(g.Successors(node.ID)) == 0 && len(g.Predecessors(node.ID)) == 0 {
			result.Orphaned = append(result.Orphaned, node.ID)
		}
	}

	return result
}

// PackageMetrics holds metrics for a package.
type PackageMetrics struct {
	Package     string  `json:"package"`
	NodeCount   int     `json:"nodeCount"`
	EdgeCount   int     `json:"edgeCount"`
	AvgFanIn    float64 `json:"avgFanIn"`
	AvgFanOut   float64 `json:"avgFanOut"`
	MaxFanIn    int     `json:"maxFanIn"`
	MaxFanOut   int     `json:"maxFanOut"`
	CyclicDepth int     `json:"cyclicDepth"`
}

// ComputePackageMetrics computes metrics for each package.
func (a *Analyzer) ComputePackageMetrics(g *Graph) map[string]*PackageMetrics {
	pkgNodes := make(map[string][]*Node)
	for _, node := range g.Nodes {
		pkgNodes[node.Package] = append(pkgNodes[node.Package], node)
	}

	metrics := make(map[string]*PackageMetrics)
	for pkg, nodes := range pkgNodes {
		pkgEdges := 0
		totalFanIn := 0
		totalFanOut := 0
		maxFanIn := 0
		maxFanOut := 0

		for _, node := range nodes {
			fanIn := len(g.Predecessors(node.ID))
			fanOut := len(g.Successors(node.ID))
			totalFanIn += fanIn
			totalFanOut += fanOut
			if fanIn > maxFanIn {
				maxFanIn = fanIn
			}
			if fanOut > maxFanOut {
				maxFanOut = fanOut
			}
			pkgEdges += fanOut
		}

		avgFanIn := 0.0
		avgFanOut := 0.0
		if len(nodes) > 0 {
			avgFanIn = float64(totalFanIn) / float64(len(nodes))
			avgFanOut = float64(totalFanOut) / float64(len(nodes))
		}

		// Compute cyclic depth
		cyclicDepth := 0
		cycles := g.FindCycles()
		for _, cycle := range cycles {
			inPkg := 0
			for _, nodeID := range cycle {
				if n := g.GetNode(nodeID); n != nil && n.Package == pkg {
					inPkg++
				}
			}
			if inPkg > cyclicDepth {
				cyclicDepth = inPkg
			}
		}

		metrics[pkg] = &PackageMetrics{
			Package:     pkg,
			NodeCount:   len(nodes),
			EdgeCount:   pkgEdges,
			AvgFanIn:    avgFanIn,
			AvgFanOut:   avgFanOut,
			MaxFanIn:    maxFanIn,
			MaxFanOut:   maxFanOut,
			CyclicDepth: cyclicDepth,
		}
	}

	return metrics
}

// CrossPackageEdges returns edges that cross package boundaries.
func (a *Analyzer) CrossPackageEdges(g *Graph) []*Edge {
	var crossEdges []*Edge
	for _, edge := range g.Edges {
		fromNode := g.GetNode(edge.From)
		toNode := g.GetNode(edge.To)
		if fromNode != nil && toNode != nil && fromNode.Package != toNode.Package {
			crossEdges = append(crossEdges, edge)
		}
	}
	return crossEdges
}

// StronglyConnectedComponents returns the strongly connected components.
func (a *Analyzer) StronglyConnectedComponents(g *Graph) [][]string {
	// Use Tarjan's algorithm
	index := 0
	stack := make([]string, 0)
	onStack := make(map[string]bool)
	indices := make(map[string]int)
	lowlink := make(map[string]int)
	var sccs [][]string

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
			sccs = append(sccs, scc)
		}
	}

	for v := range g.Nodes {
		if _, ok := indices[v]; !ok {
			strongConnect(v)
		}
	}

	return sccs
}