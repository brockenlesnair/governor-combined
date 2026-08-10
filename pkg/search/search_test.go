package search

import (
	"context"
	"log/slog"
	"testing"

	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
)

func newTestGraph() *callgraph.Graph {
	g := callgraph.NewGraph()

	g.AddNode(&callgraph.Node{
		ID: "pkg.FuncA", Name: "pkg.FuncA", Package: "pkg",
		File: "a.go", Line: 10, Kind: callgraph.NodeKindFunction, Exported: true,
		Signature: "func(ctx context.Context, name string) error",
	})
	g.AddNode(&callgraph.Node{
		ID: "pkg.FuncB", Name: "pkg.FuncB", Package: "pkg",
		File: "b.go", Line: 20, Kind: callgraph.NodeKindFunction, Exported: true,
		Signature: "func(x int) string",
	})
	g.AddNode(&callgraph.Node{
		ID: "pkg.FuncC", Name: "pkg.FuncC", Package: "pkg",
		File: "c.go", Line: 30, Kind: callgraph.NodeKindFunction, Exported: false,
		Signature: "func()",
	})
	g.AddNode(&callgraph.Node{
		ID: "pkg.MethodD", Name: "pkg.MethodD", Package: "pkg",
		File: "d.go", Line: 40, Kind: callgraph.NodeKindMethod, Receiver: "MyStruct",
		Signature: "func(ctx context.Context) error",
	})
	g.AddNode(&callgraph.Node{
		ID: "pkg.MyStruct", Name: "pkg.MyStruct", Package: "pkg",
		File: "e.go", Line: 50, Kind: callgraph.NodeKindType,
	})
	g.AddNode(&callgraph.Node{
		ID: "pkg.InterfaceE", Name: "pkg.InterfaceE", Package: "pkg",
		File: "f.go", Line: 60, Kind: callgraph.NodeKindType,
	})

	g.AddEdge("pkg.FuncA", "pkg.FuncB", "direct")
	g.AddEdge("pkg.FuncA", "pkg.FuncC", "direct")
	g.AddEdge("pkg.FuncB", "pkg.FuncC", "direct")
	g.AddEdge("pkg.MethodD", "pkg.FuncA", "direct")

	return g
}

func TestNewFuzzyMatcher(t *testing.T) {
	names := []string{"FuncA", "FuncB", "FuncC", "MethodD"}
	fm := NewFuzzyMatcher(names)
	if fm == nil {
		t.Fatal("NewFuzzyMatcher returned nil")
	}
	if len(fm.trigrams) == 0 {
		t.Error("trigram index is empty")
	}
}

