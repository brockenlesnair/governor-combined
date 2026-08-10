package callgraph

import (
	"context"
	"testing"
)

func TestGraphBuilder_Build(t *testing.T) {
	builder := NewGraphBuilder()

	// Use a simple test directory
	graph, err := builder.Build(context.Background(), ".")
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if graph == nil {
		t.Fatal("Graph should not be nil")
	}

	t.Logf("Graph built with %d nodes and %d edges", graph.NodeCount(), graph.EdgeCount())
}

func TestGraph_Successors_Predecessors(t *testing.T) {
	g := NewGraph()

	nodeA := &Node{ID: "A", Name: "A", Package: "test", Kind: NodeKindFunction}
	nodeB := &Node{ID: "B", Name: "B", Package: "test", Kind: NodeKindFunction}
	nodeC := &Node{ID: "C", Name: "C", Package: "test", Kind: NodeKindFunction}

	g.AddNode(nodeA)
	g.AddNode(nodeB)
	g.AddNode(nodeC)

	g.AddEdge("A", "B", "direct")
	g.AddEdge("B", "C", "direct")
	g.AddEdge("A", "C", "direct")

	succ := g.Successors("A")
	if len(succ) != 2 {
		t.Errorf("Expected 2 successors for A, got %d", len(succ))
	}

	pred := g.Predecessors("C")
	if len(pred) != 2 {
		t.Errorf("Expected 2 predecessors for C, got %d", len(pred))
	}
}

func TestGraph_ReachableFrom(t *testing.T) {
	g := NewGraph()

	nodeA := &Node{ID: "A", Name: "A", Package: "test", Kind: NodeKindFunction}
	nodeB := &Node{ID: "B", Name: "B", Package: "test", Kind: NodeKindFunction}
	nodeC := &Node{ID: "C", Name: "C", Package: "test", Kind: NodeKindFunction}
	nodeD := &Node{ID: "D", Name: "D", Package: "test", Kind: NodeKindFunction}

	g.AddNode(nodeA)
	g.AddNode(nodeB)
	g.AddNode(nodeC)
	g.AddNode(nodeD)

	g.AddEdge("A", "B", "direct")
	g.AddEdge("B", "C", "direct")
	g.AddEdge("A", "D", "direct")

	reachable := g.ReachableFrom("A")
	if len(reachable) != 4 {
		t.Errorf("Expected 4 reachable from A, got %d: %v", len(reachable), reachable)
	}
}

func TestGraph_Reaching(t *testing.T) {
	g := NewGraph()

	nodeA := &Node{ID: "A", Name: "A", Package: "test", Kind: NodeKindFunction}
	nodeB := &Node{ID: "B", Name: "B", Package: "test", Kind: NodeKindFunction}
	nodeC := &Node{ID: "C", Name: "C", Package: "test", Kind: NodeKindFunction}

	g.AddNode(nodeA)
	g.AddNode(nodeB)
	g.AddNode(nodeC)

	g.AddEdge("A", "B", "direct")
	g.AddEdge("B", "C", "direct")

	reaching := g.Reaching("C")
	if len(reaching) != 3 {
		t.Errorf("Expected 3 reaching C, got %d: %v", len(reaching), reaching)
	}
}

func TestGraph_FindCycles(t *testing.T) {
	g := NewGraph()

	nodeA := &Node{ID: "A", Name: "A", Package: "test", Kind: NodeKindFunction}
	nodeB := &Node{ID: "B", Name: "B", Package: "test", Kind: NodeKindFunction}
	nodeC := &Node{ID: "C", Name: "C", Package: "test", Kind: NodeKindFunction}

	g.AddNode(nodeA)
	g.AddNode(nodeB)
	g.AddNode(nodeC)

	g.AddEdge("A", "B", "direct")
	g.AddEdge("B", "C", "direct")
	g.AddEdge("C", "A", "direct") // Cycle

	cycles := g.FindCycles()
	if len(cycles) != 1 {
		t.Errorf("Expected 1 cycle, got %d: %v", len(cycles), cycles)
	}
}

