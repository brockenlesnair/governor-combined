package callgraph

import "context"

// SourceParser is the interface that language-specific parsers must implement.
type SourceParser interface {
	// ParsePackages discovers all packages in the given directory.
	ParsePackages(ctx context.Context, rootPath string) ([]*PackageInfo, error)
	// ParseFile parses a single file and extracts call graph nodes and edges.
	ParseFile(ctx context.Context, filePath string, pkgInfo *PackageInfo) ([]*Node, []*Edge, error)
}

// NodeKind represents the kind of a node in the call graph.
type NodeKind string

const (
	NodeKindFunction NodeKind = "function"
	NodeKindMethod   NodeKind = "method"
	NodeKindType     NodeKind = "type"
	NodeKindPackage  NodeKind = "package"
)

// Node represents a node in the call graph.
type Node struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Package   string   `json:"package"`
	File      string   `json:"file"`
	Line      int      `json:"line"`
	Kind      NodeKind `json:"kind"`
	Receiver  string   `json:"receiver,omitempty"` // for methods
	Exported  bool     `json:"exported"`
	Signature string   `json:"signature,omitempty"` // e.g. "func(ctx context.Context, name string) error"

	// Computed metrics
	FanIn    int  `json:"fanIn,omitempty"`
	FanOut   int  `json:"fanOut,omitempty"`
	InCycle  bool `json:"inCycle,omitempty"`
	IsLeaf   bool `json:"isLeaf,omitempty"`
	IsRoot   bool `json:"isRoot,omitempty"`
}

// Short returns the short name of the node (without package prefix).
func (n *Node) Short() string {
	// Name is typically "pkg.Name" — extract just the name part
	for i := len(n.Name) - 1; i >= 0; i-- {
		if n.Name[i] == '.' {
			return n.Name[i+1:]
		}
	}
	return n.Name
}

// Edge represents a directed edge in the call graph.
type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	CallType string `json:"callType"` // "direct", "interface", "dynamic"
}

// Graph represents the call graph.
type Graph struct {
	Nodes map[string]*Node `json:"nodes"`
	Edges []*Edge          `json:"edges"`

	// Adjacency lists for efficient traversal
	adjacency  map[string][]string
	reverseAdj map[string][]string
}

// NewGraph creates a new empty graph.
func NewGraph() *Graph {
	return &Graph{
		Nodes:      make(map[string]*Node),
		Edges:      make([]*Edge, 0),
		adjacency:  make(map[string][]string),
		reverseAdj: make(map[string][]string),
	}
}

// AddNode adds a node to the graph.
func (g *Graph) AddNode(node *Node) {
	g.Nodes[node.ID] = node
	if _, ok := g.adjacency[node.ID]; !ok {
		g.adjacency[node.ID] = make([]string, 0)
	}
	if _, ok := g.reverseAdj[node.ID]; !ok {
		g.reverseAdj[node.ID] = make([]string, 0)
	}
}

// AddEdge adds an edge to the graph.
func (g *Graph) AddEdge(from, to, callType string) {
	edge := &Edge{From: from, To: to, CallType: callType}
	g.Edges = append(g.Edges, edge)
	g.adjacency[from] = append(g.adjacency[from], to)
	g.reverseAdj[to] = append(g.reverseAdj[to], from)
}

// GetNode returns a node by ID.
func (g *Graph) GetNode(id string) *Node {
	return g.Nodes[id]
}

// Successors returns the direct successors of a node.
func (g *Graph) Successors(id string) []string {
	return g.adjacency[id]
}

// Predecessors returns the direct predecessors of a node.
func (g *Graph) Predecessors(id string) []string {
	return g.reverseAdj[id]
}

// NodeCount returns the number of nodes in the graph.
func (g *Graph) NodeCount() int {
	return len(g.Nodes)
}

// EdgeCount returns the number of edges in the graph.
func (g *Graph) EdgeCount() int {
	return len(g.Edges)
}

// Package represents a Go package in the call graph.
type Package struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	Files      []string `json:"files"`
	Imports    []string `json:"imports"`
	Exports    []string `json:"exports"`
	NodeIDs    []string `json:"nodeIds"`
}