func TestFuzzyMatcherMatch(t *testing.T) {
	names := []string{"FuncA", "FuncB", "FuncC", "MethodD"}
	fm := NewFuzzyMatcher(names)

	tests := []struct {
		query     string
		threshold float64
		minCount  int
	}{
		{"FuncA", 0.3, 1},
		{"Func", 0.3, 3},
		{"MethodD", 0.3, 1},
		{"xyz", 0.3, 0},
	}

	for _, tt := range tests {
		matches := fm.Match(tt.query, tt.threshold)
		if len(matches) < tt.minCount {
			t.Errorf("Match(%q, %f) got %d matches, want >= %d", tt.query, tt.threshold, len(matches), tt.minCount)
		}
	}
}

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
	}
	for _, tt := range tests {
		got := levenshteinDistance(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("levenshteinDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestExtractTrigrams(t *testing.T) {
	tests := []struct {
		input string
		min   int
	}{
		{"abc", 1},
		{"hello", 3},
		{"ab", 1},
		{"", 0},
	}
	for _, tt := range tests {
		tris := extractTrigrams(tt.input)
		if len(tris) < tt.min {
			t.Errorf("extractTrigrams(%q) got %d trigrams, want >= %d", tt.input, len(tris), tt.min)
		}
	}
}

func TestMatchKind(t *testing.T) {
	tests := []struct {
		name, query, want string
	}{
		{"FuncA", "FuncA", "exact"},
		{"FuncAB", "Func", "prefix"},
		{"MyFuncA", "FuncA", "substring"},
		{"FuncA", "FuncB", "fuzzy"},
	}
	for _, tt := range tests {
		got := matchKind(tt.name, tt.query)
		if got != tt.want {
			t.Errorf("matchKind(%q, %q) = %q, want %q", tt.name, tt.query, got, tt.want)
		}
	}
}

func TestExtractParamTypes(t *testing.T) {
	tests := []struct {
		sig  string
		want int
	}{
		{"func(ctx context.Context, name string) error", 2},
		{"func(x int) string", 1},
		{"func()", 0},
		{"", 0},
	}
	for _, tt := range tests {
		params := extractParamTypes(tt.sig)
		if len(params) != tt.want {
			t.Errorf("extractParamTypes(%q) got %d params, want %d", tt.sig, len(params), tt.want)
		}
	}
}

func TestExtractReturnTypes(t *testing.T) {
	tests := []struct {
		sig  string
		want int
	}{
		{"func() error", 1},
		{"func() (string, error)", 2},
		{"func()", 0},
	}
	for _, tt := range tests {
		returns := extractReturnTypes(tt.sig)
		if len(returns) != tt.want {
			t.Errorf("extractReturnTypes(%q) got %d returns, want %d", tt.sig, len(returns), tt.want)
		}
	}
}

func TestMatchType(t *testing.T) {
	node := &callgraph.Node{
		ID: "test", Name: "test", Kind: callgraph.NodeKindMethod,
		Signature: "func(ctx context.Context, name string) error",
	}

	tests := []struct {
		filter *TypeFilter
		want   bool
	}{
		{nil, true},
		{&TypeFilter{Params: []string{"context.Context"}}, true},
		{&TypeFilter{Return: "error"}, true},
		{&TypeFilter{Return: "string"}, false},
		{&TypeFilter{ExactMatch: true, Return: "error"}, true},
	}
	for _, tt := range tests {
		got := MatchType(node, tt.filter)
		if got != tt.want {
			t.Errorf("MatchType(filter=%+v) = %v, want %v", tt.filter, got, tt.want)
		}
	}
}

func TestNewGraphSearch(t *testing.T) {
	g := newTestGraph()
	gs := NewGraphSearch(g)
	if gs == nil {
		t.Fatal("NewGraphSearch returned nil")
	}
}

func TestGraphSearchCallerCount(t *testing.T) {
	g := newTestGraph()
	gs := NewGraphSearch(g)

	count := gs.CallerCount("pkg.FuncC")
	if count != 2 {
		t.Errorf("CallerCount(FuncC) = %d, want 2", count)
	}

	count = gs.CallerCount("pkg.FuncA")
	if count != 1 {
		t.Errorf("CallerCount(FuncA) = %d, want 1", count)
	}
}

func TestGraphSearchCalleeCount(t *testing.T) {
	g := newTestGraph()
	gs := NewGraphSearch(g)

	count := gs.CalleeCount("pkg.FuncA")
	if count != 2 {
		t.Errorf("CalleeCount(FuncA) = %d, want 2", count)
	}

	count = gs.CalleeCount("pkg.FuncC")
	if count != 0 {
		t.Errorf("CalleeCount(FuncC) = %d, want 0", count)
	}
}

func TestGraphSearchCallersOf(t *testing.T) {
	g := newTestGraph()
	gs := NewGraphSearch(g)
	ctx := context.Background()

	edges := gs.CallersOf(ctx, "pkg.FuncC", 1)
	if len(edges) < 2 {
		t.Errorf("CallersOf(FuncC, 1) got %d edges, want >= 2", len(edges))
	}
}

func TestGraphSearchCalleesOf(t *testing.T) {
	g := newTestGraph()
	gs := NewGraphSearch(g)
	ctx := context.Background()

	edges := gs.CalleesOf(ctx, "pkg.FuncA", 1)
	if len(edges) < 2 {
		t.Errorf("CalleesOf(FuncA, 1) got %d edges, want >= 2", len(edges))
	}
}

func TestGraphSearchFindBySignature(t *testing.T) {
	g := newTestGraph()
	gs := NewGraphSearch(g)
	ctx := context.Background()

	filter := &TypeFilter{Return: "error"}
	refs := gs.FindBySignature(ctx, filter)
	if len(refs) < 2 {
		t.Errorf("FindBySignature(return=error) got %d results, want >= 2", len(refs))
	}
}

func TestNewRanker(t *testing.T) {
	g := newTestGraph()
	gs := NewGraphSearch(g)
	r := NewRanker(gs)
	if r == nil {
		t.Fatal("NewRanker returned nil")
	}
}

func TestRankerScoreResult(t *testing.T) {
	g := newTestGraph()
	gs := NewGraphSearch(g)
	r := NewRanker(gs)

	ref := g.Nodes["pkg.FuncA"]
	match := FuzzyMatch{Name: "FuncA", Score: 0.9, Kind: "exact"}
	score := r.ScoreResult(match, ref)
	if score < 0 || score > 1.0 {
		t.Errorf("ScoreResult = %f, want [0, 1]", score)
	}
}

func TestRankerRankAndSort(t *testing.T) {
	g := newTestGraph()
	gs := NewGraphSearch(g)
	r := NewRanker(gs)

	matches := []FuzzyMatch{
		{Name: "FuncA", Score: 0.9, Kind: "exact"},
		{Name: "FuncB", Score: 0.7, Kind: "fuzzy"},
	}

	results := r.RankAndSort(matches, g.Nodes)
	if len(results) != 2 {
		t.Fatalf("RankAndSort got %d results, want 2", len(results))
	}

	if results[0].Score < results[1].Score {
		t.Error("results not sorted descending by score")
	}
}

func TestClampScore(t *testing.T) {
	tests := []struct {
		input, want float64
	}{
		{-0.5, 0},
		{0.5, 0.5},
		{1.5, 1.0},
	}
	for _, tt := range tests {
		got := clampScore(tt.input)
		if got != tt.want {
			t.Errorf("clampScore(%f) = %f, want %f", tt.input, got, tt.want)
		}
	}
}

func TestNewSearcher(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	if s == nil {
		t.Fatal("NewSearcher returned nil")
	}
	if len(s.cg.Nodes) != 6 {
		t.Errorf("Searcher has %d nodes, want 6", len(s.cg.Nodes))
	}
}

func TestSearcherSearchFuzzy(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	query := &Query{Pattern: "FuncA", MaxResults: 10}
	result, err := s.Search(ctx, query)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if result.TotalFound == 0 {
		t.Error("Search found no results")
	}
	if result.DurationMs < 0 {
		t.Error("DurationMs is negative")
	}
}

func TestSearcherSearchRegex(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	query := &Query{Regex: "Func.", MaxResults: 10}
	result, err := s.Search(ctx, query)
	if err != nil {
		t.Fatalf("Search(regex) failed: %v", err)
	}
	if result.TotalFound < 3 {
		t.Errorf("Search(regex) got %d results, want >= 3", result.TotalFound)
	}
}

func TestSearcherSearchInvalidRegex(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	query := &Query{Regex: "[invalid", MaxResults: 10}
	_, err := s.Search(ctx, query)
	if err == nil {
		t.Error("Search(invalid regex) should return error")
	}
}

func TestSearcherSearchNilQuery(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	_, err := s.Search(ctx, nil)
	if err == nil {
		t.Error("Search(nil) should return error")
	}
}

func TestSearcherSearchEmptyPattern(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	query := &Query{MaxResults: 10}
	_, err := s.Search(ctx, query)
	if err == nil {
		t.Error("Search(empty pattern) should return error")
	}
}

func TestSearcherSearchCallers(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	query := &Query{CallerOf: "pkg.FuncC", MaxResults: 10}
	result, err := s.Search(ctx, query)
	if err != nil {
		t.Fatalf("Search(caller_of) failed: %v", err)
	}
	if result.TotalFound < 2 {
		t.Errorf("Search(caller_of) got %d results, want >= 2", result.TotalFound)
	}
}

func TestSearcherSearchCallees(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	query := &Query{CalleeOf: "pkg.FuncA", MaxResults: 10}
	result, err := s.Search(ctx, query)
	if err != nil {
		t.Fatalf("Search(callee_of) failed: %v", err)
	}
	if result.TotalFound < 2 {
		t.Errorf("Search(callee_of) got %d results, want >= 2", result.TotalFound)
	}
}

func TestSearcherSearchByType(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	query := &Query{ReturnType: "error", MaxResults: 10}
	result, err := s.Search(ctx, query)
	if err != nil {
		t.Fatalf("Search(by_type) failed: %v", err)
	}
	if result.TotalFound < 1 {
		t.Error("Search(by_type) got 0 results")
	}
}

func TestSearcherFindCallers(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	results, err := s.FindCallers(ctx, "pkg.FuncC", 1)
	if err != nil {
		t.Fatalf("FindCallers failed: %v", err)
	}
	if len(results) < 2 {
		t.Errorf("FindCallers got %d results, want >= 2", len(results))
	}
}

func TestSearcherFindCallees(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	results, err := s.FindCallees(ctx, "pkg.FuncA", 1)
	if err != nil {
		t.Fatalf("FindCallees failed: %v", err)
	}
	if len(results) < 2 {
		t.Errorf("FindCallees got %d results, want >= 2", len(results))
	}
}

func TestSearcherFindByType(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	results, err := s.FindByType(ctx, []string{"context.Context"}, "error")
	if err != nil {
		t.Fatalf("FindByType failed: %v", err)
	}
	if len(results) < 1 {
		t.Error("FindByType got 0 results")
	}
}

func TestSearcherSearchExact(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	result, err := s.SearchExact(ctx, "pkg.FuncA")
	if err != nil {
		t.Fatalf("SearchExact failed: %v", err)
	}
	if result.Entity.ID != "pkg.FuncA" {
		t.Errorf("SearchExact got %q, want %q", result.Entity.ID, "pkg.FuncA")
	}
}

func TestSearcherSearchExactNotFound(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	_, err := s.SearchExact(ctx, "pkg.NonExistent")
	if err == nil {
		t.Error("SearchExact(nonexistent) should return error")
	}
}

func TestSearcherSearchPrefix(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	results := s.SearchPrefix(ctx, "Func", 10)
	if len(results) < 3 {
		t.Errorf("SearchPrefix got %d results, want >= 3", len(results))
	}
}

func TestSearcherSearchSubstring(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	results := s.SearchSubstring(ctx, "unc", 10)
	if len(results) < 3 {
		t.Errorf("SearchSubstring got %d results, want >= 3", len(results))
	}
}

func TestSearcherSearchByKind(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	results := s.SearchByKind(ctx, callgraph.NodeKindFunction, 10)
	if len(results) != 3 {
		t.Errorf("SearchByKind(function) got %d results, want 3", len(results))
	}
}

func TestSearcherStats(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)

	stats := s.Stats()
	if stats.TotalNodes != 6 {
		t.Errorf("Stats.TotalNodes = %d, want 6", stats.TotalNodes)
	}
	if stats.TotalEdges != 4 {
		t.Errorf("Stats.TotalEdges = %d, want 4", stats.TotalEdges)
	}
}

func TestSearcherString(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)

	str := s.String()
	if str == "" {
		t.Error("String() returned empty")
	}
}

