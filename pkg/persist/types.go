// Package persist handles serialization and storage of call graphs and related
// data structures. It supports JSON and SQLite backends for caching computed
// graphs across analysis runs.
package persist

// Node represents a node in the call graph (simplified for persistence).
type Node struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Package  string `json:"package"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Kind     string `json:"kind"`
	Receiver string `json:"receiver,omitempty"`
	Exported bool   `json:"exported"`

	// Computed metrics
	FanIn    int  `json:"fanIn,omitempty"`
	FanOut   int  `json:"fanOut,omitempty"`
	InCycle  bool `json:"inCycle,omitempty"`
	IsLeaf   bool `json:"isLeaf,omitempty"`
	IsRoot   bool `json:"isRoot,omitempty"`
}

// Edge represents an edge in the call graph.
type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	CallType string `json:"callType"`
}

// CallGraph represents a call graph for persistence.
type CallGraph struct {
	Nodes map[string]*Node `json:"nodes"`
	Edges []*Edge          `json:"edges"`

	// Adjacency lists for efficient traversal
	adjacency  map[string][]string
	reverseAdj map[string][]string
}

// NewCallGraph creates a new empty call graph.
func NewCallGraph() *CallGraph {
	return &CallGraph{
		Nodes:      make(map[string]*Node),
		Edges:      make([]*Edge, 0),
		adjacency:  make(map[string][]string),
		reverseAdj: make(map[string][]string),
	}
}

// AddNode adds a node to the graph.
func (g *CallGraph) AddNode(node *Node) {
	g.Nodes[node.ID] = node
	if _, ok := g.adjacency[node.ID]; !ok {
		g.adjacency[node.ID] = make([]string, 0)
	}
	if _, ok := g.reverseAdj[node.ID]; !ok {
		g.reverseAdj[node.ID] = make([]string, 0)
	}
}

// AddEdge adds an edge to the graph.
func (g *CallGraph) AddEdge(from, to, callType string) {
	edge := &Edge{From: from, To: to, CallType: callType}
	g.Edges = append(g.Edges, edge)
	g.adjacency[from] = append(g.adjacency[from], to)
	g.reverseAdj[to] = append(g.reverseAdj[to], from)
}

// GetNode returns a node by ID.
func (g *CallGraph) GetNode(id string) *Node {
	return g.Nodes[id]
}

// Successors returns the direct successors of a node.
func (g *CallGraph) Successors(id string) []string {
	return g.adjacency[id]
}

// Predecessors returns the direct predecessors of a node.
func (g *CallGraph) Predecessors(id string) []string {
	return g.reverseAdj[id]
}

// NodeCount returns the number of nodes in the graph.
func (g *CallGraph) NodeCount() int {
	return len(g.Nodes)
}

// EdgeCount returns the number of edges in the graph.
func (g *CallGraph) EdgeCount() int {
	return len(g.Edges)
}