func TestGraph_TopologicalSort(t *testing.T) {
	g := NewGraph()

	nodeA := &Node{ID: "A", Name: "A", Package: "test", Kind: NodeKindFunction}
	nodeB := &Node{ID: "B", Name: "B", Package: "test", Kind: NodeKindFunction}
	nodeC := &Node{ID: "C", Name: "C", Package: "test", Kind: NodeKindFunction}

	g.AddNode(nodeA)
	g.AddNode(nodeB)
	g.AddNode(nodeC)

	g.AddEdge("A", "B", "direct")
	g.AddEdge("B", "C", "direct")

	order, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}

	// A should come before B, B before C
	aIdx := -1
	bIdx := -1
	cIdx := -1
	for i, n := range order {
		switch n {
		case "A":
			aIdx = i
		case "B":
			bIdx = i
		case "C":
			cIdx = i
		}
	}

	if aIdx > bIdx || bIdx > cIdx {
		t.Errorf("Invalid topological order: %v", order)
	}
}

func TestGraph_TopologicalSort_Cycle(t *testing.T) {
	g := NewGraph()

	nodeA := &Node{ID: "A", Name: "A", Package: "test", Kind: NodeKindFunction}
	nodeB := &Node{ID: "B", Name: "B", Package: "test", Kind: NodeKindFunction}

	g.AddNode(nodeA)
	g.AddNode(nodeB)

	g.AddEdge("A", "B", "direct")
	g.AddEdge("B", "A", "direct") // Cycle

	_, err := g.TopologicalSort()
	if err == nil {
		t.Error("Expected error for cyclic graph")
	}
}

func TestAnalyzer_AnalyzeImpact(t *testing.T) {
	g := NewGraph()

	nodeA := &Node{ID: "A", Name: "A", Package: "test", Kind: NodeKindFunction, Exported: true}
	nodeB := &Node{ID: "B", Name: "B", Package: "test", Kind: NodeKindFunction}
	nodeC := &Node{ID: "C", Name: "C", Package: "test", Kind: NodeKindFunction}

	g.AddNode(nodeA)
	g.AddNode(nodeB)
	g.AddNode(nodeC)

	g.AddEdge("B", "A", "direct")
	g.AddEdge("C", "B", "direct")

	analyzer := NewAnalyzer()
	analyzer.Analyze(g)

	impact := analyzer.AnalyzeImpact(g, "A")
	if len(impact.DirectDeps) != 1 || impact.DirectDeps[0] != "B" {
		t.Errorf("Expected direct dep B, got %v", impact.DirectDeps)
	}

	if len(impact.Affected) != 2 {
		t.Errorf("Expected 2 affected nodes, got %d: %v", len(impact.Affected), impact.Affected)
	}
}

func TestAnalyzer_AnalyzeDeadCode(t *testing.T) {
	g := NewGraph()

	// Exported function (entry point)
	nodeA := &Node{ID: "A", Name: "A", Package: "test", Kind: NodeKindFunction, Exported: true}
	// Called by A
	nodeB := &Node{ID: "B", Name: "B", Package: "test", Kind: NodeKindFunction}
	// Not reachable from entry points
	nodeC := &Node{ID: "C", Name: "C", Package: "test", Kind: NodeKindFunction}
	// Exported but not reachable from OTHER entry points (D is its own entry point)
	nodeD := &Node{ID: "D", Name: "D", Package: "test", Kind: NodeKindFunction, Exported: true}
	// Orphaned
	nodeE := &Node{ID: "E", Name: "E", Package: "test", Kind: NodeKindFunction}

	g.AddNode(nodeA)
	g.AddNode(nodeB)
	g.AddNode(nodeC)
	g.AddNode(nodeD)
	g.AddNode(nodeE)

	g.AddEdge("A", "B", "direct")

	analyzer := NewAnalyzer()
	analyzer.Analyze(g)

	deadCode := analyzer.AnalyzeDeadCode(g)

	// C should be unreachable (not exported, not reachable from entry points)
	foundC := false
	for _, n := range deadCode.Unreachable {
		if n == "C" {
			foundC = true
			break
		}
	}
	if !foundC {
		t.Errorf("C should be in unreachable, got %v", deadCode.Unreachable)
	}

	// D is exported so it's an entry point itself - NOT in unusedExports
	// (This is correct behavior - exported functions are considered reachable from themselves)

	// E should be orphaned
	foundE := false
	for _, n := range deadCode.Orphaned {
		if n == "E" {
			foundE = true
			break
		}
	}
	if !foundE {
		t.Errorf("E should be in orphaned, got %v", deadCode.Orphaned)
	}
}