func TestSearcherNodes(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)

	nodes := s.Nodes()
	if len(nodes) != 6 {
		t.Errorf("Nodes() got %d nodes, want 6", len(nodes))
	}
}

func TestSearcherGraph(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)

	graph := s.Graph()
	if graph == nil {
		t.Error("Graph() returned nil")
	}
}

func TestSearcherBuildSearchIndex(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)

	s.BuildSearchIndex()
	if s.matcher == nil {
		t.Error("BuildSearchIndex did not set matcher")
	}
}

func TestSearcherSearchWithKindFilter(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	query := &Query{Pattern: "Func", Kind: SearchFunction, MaxResults: 10}
	result, err := s.Search(ctx, query)
	if err != nil {
		t.Fatalf("Search(kind filter) failed: %v", err)
	}
	for _, r := range result.Matches {
		if r.Entity.Kind != callgraph.NodeKindFunction {
			t.Errorf("kind filter passed non-function: %s", r.Entity.Kind)
		}
	}
}

func TestSearcherSearchMaxResults(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	query := &Query{Pattern: "Func", MaxResults: 1}
	result, err := s.Search(ctx, query)
	if err != nil {
		t.Fatalf("Search(max_results) failed: %v", err)
	}
	if len(result.Matches) > 1 {
		t.Errorf("Search(max_results=1) got %d matches", len(result.Matches))
	}
}

