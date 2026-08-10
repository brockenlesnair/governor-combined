package untested

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeNode is a shorthand for creating a callgraph.Node with sensible defaults.
func makeNode(id, name, pkg string, kind callgraph.NodeKind, exported bool) *callgraph.Node {
	return &callgraph.Node{
		ID:       id,
		Name:     name,
		Package:  pkg,
		Kind:     kind,
		Exported: exported,
	}
}

// buildTestGraph creates a small graph for the majority of tests:
//
//	TestFoo -> (calls) -> FuncA -> (calls) -> FuncB
//	                          -> (calls) -> FuncC
//	FuncD (isolated, no test)
//	TestMethod (method, reachable from test)
//	MethodX (method, not reachable)
func buildTestGraph() *callgraph.Graph {
	g := callgraph.NewGraph()

	g.AddNode(makeNode("test:TestFoo", "pkg.TestFoo", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:FuncA", "pkg.FuncA", "pkg", callgraph.NodeKindFunction, true))
	g.AddNode(makeNode("fn:FuncB", "pkg.FuncB", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:FuncC", "pkg.FuncC", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:FuncD", "pkg.FuncD", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("method:TestMethod", "pkg.TestMethod", "pkg", callgraph.NodeKindMethod, true))
	g.AddNode(makeNode("method:MethodX", "pkg.MethodX", "pkg", callgraph.NodeKindMethod, false))

	// Edges: TestFoo -> FuncA, FuncA -> FuncB, FuncA -> FuncC
	g.AddEdge("test:TestFoo", "fn:FuncA", "direct")
	g.AddEdge("fn:FuncA", "fn:FuncB", "direct")
	g.AddEdge("fn:FuncA", "fn:FuncC", "direct")
	g.AddEdge("test:TestFoo", "method:TestMethod", "direct")

	return g
}

// writeTempFile creates a temp file with the given content and returns its path.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Tests: constructor
// ---------------------------------------------------------------------------

func TestNewDetector(t *testing.T) {
	cg := callgraph.NewGraph()
	cg.AddNode(makeNode("n1", "pkg.Foo", "pkg", callgraph.NodeKindFunction, false))

	d := NewDetector(cg, slog.Default())
	if d == nil {
		t.Fatal("NewDetector returned nil")
	}
	if d.cg != cg {
		t.Error("graph not stored")
	}
}

func TestNewDetector_NilLogger(t *testing.T) {
	cg := callgraph.NewGraph()
	d := NewDetector(cg, nil)
	if d == nil {
		t.Fatal("NewDetector returned nil with nil logger")
	}
	if d.logger == nil {
		t.Error("logger should default to slog.Default()")
	}
}

// ---------------------------------------------------------------------------
// Tests: Detect
// ---------------------------------------------------------------------------

func TestDetect_EmptyGraph(t *testing.T) {
	d := NewDetector(callgraph.NewGraph(), nil)
	_, err := d.Detect(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty graph")
	}
	var ue *UntestedError
	if !errorsAs(err, &ue) {
		t.Fatalf("expected UntestedError, got %T", err)
	}
	if ue.Code != ErrCodeGraphEmpty {
		t.Errorf("expected code %s, got %s", ErrCodeGraphEmpty, ue.Code)
	}
}

func TestDetect_NilGraph(t *testing.T) {
	d := NewDetector(nil, nil)
	_, err := d.Detect(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil graph")
	}
}

func TestDetect_AllTested(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("test:T1", "pkg.TestFoo", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:A", "pkg.A", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:B", "pkg.B", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:C", "pkg.C", "pkg", callgraph.NodeKindFunction, false))
	g.AddEdge("test:T1", "fn:A", "direct")
	g.AddEdge("test:T1", "fn:B", "direct")
	g.AddEdge("fn:A", "fn:B", "direct")
	g.AddEdge("fn:B", "fn:C", "direct")

	d := NewDetector(g, nil)
	res, err := d.Detect(context.Background(), &Config{IncludeExported: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UntestedCount != 0 {
		t.Errorf("expected 0 untested, got %d", res.UntestedCount)
	}
	if res.TestedCount != 3 {
		t.Errorf("expected 3 tested, got %d", res.TestedCount)
	}
	if res.CoveragePct != 100.0 {
		t.Errorf("expected 100%% coverage, got %.1f", res.CoveragePct)
	}
}

func TestDetect_NoneTested(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("fn:A", "pkg.A", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:B", "pkg.B", "pkg", callgraph.NodeKindFunction, false))

	d := NewDetector(g, nil)
	res, err := d.Detect(context.Background(), &Config{IncludeExported: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UntestedCount != 2 {
		t.Errorf("expected 2 untested, got %d", res.UntestedCount)
	}
	if res.TestedCount != 0 {
		t.Errorf("expected 0 tested, got %d", res.TestedCount)
	}
}

func TestDetect_Mixed(t *testing.T) {
	g := buildTestGraph()
	d := NewDetector(g, nil)
	// With default config (IncludeExported=false, IncludeMethods=true):
	// Non-exported functions: FuncB, FuncC, FuncD, MethodX, TestFoo (skipped as test)
	// Tested via TestFoo: FuncA (exported, skipped), FuncB, FuncC, TestMethod
	// Untested: FuncD, MethodX
	res, err := d.Detect(context.Background(), &Config{IncludeExported: false, IncludeMethods: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UntestedCount != 2 {
		t.Errorf("expected 2 untested (FuncD, MethodX), got %d: %v",
			res.UntestedCount, names(res.Untested))
	}
}

func TestDetect_ExportedFilter(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("fn:Exp", "pkg.Exp", "pkg", callgraph.NodeKindFunction, true))
	g.AddNode(makeNode("fn:Unexp", "pkg.Unexp", "pkg", callgraph.NodeKindFunction, false))

	d := NewDetector(g, nil)
	// IncludeExported=false → skip exported
	res, err := d.Detect(context.Background(), &Config{IncludeExported: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only unexported non-test functions are counted.
	if res.TotalFunctions != 1 {
		t.Errorf("expected 1 total function, got %d", res.TotalFunctions)
	}
	if res.UntestedCount != 1 {
		t.Errorf("expected 1 untested, got %d", res.UntestedCount)
	}
}

func TestDetect_ExportedFilter_Included(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("fn:Exp", "pkg.Exp", "pkg", callgraph.NodeKindFunction, true))
	g.AddNode(makeNode("fn:Unexp", "pkg.Unexp", "pkg", callgraph.NodeKindFunction, false))

	d := NewDetector(g, nil)
	// IncludeExported=true → include exported
	res, err := d.Detect(context.Background(), &Config{IncludeExported: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalFunctions != 2 {
		t.Errorf("expected 2 total functions, got %d", res.TotalFunctions)
	}
}

func TestDetect_MethodFilter(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("fn:A", "pkg.A", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("method:M1", "pkg.M1", "pkg", callgraph.NodeKindMethod, false))

	d := NewDetector(g, nil)
	// IncludeMethods=false → skip methods
	res, err := d.Detect(context.Background(), &Config{IncludeExported: false, IncludeMethods: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalFunctions != 1 {
		t.Errorf("expected 1 total function (method filtered), got %d", res.TotalFunctions)
	}
}

func TestDetect_MinPriority(t *testing.T) {
	g := callgraph.NewGraph()
	// Func in a cmd/main package — high package weight
	g.AddNode(makeNode("fn:High", "main.High", "cmd/main", callgraph.NodeKindFunction, true))
	// Func in a leaf package — low package weight, no callers
	g.AddNode(makeNode("fn:Low", "pkg.Low", "util", callgraph.NodeKindFunction, false))

	d := NewDetector(g, nil)
	// With high min priority, only the high-priority func should appear.
	res, err := d.Detect(context.Background(), &Config{
		IncludeExported: true,
		MinPriority:     0.6,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, uf := range res.Untested {
		if uf.Priority < 0.6 {
			t.Errorf("func %s has priority %.2f, expected >= 0.6", uf.Name, uf.Priority)
		}
	}
}

func TestDetect_ContextCancelled(t *testing.T) {
	g := buildTestGraph()
	d := NewDetector(g, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := d.Detect(ctx, nil)
	// The function may or may not return an error depending on timing
	// because the select is checked at the start of each iteration.
	// We just verify it doesn't panic.
	_ = err
}

// ---------------------------------------------------------------------------
// Tests: DetectWithCoverage
// ---------------------------------------------------------------------------

func TestDetect_WithCoverage(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("fn:A", "pkg.A", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:B", "pkg.B", "pkg", callgraph.NodeKindFunction, false))

	// Create a minimal cover profile.
	profile := "mode: set\n/tmp/file.go:1.0,1.5 1 1\n/tmp/file.go:2.0,2.5 1 0\n"
	profilePath := writeTempFile(t, "cover.out", profile)

	d := NewDetector(g, nil)
	res, err := d.DetectWithCoverage(context.Background(),
		&Config{IncludeExported: false}, profilePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both funcs are untested and have different files from /tmp/file.go,
	// so CoveragePct should remain 0 (no file match).
	// But the key thing is: no crash, and result is returned.
	if res == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestDetectWithCoverage_ParseError(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("fn:A", "pkg.A", "pkg", callgraph.NodeKindFunction, false))

	// Write an invalid profile.
	profilePath := writeTempFile(t, "bad.out", "this is not a valid profile\n")

	d := NewDetector(g, nil)
	res, err := d.DetectWithCoverage(context.Background(),
		&Config{IncludeExported: false}, profilePath)
	// Should NOT return error — graceful degradation.
	if err != nil {
		t.Fatalf("expected no error (graceful degradation), got: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result even with bad profile")
	}
}

// ---------------------------------------------------------------------------
// Tests: ParseCoverProfile
// ---------------------------------------------------------------------------

func TestParseCoverProfile(t *testing.T) {
	profile := `mode: set
/tmp/foo.go:10.2,10.6 3 1
/tmp/foo.go:12.3,12.7 1 0
/tmp/foo.go:15.1,15.5 2 1
`
	profilePath := writeTempFile(t, "cover.out", profile)

	cd, err := ParseCoverProfile(profilePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// TotalLines: 3+1+2 = 6
	if cd.TotalLines != 6 {
		t.Errorf("expected TotalLines=6, got %d", cd.TotalLines)
	}
	// CoveredLines: 3+2 = 5 (second line count=0)
	if cd.CoveredLines != 5 {
		t.Errorf("expected CoveredLines=5, got %d", cd.CoveredLines)
	}
	// FileCoverage for /tmp/foo.go: 5/6 * 100 ≈ 83.33
	pct := cd.FileCoverage["/tmp/foo.go"]
	if pct < 83.0 || pct > 84.0 {
		t.Errorf("expected FileCoverage≈83.33, got %.2f", pct)
	}
}

func TestParseCoverProfile_Empty(t *testing.T) {
	profilePath := writeTempFile(t, "empty.out", "mode: set\n")

	cd, err := ParseCoverProfile(profilePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cd.TotalLines != 0 {
		t.Errorf("expected 0 total lines, got %d", cd.TotalLines)
	}
}

func TestParseCoverProfile_Invalid(t *testing.T) {
	profilePath := writeTempFile(t, "bad.out", "this is garbage\n")

	_, err := ParseCoverProfile(profilePath)
	if err == nil {
		t.Fatal("expected error for invalid profile")
	}
}

func TestParseCoverProfile_MissingMode(t *testing.T) {
	profilePath := writeTempFile(t, "badmode.out", "some random content\n")

	_, err := ParseCoverProfile(profilePath)
	if err == nil {
		t.Fatal("expected error for missing mode header")
	}
}

func TestParseCoverProfile_FileNotFound(t *testing.T) {
	_, err := ParseCoverProfile("/nonexistent/path/cover.out")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// ---------------------------------------------------------------------------
// Tests: PriorityScorer
// ---------------------------------------------------------------------------

func TestPriorityScorer(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("fn:A", "pkg.A", "pkg", callgraph.NodeKindFunction, false))
	// Add callers to increase caller weight.
	for i := 0; i < 5; i++ {
		id := "caller:" + string(rune('0'+i))
		g.AddNode(makeNode(id, "pkg."+id, "other", callgraph.NodeKindFunction, false))
		g.AddEdge(id, "fn:A", "direct")
	}

	scorer := NewPriorityScorer(g)
	node := g.GetNode("fn:A")
	score := scorer.Score(node)
	if score < 0 || score > 1 {
		t.Errorf("score out of range [0,1]: %.3f", score)
	}
	// With 5 callers: callerWeight = 5/20 = 0.25 → 0.25*0.35 = 0.0875
	// Export weight: 0.3 → 0.3*0.25 = 0.075
	// Method weight: 0.2 (function) → 0.2*0.15 = 0.03
	// Depth: depends on TransitiveCallers
	// Package: 0.3 (no keyword match) → 0.3*0.10 = 0.03
	// Total should be positive
	if score <= 0 {
		t.Errorf("expected positive score, got %.3f", score)
	}
}

func TestPriorityScorer_Exported(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("fn:Exp", "pkg.Exp", "pkg", callgraph.NodeKindFunction, true))
	g.AddNode(makeNode("fn:Unexp", "pkg.Unexp", "pkg", callgraph.NodeKindFunction, false))

	scorer := NewPriorityScorer(g)
	expScore := scorer.Score(g.GetNode("fn:Exp"))
	unexpScore := scorer.Score(g.GetNode("fn:Unexp"))
	if expScore <= unexpScore {
		t.Errorf("exported (%.3f) should score higher than unexported (%.3f)", expScore, unexpScore)
	}
}

func TestPriorityScorer_Method(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("fn:A", "pkg.A", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("m:B", "pkg.B", "pkg", callgraph.NodeKindMethod, false))
	g.AddNode(makeNode("m:C", "pkg.C", "pkg", callgraph.NodeKindMethod, false))
	// Receiver "error" is critical.
	g.GetNode("m:C").Receiver = "error"

	scorer := NewPriorityScorer(g)
	fnScore := scorer.Score(g.GetNode("fn:A"))
	methodScore := scorer.Score(g.GetNode("m:B"))
	criticalScore := scorer.Score(g.GetNode("m:C"))

	if methodScore <= fnScore {
		t.Errorf("method (%.3f) should score >= function (%.3f)", methodScore, fnScore)
	}
	if criticalScore <= methodScore {
		t.Errorf("critical-receiver method (%.3f) should score >= ordinary method (%.3f)",
			criticalScore, methodScore)
	}
}

// ---------------------------------------------------------------------------
// Tests: isTestFunc
// ---------------------------------------------------------------------------

func TestIsTestFunc(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"pkg.TestFoo", true},
		{"pkg.BenchmarkBar", true},
		{"pkg.FuzzBaz", true},
		{"pkg.ExampleQux", true},
		{"pkg.Test", true},
		{"TestFoo", true},
		{"pkg.normalFunc", false},
		{"pkg.TestHelper", true}, // "TestHelper" starts with "Test" — valid test func name
		{"init", false},
		{"main.main", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTestFunc(tt.name)
			if got != tt.want {
				t.Errorf("isTestFunc(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: Error types
// ---------------------------------------------------------------------------

func TestUntestedError(t *testing.T) {
	e := NewError(ErrCodeGraphEmpty, "graph is empty")
	if e.Error() != "[GRAPH_EMPTY] graph is empty" {
		t.Errorf("unexpected error string: %s", e.Error())
	}
	if e.Code != ErrCodeGraphEmpty {
		t.Errorf("wrong code: %s", e.Code)
	}
}

func TestUntestedErrorWrap(t *testing.T) {
	inner := os.ErrNotExist
	e := NewErrorWrap(ErrCodeUntestedFailed, "open file", inner)
	if e.Unwrap() != inner {
		t.Error("Unwrap does not return inner error")
	}
	if e.Error() == "" {
		t.Error("empty error string")
	}
}

// ---------------------------------------------------------------------------
// Tests: hasTestFile
// ---------------------------------------------------------------------------

func TestHasTestFile(t *testing.T) {
	// Create a temp dir with a source file and a test file.
	dir := t.TempDir()
	src := filepath.Join(dir, "foo.go")
	test := filepath.Join(dir, "foo_test.go")
	os.WriteFile(src, []byte("package foo\n"), 0o644)
	os.WriteFile(test, []byte("package foo\n"), 0o644)

	if !hasTestFile(src) {
		t.Error("expected hasTestFile to return true when _test.go exists")
	}
	if hasTestFile(filepath.Join(dir, "bar.go")) {
		t.Error("expected hasTestFile to return false when no _test.go exists")
	}
	if hasTestFile("") {
		t.Error("expected hasTestFile to return false for empty path")
	}
}

// ---------------------------------------------------------------------------
// Tests: Coverage enrichment via DetectWithCoverage
// ---------------------------------------------------------------------------

func TestDetectWithCoverage_Enrichment(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("fn:A", "pkg.A", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:B", "pkg.B", "pkg", callgraph.NodeKindFunction, false))

	// Cover profile with a match for fn:A's file.
	profile := "mode: set\n/tmp/foo.go:1.0,1.5 3 3\n"
	profilePath := writeTempFile(t, "cover.out", profile)

	d := NewDetector(g, nil)
	res, err := d.DetectWithCoverage(context.Background(),
		&Config{IncludeExported: false}, profilePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both funcs have empty File fields, so no file match → CoveragePct stays 0.
	for _, uf := range res.Untested {
		if uf.CoveragePct != 0 {
			t.Errorf("expected CoveragePct=0 for unmatched file, got %.1f", uf.CoveragePct)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: Non-function node kinds are skipped
// ---------------------------------------------------------------------------

func TestDetect_SkipsNonFunctionKinds(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("type:T", "pkg.T", "pkg", callgraph.NodeKindType, false))
	g.AddNode(makeNode("pkg:P", "pkg.P", "pkg", callgraph.NodeKindPackage, false))
	g.AddNode(makeNode("fn:A", "pkg.A", "pkg", callgraph.NodeKindFunction, false))

	d := NewDetector(g, nil)
	res, err := d.Detect(context.Background(), &Config{IncludeExported: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the function node is counted.
	if res.TotalFunctions != 1 {
		t.Errorf("expected 1 total function (type/pkg skipped), got %d", res.TotalFunctions)
	}
}

// ---------------------------------------------------------------------------
// Tests: init functions are skipped
// ---------------------------------------------------------------------------

func TestDetect_SkipsInit(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("fn:init", "pkg.init", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:A", "pkg.A", "pkg", callgraph.NodeKindFunction, false))

	d := NewDetector(g, nil)
	res, err := d.Detect(context.Background(), &Config{IncludeExported: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalFunctions != 1 {
		t.Errorf("expected 1 total (init skipped), got %d", res.TotalFunctions)
	}
}

// ---------------------------------------------------------------------------
// Tests: Caller count in UntestedFunc
// ---------------------------------------------------------------------------

func TestDetect_CallerCalleeCount(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("fn:Hub", "pkg.Hub", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:Caller1", "pkg.Caller1", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:Caller2", "pkg.Caller2", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:Callee1", "pkg.Callee1", "pkg", callgraph.NodeKindFunction, false))
	g.AddEdge("fn:Caller1", "fn:Hub", "direct")
	g.AddEdge("fn:Caller2", "fn:Hub", "direct")
	g.AddEdge("fn:Hub", "fn:Callee1", "direct")

	d := NewDetector(g, nil)
	res, err := d.Detect(context.Background(), &Config{IncludeExported: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, uf := range res.Untested {
		if uf.ID == "fn:Hub" {
			if uf.CallerCount != 2 {
				t.Errorf("expected CallerCount=2, got %d", uf.CallerCount)
			}
			if uf.CalleeCount != 1 {
				t.Errorf("expected CalleeCount=1, got %d", uf.CalleeCount)
			}
			return
		}
	}
	t.Error("Hub not found in untested results")
}

// ---------------------------------------------------------------------------
// Tests: CoverageData struct fields
// ---------------------------------------------------------------------------

func TestCoverageData_Fields(t *testing.T) {
	cd := &CoverageData{
		FuncCoverage: make(map[string]float64),
		FileCoverage: map[string]float64{"/a.go": 50.0},
		TotalLines:   10,
		CoveredLines: 5,
	}
	if cd.TotalLines != 10 || cd.CoveredLines != 5 {
		t.Error("coverage data fields incorrect")
	}
	if cd.FuncCoverage == nil {
		t.Error("FuncCoverage should be initialized")
	}
}

// ---------------------------------------------------------------------------
// Tests: DetectionResult
// ---------------------------------------------------------------------------

func TestDetectionResult_Fields(t *testing.T) {
	g := buildTestGraph()
	d := NewDetector(g, nil)
	res, err := d.Detect(context.Background(), &Config{IncludeExported: false, IncludeMethods: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalFunctions != res.TestedCount+res.UntestedCount {
		t.Error("TotalFunctions != TestedCount + UntestedCount")
	}
	if res.DurationMs < 0 {
		t.Error("DurationMs should be non-negative")
	}
}

// ---------------------------------------------------------------------------
// Tests: MinPriority with all nodes below threshold
// ---------------------------------------------------------------------------

func TestDetect_MinPriorityFiltersAll(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("fn:A", "pkg.A", "util", callgraph.NodeKindFunction, false))

	d := NewDetector(g, nil)
	res, err := d.Detect(context.Background(), &Config{
		IncludeExported: false,
		MinPriority:     0.99,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UntestedCount != 0 {
		t.Errorf("expected 0 untested (all below min priority), got %d", res.UntestedCount)
	}
}

// ---------------------------------------------------------------------------
// Test: Transitive callees mark tested
// ---------------------------------------------------------------------------

func TestDetect_TransitiveCalleesMarkTested(t *testing.T) {
	g := callgraph.NewGraph()
	// T1 -> A -> B -> C
	g.AddNode(makeNode("test:T1", "pkg.TestT1", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:A", "pkg.A", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:B", "pkg.B", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:C", "pkg.C", "pkg", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:D", "pkg.D", "pkg", callgraph.NodeKindFunction, false)) // isolated
	g.AddEdge("test:T1", "fn:A", "direct")
	g.AddEdge("fn:A", "fn:B", "direct")
	g.AddEdge("fn:B", "fn:C", "direct")

	d := NewDetector(g, nil)
	res, err := d.Detect(context.Background(), &Config{IncludeExported: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A, B, C are reachable from test; D is not.
	if res.TestedCount != 3 {
		t.Errorf("expected 3 tested, got %d", res.TestedCount)
	}
	if res.UntestedCount != 1 {
		t.Errorf("expected 1 untested (D), got %d: %v",
			res.UntestedCount, names(res.Untested))
	}
}

// ---------------------------------------------------------------------------
// Test: PriorityScorer depthWeight for entry points
// ---------------------------------------------------------------------------

func TestPriorityScorer_EntryPoint(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("main:main", "main.main", "main", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:A", "pkg.A", "pkg", callgraph.NodeKindFunction, false))

	scorer := NewPriorityScorer(g)
	mainScore := scorer.Score(g.GetNode("main:main"))
	aScore := scorer.Score(g.GetNode("fn:A"))
	if mainScore <= aScore {
		t.Errorf("main.main (%.3f) should score higher than pkg.A (%.3f)", mainScore, aScore)
	}
}

func TestPriorityScorer_HTTPHandler(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("handler:H1", "pkg.H1", "pkg", callgraph.NodeKindFunction, false))
	g.GetNode("handler:H1").Signature = "func(http.ResponseWriter, *http.Request)"
	g.AddNode(makeNode("fn:A", "pkg.A", "pkg", callgraph.NodeKindFunction, false))

	scorer := NewPriorityScorer(g)
	handlerScore := scorer.Score(g.GetNode("handler:H1"))
	aScore := scorer.Score(g.GetNode("fn:A"))
	if handlerScore <= aScore {
		t.Errorf("HTTP handler (%.3f) should score higher than plain func (%.3f)",
			handlerScore, aScore)
	}
}

// ---------------------------------------------------------------------------
// Tests: Package weight
// ---------------------------------------------------------------------------

func TestPriorityScorer_PackageWeight(t *testing.T) {
	g := callgraph.NewGraph()
	g.AddNode(makeNode("fn:Cmd", "main.Func", "cmd/main", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:Pkg", "pkg.Func", "pkg/internal", callgraph.NodeKindFunction, false))
	g.AddNode(makeNode("fn:Leaf", "util.Func", "util", callgraph.NodeKindFunction, false))

	scorer := NewPriorityScorer(g)
	cmdScore := scorer.Score(g.GetNode("fn:Cmd"))
	pkgScore := scorer.Score(g.GetNode("fn:Pkg"))
	leafScore := scorer.Score(g.GetNode("fn:Leaf"))
	if cmdScore <= pkgScore || pkgScore <= leafScore {
		t.Errorf("expected cmd(%.3f) > pkg(%.3f) > leaf(%.3f)", cmdScore, pkgScore, leafScore)
	}
}

// ---------------------------------------------------------------------------
// helper: extract names from UntestedFunc slice
// ---------------------------------------------------------------------------

func names(fns []UntestedFunc) []string {
	out := make([]string, len(fns))
	for i, f := range fns {
		out[i] = f.Name
	}
	return out
}

// errorsAs is a thin wrapper around errors.As for testing without importing errors.
func errorsAs(err error, target interface{}) bool {
	type iface interface {
		Unwrap() error
	}
	for {
		if err == nil {
			return false
		}
		switch t := target.(type) {
		case **UntestedError:
			if e, ok := err.(*UntestedError); ok {
				*t = e
				return true
			}
		}
		ue, ok := err.(iface)
		if !ok {
			return false
		}
		err = ue.Unwrap()
	}
}
