package deadcode

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
)

// --- Test helpers ---

func newTestGraph() *callgraph.Graph {
	return callgraph.NewGraph()
}

func addNode(g *callgraph.Graph, id, name, pkg, file string, line int, kind callgraph.NodeKind, exported bool, sig string) *callgraph.Node {
	n := &callgraph.Node{
		ID:        id,
		Name:      name,
		Package:   pkg,
		File:      file,
		Line:      line,
		Kind:      kind,
		Exported:  exported,
		Signature: sig,
	}
	g.AddNode(n)
	return n
}

func addEdge(g *callgraph.Graph, from, to, callType string) {
	g.AddEdge(from, to, callType)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// buildDiamondGraph creates a diamond-shaped call graph:
//
//	main.main -> pkgA.FuncA
//	main.main -> pkgB.FuncB
//	pkgA.FuncA -> pkgC.FuncC
//	pkgB.FuncB -> pkgC.FuncC
//	pkgD.FuncD (isolated, no edges)
func buildDiamondGraph() *callgraph.Graph {
	g := newTestGraph()
	addNode(g, "main.main", "main.main", "main", "main.go", 10, callgraph.NodeKindFunction, true, "func()")
	addNode(g, "pkgA.FuncA", "pkgA.FuncA", "pkgA", "a.go", 5, callgraph.NodeKindFunction, true, "func()")
	addNode(g, "pkgB.FuncB", "pkgB.FuncB", "pkgB", "b.go", 5, callgraph.NodeKindFunction, true, "func()")
	addNode(g, "pkgC.FuncC", "pkgC.FuncC", "pkgC", "c.go", 5, callgraph.NodeKindFunction, false, "func()")
	addNode(g, "pkgD.FuncD", "pkgD.FuncD", "pkgD", "d.go", 5, callgraph.NodeKindFunction, false, "func()")

	addEdge(g, "main.main", "pkgA.FuncA", "direct")
	addEdge(g, "main.main", "pkgB.FuncB", "direct")
	addEdge(g, "pkgA.FuncA", "pkgC.FuncC", "direct")
	addEdge(g, "pkgB.FuncB", "pkgC.FuncC", "direct")

	return g
}

// --- Tests ---

func TestNewDetector(t *testing.T) {
	g := newTestGraph()
	d := NewDetector(g, testLogger())
	if d == nil {
		t.Fatal("NewDetector returned nil")
	}
	if d.graph != g {
		t.Fatal("NewDetector did not store graph")
	}
}

func TestNewDetector_NilLogger(t *testing.T) {
	g := newTestGraph()
	d := NewDetector(g, nil)
	if d == nil {
		t.Fatal("NewDetector returned nil")
	}
	if d.logger == nil {
		t.Fatal("default logger not set")
	}
}

func TestDetect_EmptyGraph(t *testing.T) {
	g := newTestGraph()
	d := NewDetector(g, testLogger())
	_, err := d.Detect(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty graph")
	}
	var dcErr *DeadCodeError
	if !strings.Contains(err.Error(), "GRAPH_EMPTY") {
		t.Fatalf("expected GRAPH_EMPTY error, got: %v", err)
	}
	_ = dcErr
}

func TestDetect_AllReachable(t *testing.T) {
	g := newTestGraph()
	addNode(g, "main.main", "main.main", "main", "main.go", 10, callgraph.NodeKindFunction, true, "func()")
	addNode(g, "pkgA.Helper", "pkgA.Helper", "pkgA", "a.go", 5, callgraph.NodeKindFunction, false, "func()")
	addNode(g, "pkgB.Util", "pkgB.Util", "pkgB", "b.go", 5, callgraph.NodeKindFunction, false, "func()")

	addEdge(g, "main.main", "pkgA.Helper", "direct")
	addEdge(g, "pkgA.Helper", "pkgB.Util", "direct")

	d := NewDetector(g, testLogger())
	result, err := d.Detect(context.Background(), &Config{
		ExcludeExported: false,
		ExcludeTests:    true,
		MinConfidence:   0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DeadCount != 0 {
		t.Fatalf("expected 0 dead, got %d (dead: %+v)", result.DeadCount, result.DeadCode)
	}
	if result.AliveCount != 3 {
		t.Fatalf("expected 3 alive, got %d", result.AliveCount)
	}
}

func TestDetect_NoneReachable(t *testing.T) {
	g := newTestGraph()
	// No entry points (no main, no init, no exported in cmd)
	addNode(g, "pkgA.foo", "pkgA.foo", "pkgA", "a.go", 5, callgraph.NodeKindFunction, false, "func()")
	addNode(g, "pkgB.bar", "pkgB.bar", "pkgB", "b.go", 10, callgraph.NodeKindFunction, false, "func()")

	d := NewDetector(g, testLogger())
	result, err := d.Detect(context.Background(), &Config{
		ExcludeExported: false,
		ExcludeTests:    true,
		MinConfidence:   0.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DeadCount != 2 {
		t.Fatalf("expected 2 dead, got %d", result.DeadCount)
	}
}

func TestDetect_Mixed(t *testing.T) {
	g := buildDiamondGraph()
	d := NewDetector(g, testLogger())
	result, err := d.Detect(context.Background(), &Config{
		ExcludeExported: false,
		ExcludeTests:    true,
		MinConfidence:   0.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	// main.main, pkgA.FuncA, pkgB.FuncB, pkgC.FuncC are all reachable
	// pkgD.FuncD is isolated = dead
	if result.DeadCount != 1 {
		t.Fatalf("expected 1 dead, got %d (dead: %+v)", result.DeadCount, result.DeadCode)
	}
	if result.DeadCode[0].ID != "pkgD.FuncD" {
		t.Fatalf("expected pkgD.FuncD to be dead, got %s", result.DeadCode[0].ID)
	}
}

func TestDetect_ExcludeExported(t *testing.T) {
	g := newTestGraph()
	addNode(g, "main.main", "main.main", "main", "main.go", 10, callgraph.NodeKindFunction, true, "func()")
	addNode(g, "pkgA.ExportedFunc", "pkgA.ExportedFunc", "pkgA", "a.go", 5, callgraph.NodeKindFunction, true, "func()")
	addNode(g, "pkgB.unexportedFunc", "pkgB.unexportedFunc", "pkgB", "b.go", 5, callgraph.NodeKindFunction, false, "func()")

	addEdge(g, "main.main", "pkgA.ExportedFunc", "direct")
	// pkgB.unexportedFunc is not reachable and not exported — should be dead

	d := NewDetector(g, testLogger())
	result, err := d.Detect(context.Background(), &Config{
		ExcludeExported: true,
		ExcludeTests:    true,
		MinConfidence:   0.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	// pkgA.ExportedFunc is reachable + exported → excluded (alive)
	// pkgB.unexportedFunc is unreachable, unexported → dead
	if result.DeadCount != 1 {
		t.Fatalf("expected 1 dead, got %d (dead: %+v)", result.DeadCount, result.DeadCode)
	}
}

func TestDetect_ExcludeTests(t *testing.T) {
	g := newTestGraph()
	addNode(g, "main.main", "main.main", "main", "main.go", 10, callgraph.NodeKindFunction, true, "func()")
	addNode(g, "pkgTestSomething.TestFoo", "pkgTestSomething.TestFoo", "pkgTestSomething", "test.go", 20, callgraph.NodeKindFunction, true, "func(t *testing.T)")
	addNode(g, "pkgTestSomething.BenchmarkBar", "pkgTestSomething.BenchmarkBar", "pkgTestSomething", "test.go", 30, callgraph.NodeKindFunction, true, "func(b *testing.B)")

	d := NewDetector(g, testLogger())
	result, err := d.Detect(context.Background(), &Config{
		ExcludeExported: false,
		ExcludeTests:    true,
		MinConfidence:   0.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Test functions should be excluded (alive), not flagged as dead
	for _, dc := range result.DeadCode {
		if strings.HasPrefix(dc.Name, "Test") || strings.HasPrefix(dc.Name, "Benchmark") {
			t.Fatalf("test function %s should have been excluded", dc.Name)
		}
	}
}

func TestDetect_MinConfidence(t *testing.T) {
	g := newTestGraph()
	addNode(g, "main.main", "main.main", "main", "main.go", 10, callgraph.NodeKindFunction, true, "func()")
	addNode(g, "pkgA.lowConfFunc", "pkgA.lowConfFunc", "pkgA", "a.go", 5, callgraph.NodeKindFunction, true, "func()")
	addNode(g, "pkgB.highConfFunc", "pkgB.highConfFunc", "pkgB", "b.go", 10, callgraph.NodeKindFunction, false, "func()")

	d := NewDetector(g, testLogger())

	// With high threshold, only high-confidence dead code shows
	result, err := d.Detect(context.Background(), &Config{
		ExcludeExported: false,
		ExcludeTests:    true,
		MinConfidence:   0.9,
	})
	if err != nil {
		t.Fatal(err)
	}
	// pkgA.lowConfFunc: exported, in library pkg, no callers → 0.7 * 0.6 = 0.42 < 0.9 → filtered
	// pkgB.highConfFunc: unexported, no callers → 0.95 >= 0.9 → included
	for _, dc := range result.DeadCode {
		if dc.Confidence < 0.9 {
			t.Fatalf("expected confidence >= 0.9, got %f for %s", dc.Confidence, dc.Name)
		}
	}
}

func TestReachableSet(t *testing.T) {
	g := buildDiamondGraph()
	ra := NewReachabilityAnalyzer(g)

	entryPoints := []string{"main.main"}
	reachable := ra.ReachableSet(context.Background(), entryPoints)

	expected := map[string]bool{
		"main.main":   true,
		"pkgA.FuncA":  true,
		"pkgB.FuncB":  true,
		"pkgC.FuncC":  true,
		"pkgD.FuncD":  false, // isolated
	}

	for id, expectReachable := range expected {
		if reachable[id] != expectReachable {
			t.Fatalf("node %s: expected reachable=%v, got %v", id, expectReachable, reachable[id])
		}
	}
}

func TestReachableSet_ContextCancelled(t *testing.T) {
	g := newTestGraph()
	addNode(g, "A", "A", "pkg", "a.go", 1, callgraph.NodeKindFunction, false, "func()")
	addNode(g, "B", "B", "pkg", "b.go", 2, callgraph.NodeKindFunction, false, "func()")
	addNode(g, "C", "C", "pkg", "c.go", 3, callgraph.NodeKindFunction, false, "func()")
	addEdge(g, "A", "B", "direct")
	addEdge(g, "B", "C", "direct")

	ra := NewReachabilityAnalyzer(g)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	reachable := ra.ReachableSet(ctx, []string{"A"})

	// With cancelled context, we should get at least the entry point
	// (it's added before the BFS loop checks context)
	if !reachable["A"] {
		t.Fatal("entry point A should be in reachable set even with cancelled context")
	}
	// B and C may or may not be present depending on scheduling
	// The important thing is that it doesn't panic or hang
}

func TestReachableSet_EmptyEntryPoints(t *testing.T) {
	g := buildDiamondGraph()
	ra := NewReachabilityAnalyzer(g)
	reachable := ra.ReachableSet(context.Background(), []string{})
	if len(reachable) != 0 {
		t.Fatalf("expected empty reachable set, got %d", len(reachable))
	}
}

func TestFindEntryPoints_Main(t *testing.T) {
	g := newTestGraph()
	addNode(g, "main.main", "main.main", "main", "main.go", 10, callgraph.NodeKindFunction, true, "func()")
	addNode(g, "pkgA.FuncA", "pkgA.FuncA", "pkgA", "a.go", 5, callgraph.NodeKindFunction, true, "func()")

	ra := NewReachabilityAnalyzer(g)
	eps := ra.FindEntryPoints()

	found := false
	for _, ep := range eps {
		if ep == "main.main" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("main.main should be detected as entry point")
	}
}

func TestFindEntryPoints_Init(t *testing.T) {
	g := newTestGraph()
	addNode(g, "pkgA.init", "pkgA.init", "pkgA", "a.go", 5, callgraph.NodeKindFunction, false, "func()")
	addNode(g, "pkgA.init.foo", "pkgA.init.foo", "pkgA", "a.go", 10, callgraph.NodeKindFunction, false, "func()")
	addNode(g, "pkgA.init#1", "pkgA.init#1", "pkgA", "a.go", 15, callgraph.NodeKindFunction, false, "func()")

	ra := NewReachabilityAnalyzer(g)
	eps := ra.FindEntryPoints()

	initCount := 0
	for _, ep := range eps {
		if strings.Contains(ep, "init") {
			initCount++
		}
	}
	if initCount != 3 {
		t.Fatalf("expected 3 init entry points, got %d (eps: %v)", initCount, eps)
	}
}

func TestFindEntryPoints_HTTPHandler(t *testing.T) {
	g := newTestGraph()
	addNode(g, "pkgA.HandleRequest", "pkgA.HandleRequest", "pkgA", "a.go", 5, callgraph.NodeKindFunction, true,
		"func(http.ResponseWriter, *http.Request)")
	addNode(g, "pkgA.NormalFunc", "pkgA.NormalFunc", "pkgA", "a.go", 10, callgraph.NodeKindFunction, true, "func()")

	ra := NewReachabilityAnalyzer(g)
	eps := ra.FindEntryPoints()

	found := false
	for _, ep := range eps {
		if ep == "pkgA.HandleRequest" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("HTTP handler should be detected as entry point")
	}
}

func TestFindEntryPoints_ExportedInCmd(t *testing.T) {
	g := newTestGraph()
	addNode(g, "cmd.Run", "cmd.Run", "cmd", "run.go", 5, callgraph.NodeKindFunction, true, "func()")
	addNode(g, "lib.Helper", "lib.Helper", "lib", "helper.go", 5, callgraph.NodeKindFunction, true, "func()")

	ra := NewReachabilityAnalyzer(g)
	eps := ra.FindEntryPoints()

	foundCmd := false
	for _, ep := range eps {
		if ep == "cmd.Run" {
			foundCmd = true
			break
		}
	}
	if !foundCmd {
		t.Fatal("exported func in cmd package should be entry point")
	}
}

func TestConfidenceScorer_Reachable(t *testing.T) {
	g := newTestGraph()
	addNode(g, "A", "A", "pkg", "a.go", 1, callgraph.NodeKindFunction, false, "func()")

	scorer := NewConfidenceScorer(g)
	node := g.GetNode("A")
	score := scorer.Score(node, true, 0)
	if score != 0.0 {
		t.Fatalf("expected 0.0 for reachable node, got %f", score)
	}
}

func TestConfidenceScorer_Unexported(t *testing.T) {
	g := newTestGraph()
	addNode(g, "A", "A", "pkg", "a.go", 1, callgraph.NodeKindFunction, false, "func()")

	scorer := NewConfidenceScorer(g)
	node := g.GetNode("A")
	score := scorer.Score(node, false, 0)
	if score != 0.95 {
		t.Fatalf("expected 0.95 for unexported with 0 callers, got %f", score)
	}
}

func TestConfidenceScorer_ExportedLibrary(t *testing.T) {
	g := newTestGraph()
	addNode(g, "A", "A", "github.com/foo/bar", "a.go", 1, callgraph.NodeKindFunction, true, "func()")
	addEdge(g, "A", "B", "direct") // A has a caller (B -> A)

	scorer := NewConfidenceScorer(g)
	node := g.GetNode("A")
	// Exported, library pkg, 1 caller: 0.7 * 0.6 * (1.0 - 1*0.1) = 0.7 * 0.6 * 0.9 = 0.378
	score := scorer.Score(node, false, 1)
	if score < 0.3 || score > 0.5 {
		t.Fatalf("expected ~0.378 for exported library func with 1 caller, got %f", score)
	}
}

func TestConfidenceScorer_ExportedMainCmd(t *testing.T) {
	g := newTestGraph()
	addNode(g, "A", "A", "main", "a.go", 1, callgraph.NodeKindFunction, true, "func()")

	scorer := NewConfidenceScorer(g)
	node := g.GetNode("A")
	// Exported, main pkg, 0 callers: 0.9
	score := scorer.Score(node, false, 0)
	if score != 0.9 {
		t.Fatalf("expected 0.9 for exported main/cmd func with 0 callers, got %f", score)
	}
}

func TestConfidenceScorer_InterfaceMethod(t *testing.T) {
	g := newTestGraph()
	addNode(g, "impl.Do", "impl.Do", "impl", "a.go", 1, callgraph.NodeKindMethod, false, "func()")
	addNode(g, "caller.Call", "caller.Call", "caller", "b.go", 5, callgraph.NodeKindFunction, true, "func()")
	addEdge(g, "caller.Call", "impl.Do", "interface")

	scorer := NewConfidenceScorer(g)
	node := g.GetNode("impl.Do")
	// Unexported, 0 callers, interface dispatch: 0.95 * 0.5 = 0.475
	score := scorer.Score(node, false, 0)
	if score != 0.475 {
		t.Fatalf("expected 0.475 for unexported method with interface dispatch, got %f", score)
	}
}

func TestConfidenceScorer_Clamped(t *testing.T) {
	g := newTestGraph()
	addNode(g, "A", "A", "pkg", "a.go", 1, callgraph.NodeKindFunction, false, "func()")

	scorer := NewConfidenceScorer(g)
	node := g.GetNode("A")
	score := scorer.Score(node, false, 0)
	if score < 0.0 || score > 1.0 {
		t.Fatalf("score should be clamped to [0,1], got %f", score)
	}
}

func TestClassifyDeadKind(t *testing.T) {
	tests := []struct {
		kind     callgraph.NodeKind
		expected string
	}{
		{callgraph.NodeKindFunction, "function"},
		{callgraph.NodeKindMethod, "method"},
		{callgraph.NodeKindType, "type"},
		{callgraph.NodeKindPackage, "function"}, // default
	}

	for _, tt := range tests {
		node := &callgraph.Node{Kind: tt.kind}
		result := classifyDeadKind(node)
		if result != tt.expected {
			t.Errorf("classifyDeadKind(%s) = %s, want %s", tt.kind, result, tt.expected)
		}
	}
}

func TestDetect_WithNilConfig(t *testing.T) {
	g := buildDiamondGraph()
	d := NewDetector(g, testLogger())
	result, err := d.Detect(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Default config: ExcludeExported=true, ExcludeTests=true, MinConfidence=0.5
	if result.TotalEntities != 5 {
		t.Fatalf("expected 5 total entities, got %d", result.TotalEntities)
	}
}

func TestDetect_DurationMs(t *testing.T) {
	g := buildDiamondGraph()
	d := NewDetector(g, testLogger())
	result, err := d.Detect(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.DurationMs < 0 {
		t.Fatal("DurationMs should be non-negative")
	}
}

func TestErrorTypes(t *testing.T) {
	err := NewError(ErrCodeDeadCodeFailed, "something failed")
	if err.Error() != "[DEADCODE_FAILED] something failed" {
		t.Fatalf("unexpected error string: %s", err.Error())
	}

	wrapped := NewErrorWrap(ErrCodeGraphEmpty, "graph is empty", err)
	if !strings.Contains(wrapped.Error(), "GRAPH_EMPTY") {
		t.Fatalf("expected GRAPH_EMPTY in error, got: %s", wrapped.Error())
	}
	if wrapped.Unwrap() != err {
		t.Fatal("Unwrap should return inner error")
	}
}

func TestIsTestFunc(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"TestFoo", true},
		{"BenchmarkBar", true},
		{"FuzzBaz", true},
		{"ExampleQux", true},
		{"Helper", false},
		{"init", false},
		{"main", false},
	}

	for _, tt := range tests {
		result := isTestFunc(tt.name)
		if result != tt.expected {
			t.Errorf("isTestFunc(%q) = %v, want %v", tt.name, result, tt.expected)
		}
	}
}

func TestBuildReason(t *testing.T) {
	node := &callgraph.Node{Name: "foo", Exported: false}

	r := buildReason(node, false, 0)
	if !strings.Contains(r, "no callers") {
		t.Fatalf("expected 'no callers' in reason, got: %s", r)
	}

	r = buildReason(node, false, 2)
	if !strings.Contains(r, "not reachable") {
		t.Fatalf("expected 'not reachable' in reason, got: %s", r)
	}

	r = buildReason(node, true, 0)
	if !strings.Contains(r, "unused") {
		t.Fatalf("expected 'unused' in reason, got: %s", r)
	}
}

func TestBuildReason_Exported(t *testing.T) {
	node := &callgraph.Node{Name: "Foo", Exported: true}
	r := buildReason(node, true, 1)
	if !strings.Contains(r, "potentially unused") {
		t.Fatalf("expected 'potentially unused' in reason, got: %s", r)
	}
}

func TestDetect_Benchmark(t *testing.T) {
	g := newTestGraph()
	addNode(g, "main.main", "main.main", "main", "main.go", 10, callgraph.NodeKindFunction, true, "func()")
	addNode(g, "pkgA.BenchmarkPerf", "pkgA.BenchmarkPerf", "pkgA", "bench.go", 5, callgraph.NodeKindFunction, true, "func(b *testing.B)")
	addNode(g, "pkgB.ExampleFoo", "pkgB.ExampleFoo", "pkgB", "example.go", 5, callgraph.NodeKindFunction, true, "func()")
	addNode(g, "pkgC.FuzzBar", "pkgC.FuzzBar", "pkgC", "fuzz.go", 5, callgraph.NodeKindFunction, true, "func(f *testing.F)")

	d := NewDetector(g, testLogger())
	result, err := d.Detect(context.Background(), &Config{
		ExcludeExported: false,
		ExcludeTests:    true,
		MinConfidence:   0.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Benchmark/Example/Fuzz should be excluded
	for _, dc := range result.DeadCode {
		if strings.HasPrefix(dc.Name, "Benchmark") || strings.HasPrefix(dc.Name, "Example") || strings.HasPrefix(dc.Name, "Fuzz") {
			t.Fatalf("test func %s should be excluded", dc.Name)
		}
	}
}

func TestReachableSet_Large(t *testing.T) {
	g := newTestGraph()
	// Create a chain of 100 nodes
	for i := 0; i < 100; i++ {
		id := strings.Repeat("n", i+1) // unique IDs
		addNode(g, id, id, "pkg", "f.go", i, callgraph.NodeKindFunction, false, "func()")
		if i > 0 {
			prevID := strings.Repeat("n", i)
			addEdge(g, prevID, id, "direct")
		}
	}

	ra := NewReachabilityAnalyzer(g)
	firstID := "n"
	reachable := ra.ReachableSet(context.Background(), []string{firstID})

	if len(reachable) != 100 {
		t.Fatalf("expected 100 reachable nodes, got %d", len(reachable))
	}
}

func TestDetect_OnlyInit(t *testing.T) {
	g := newTestGraph()
	addNode(g, "pkgA.init", "pkgA.init", "pkgA", "a.go", 5, callgraph.NodeKindFunction, false, "func()")
	addNode(g, "pkgA.init.foo", "pkgA.init.foo", "pkgA", "a.go", 10, callgraph.NodeKindFunction, false, "func()")
	addNode(g, "pkgB.helper", "pkgB.helper", "pkgB", "b.go", 5, callgraph.NodeKindFunction, false, "func()")

	d := NewDetector(g, testLogger())
	result, err := d.Detect(context.Background(), &Config{
		ExcludeExported: false,
		ExcludeTests:    true,
		MinConfidence:   0.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	// init funcs are always alive, pkgB.helper is dead
	if result.DeadCount != 1 {
		t.Fatalf("expected 1 dead (pkgB.helper), got %d (dead: %+v)", result.DeadCount, result.DeadCode)
	}
	if result.DeadCode[0].ID != "pkgB.helper" {
		t.Fatalf("expected pkgB.helper to be dead, got %s", result.DeadCode[0].ID)
	}
}

func TestDetect_MethodKind(t *testing.T) {
	g := newTestGraph()
	addNode(g, "main.main", "main.main", "main", "main.go", 10, callgraph.NodeKindFunction, true, "func()")
	addNode(g, "receiver.DoStuff", "receiver.DoStuff", "pkg", "a.go", 5, callgraph.NodeKindMethod, true, "func()")

	d := NewDetector(g, testLogger())
	result, err := d.Detect(context.Background(), &Config{
		ExcludeExported: false,
		ExcludeTests:    true,
		MinConfidence:   0.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	// receiver.DoStuff is a method, unreachable, should be dead with DeadKind="method"
	found := false
	for _, dc := range result.DeadCode {
		if dc.ID == "receiver.DoStuff" {
			found = true
			if dc.DeadKind != "method" {
				t.Fatalf("expected dead_kind 'method', got '%s'", dc.DeadKind)
			}
			if dc.Kind != "method" {
				t.Fatalf("expected kind 'method', got '%s'", dc.Kind)
			}
		}
	}
	if !found {
		t.Fatal("receiver.DoStuff should be in dead code list")
	}
}

func TestFindEntryPoints_NoDuplicates(t *testing.T) {
	g := newTestGraph()
	// A node that is both main.main and would match other rules
	addNode(g, "main.main", "main.main", "main", "main.go", 10, callgraph.NodeKindFunction, true, "func()")

	ra := NewReachabilityAnalyzer(g)
	eps := ra.FindEntryPoints()

	count := 0
	for _, ep := range eps {
		if ep == "main.main" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected main.main to appear exactly once, appeared %d times", count)
	}
}