func TestTransitiveCallers(t *testing.T) {
	g := newTestGraph()

	edges := g.TransitiveCallers("pkg.FuncC", 1)
	if len(edges) < 2 {
		t.Errorf("TransitiveCallers(FuncC, 1) got %d edges, want >= 2", len(edges))
	}
}

func TestTransitiveCallees(t *testing.T) {
	g := newTestGraph()

	edges := g.TransitiveCallees("pkg.FuncA", 1)
	if len(edges) < 2 {
		t.Errorf("TransitiveCallees(FuncA, 1) got %d edges, want >= 2", len(edges))
	}
}

func TestNodeShort(t *testing.T) {
	tests := []struct {
		name, want string
	}{
		{"pkg.FuncA", "FuncA"},
		{"FuncA", "FuncA"},
		{"a.b.c.Func", "Func"},
	}
	for _, tt := range tests {
		n := &callgraph.Node{Name: tt.name}
		got := n.Short()
		if got != tt.want {
			t.Errorf("Node{Name=%q}.Short() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestSearcherSearchWithContext(t *testing.T) {
	g := newTestGraph()
	logger := slog.Default()
	s := NewSearcher(g, logger)
	ctx := context.Background()

	query := &Query{Pattern: "FuncA", MaxResults: 10}
	result, err := s.SearchWithContext(ctx, query)
	if err != nil {
		t.Fatalf("SearchWithContext failed: %v", err)
	}
	if result.TotalFound == 0 {
		t.Error("SearchWithContext found no results")
	}
}

func TestTrigramSimilarity(t *testing.T) {
	tests := []struct {
		query, candidate string
		overlap          int
		minScore         float64
	}{
		{"FuncA", "FuncA", 4, 0.5},
		{"Func", "FuncA", 3, 0.3},
		{"abc", "xyz", 0, 0},
	}
	for _, tt := range tests {
		score := trigramSimilarity(tt.query, tt.candidate, tt.overlap)
		if score < tt.minScore {
			t.Errorf("trigramSimilarity(%q, %q, %d) = %f, want >= %f",
				tt.query, tt.candidate, tt.overlap, score, tt.minScore)
		}
	}
}
