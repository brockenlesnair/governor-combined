package search

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"time"

	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
)

type Searcher struct {
	cg          *callgraph.Graph
	graphSearch *GraphSearch
	ranker      *Ranker
	matcher     *FuzzyMatcher
	logger      *slog.Logger
}

func NewSearcher(cg *callgraph.Graph, logger *slog.Logger) *Searcher {
	names := make([]string, 0, len(cg.Nodes)*2)
	for _, ref := range cg.Nodes {
		names = append(names, ref.Name)
		names = append(names, ref.Short())
	}

	gs := NewGraphSearch(cg)
	return &Searcher{
		cg:          cg,
		graphSearch: gs,
		ranker:      NewRanker(gs),
		matcher:     NewFuzzyMatcher(names),
		logger:      logger.With("component", "search"),
	}
}

type Query struct {
	Pattern    string     `json:"pattern"`
	Kind       SearchKind `json:"kind"`
	ParamTypes []string   `json:"param_types,omitempty"`
	ReturnType string     `json:"return_type,omitempty"`
	CallerOf   string     `json:"caller_of,omitempty"`
	CalleeOf   string     `json:"callee_of,omitempty"`
	Regex      string     `json:"regex,omitempty"`
	MaxResults int        `json:"max_results,omitempty"`
}

type SearchKind int

const (
	SearchFunction  SearchKind = iota
	SearchMethod
	SearchStruct
	SearchInterface
	SearchAll
)

type Result struct {
	Entity    callgraph.Node `json:"entity"`
	Score     float64        `json:"score"`
	Snippet   string         `json:"snippet"`
	File      string         `json:"file"`
	Line      int            `json:"line"`
	Callers   int            `json:"callers"`
	Callees   int            `json:"callees"`
	MatchType string         `json:"match_type"`
}

type SearchResult struct {
	Query      Query    `json:"query"`
	Matches    []Result `json:"matches"`
	TotalFound int      `json:"total_found"`
	DurationMs int64    `json:"duration_ms"`
}

func (s *Searcher) Search(ctx context.Context, query *Query) (*SearchResult, error) {
	start := time.Now()

	if query == nil {
		return nil, NewSearchError(ErrCodeInvalidInput, "query is required")
	}

	if query.MaxResults <= 0 {
		query.MaxResults = 50
	}

	if query.CallerOf != "" {
		return s.searchCallers(ctx, query)
	}
	if query.CalleeOf != "" {
		return s.searchCallees(ctx, query)
	}

	if len(query.ParamTypes) > 0 || query.ReturnType != "" {
		return s.searchByType(ctx, query)
	}

	var results []Result
	var err error
	if query.Regex != "" {
		results, err = s.searchRegex(ctx, query)
		if err != nil {
			return nil, err
		}
	} else if query.Pattern != "" {
		results = s.searchFuzzy(ctx, query)
	} else {
		return nil, NewSearchError(ErrCodeInvalidInput, "pattern, regex, or type filter required")
	}

	elapsed := time.Since(start)

	return &SearchResult{
		Query:      *query,
		Matches:    results,
		TotalFound: len(results),
		DurationMs: elapsed.Milliseconds(),
	}, nil
}

func (s *Searcher) searchFuzzy(ctx context.Context, query *Query) []Result {
	matches := s.matcher.Match(query.Pattern, 0.3)
	if len(matches) > query.MaxResults {
		matches = matches[:query.MaxResults]
	}

	results := s.ranker.RankAndSort(matches, s.cg.Nodes)

	if query.Kind != SearchAll {
		results = filterByKind(results, query.Kind)
	}

	if len(results) > query.MaxResults {
		results = results[:query.MaxResults]
	}

	return results
}

func (s *Searcher) searchRegex(ctx context.Context, query *Query) ([]Result, error) {
	re, err := regexp.Compile(query.Regex)
	if err != nil {
		return nil, NewSearchErrorf(ErrCodeInvalidRegex, "invalid regex: %v", err)
	}

	var matches []FuzzyMatch
	for _, ref := range s.cg.Nodes {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if re.MatchString(ref.Name) || re.MatchString(ref.Short()) {
			matches = append(matches, FuzzyMatch{
				Name:  ref.Name,
				Score: 1.0,
				Kind:  "regex",
			})
		}
	}

	results := s.ranker.RankAndSort(matches, s.cg.Nodes)
	if len(results) > query.MaxResults {
		results = results[:query.MaxResults]
	}

	return results, nil
}

func (s *Searcher) searchCallers(ctx context.Context, query *Query) (*SearchResult, error) {
	start := time.Now()

	edges := s.graphSearch.CallersOf(ctx, query.CallerOf, 0)
	seen := make(map[string]bool)
	var results []Result

	for _, edge := range edges {
		id := edge.From
		if seen[id] {
			continue
		}
		seen[id] = true

		ref := s.cg.Nodes[id]
		if ref == nil {
			continue
		}
		results = append(results, Result{
			Entity:    *ref,
			Score:     1.0,
			MatchType: "caller",
		})
	}

	if len(results) > query.MaxResults {
		results = results[:query.MaxResults]
	}

	return &SearchResult{
		Query:      *query,
		Matches:    results,
		TotalFound: len(results),
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

func (s *Searcher) searchCallees(ctx context.Context, query *Query) (*SearchResult, error) {
	start := time.Now()

	edges := s.graphSearch.CalleesOf(ctx, query.CalleeOf, 0)
	seen := make(map[string]bool)
	var results []Result

	for _, edge := range edges {
		id := edge.To
		if seen[id] {
			continue
		}
		seen[id] = true

		ref := s.cg.Nodes[id]
		if ref == nil {
			continue
		}
		results = append(results, Result{
			Entity:    *ref,
			Score:     1.0,
			MatchType: "callee",
		})
	}

	if len(results) > query.MaxResults {
		results = results[:query.MaxResults]
	}

	return &SearchResult{
		Query:      *query,
		Matches:    results,
		TotalFound: len(results),
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

func (s *Searcher) searchByType(ctx context.Context, query *Query) (*SearchResult, error) {
	start := time.Now()

	filter := &TypeFilter{
		Params: query.ParamTypes,
		Return: query.ReturnType,
	}

	refs := s.graphSearch.FindBySignature(ctx, filter)
	var results []Result
	for _, ref := range refs {
		results = append(results, Result{
			Entity:    *ref,
			Score:     0.8,
			MatchType: "type",
		})
	}

	if len(results) > query.MaxResults {
		results = results[:query.MaxResults]
	}

	return &SearchResult{
		Query:      *query,
		Matches:    results,
		TotalFound: len(results),
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

func filterByKind(results []Result, kind SearchKind) []Result {
	var filtered []Result
	for _, r := range results {
		match := false
		switch kind {
		case SearchFunction:
			match = r.Entity.Kind == callgraph.NodeKindFunction
		case SearchMethod:
			match = r.Entity.Kind == callgraph.NodeKindMethod
		case SearchStruct:
			match = r.Entity.Kind == callgraph.NodeKindType
		case SearchInterface:
			match = r.Entity.Kind == callgraph.NodeKindType
		case SearchAll:
			match = true
		}
		if match {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func (s *Searcher) FindCallers(ctx context.Context, targetID string, depth int) ([]Result, error) {
	if s.cg == nil {
		return nil, NewSearchError(ErrCodeGraphNotLoaded, "call graph not loaded")
	}

	edges := s.graphSearch.CallersOf(ctx, targetID, depth)
	seen := make(map[string]bool)
	var results []Result

	for _, edge := range edges {
		id := edge.From
		if seen[id] {
			continue
		}
		seen[id] = true

		ref := s.cg.Nodes[id]
		if ref == nil {
			continue
		}
		results = append(results, Result{
			Entity:    *ref,
			Score:     1.0,
			MatchType: "caller",
			Callers:   s.graphSearch.CallerCount(ref.ID),
			Callees:   s.graphSearch.CalleeCount(ref.ID),
		})
	}

	return results, nil
}

func (s *Searcher) FindCallees(ctx context.Context, targetID string, depth int) ([]Result, error) {
	if s.cg == nil {
		return nil, NewSearchError(ErrCodeGraphNotLoaded, "call graph not loaded")
	}

	edges := s.graphSearch.CalleesOf(ctx, targetID, depth)
	seen := make(map[string]bool)
	var results []Result

	for _, edge := range edges {
		id := edge.To
		if seen[id] {
			continue
		}
		seen[id] = true

		ref := s.cg.Nodes[id]
		if ref == nil {
			continue
		}
		results = append(results, Result{
			Entity:    *ref,
			Score:     1.0,
			MatchType: "callee",
			Callers:   s.graphSearch.CallerCount(ref.ID),
			Callees:   s.graphSearch.CalleeCount(ref.ID),
		})
	}

	return results, nil
}

func (s *Searcher) FindByType(ctx context.Context, params []string, returns string) ([]Result, error) {
	if s.cg == nil {
		return nil, NewSearchError(ErrCodeGraphNotLoaded, "call graph not loaded")
	}

	filter := &TypeFilter{
		Params: params,
		Return: returns,
	}

	refs := s.graphSearch.FindBySignature(ctx, filter)
	var results []Result
	for _, ref := range refs {
		results = append(results, Result{
			Entity:    *ref,
			Score:     0.8,
			MatchType: "type",
			Callers:   s.graphSearch.CallerCount(ref.ID),
			Callees:   s.graphSearch.CalleeCount(ref.ID),
		})
	}

	return results, nil
}

func (s *Searcher) BuildSearchIndex() {
	names := make([]string, 0, len(s.cg.Nodes)*2)
	for _, ref := range s.cg.Nodes {
		names = append(names, ref.Name)
		names = append(names, ref.Short())
	}
	s.matcher = NewFuzzyMatcher(names)
}

func (s *Searcher) SearchExact(ctx context.Context, name string) (*Result, error) {
	for _, ref := range s.cg.Nodes {
		if ref.Name == name || ref.Short() == name {
			score := s.ranker.ScoreResult(FuzzyMatch{Name: name, Score: 1.0, Kind: "exact"}, ref)
			return &Result{
				Entity:    *ref,
				Score:     score,
				MatchType: "exact",
				Callers:   s.graphSearch.CallerCount(ref.ID),
				Callees:   s.graphSearch.CalleeCount(ref.ID),
			}, nil
		}
	}
	return nil, NewSearchErrorf(ErrCodeSearchFailed, "no match found for %q", name)
}

func (s *Searcher) SearchPrefix(ctx context.Context, prefix string, maxResults int) []Result {
	if maxResults <= 0 {
		maxResults = 50
	}

	var matches []FuzzyMatch
	for _, ref := range s.cg.Nodes {
		if matchKind(ref.Name, prefix) == "prefix" || matchKind(ref.Short(), prefix) == "prefix" {
			matches = append(matches, FuzzyMatch{
				Name:  ref.Name,
				Score: 1.0,
				Kind:  "prefix",
			})
		}
	}

	results := s.ranker.RankAndSort(matches, s.cg.Nodes)
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

func (s *Searcher) SearchSubstring(ctx context.Context, substr string, maxResults int) []Result {
	if maxResults <= 0 {
		maxResults = 50
	}

	var matches []FuzzyMatch
	for _, ref := range s.cg.Nodes {
		lowerName := ref.Name
		lowerSubstr := substr
		if containsIgnoreCase(lowerName, lowerSubstr) {
			matches = append(matches, FuzzyMatch{
				Name:  ref.Name,
				Score: 0.8,
				Kind:  "substring",
			})
		}
	}

	results := s.ranker.RankAndSort(matches, s.cg.Nodes)
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

func containsIgnoreCase(s, substr string) bool {
	return len(substr) <= len(s) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (s *Searcher) Nodes() map[string]*callgraph.Node {
	return s.cg.Nodes
}

func (s *Searcher) Graph() *callgraph.Graph {
	return s.cg
}

func (s *Searcher) SearchByKind(ctx context.Context, kind callgraph.NodeKind, maxResults int) []Result {
	if maxResults <= 0 {
		maxResults = 50
	}

	var results []Result
	for _, ref := range s.cg.Nodes {
		select {
		case <-ctx.Done():
			return results
		default:
		}

		if ref.Kind == kind {
			results = append(results, Result{
				Entity:    *ref,
				Score:     0.8,
				MatchType: "kind",
				Callers:   s.graphSearch.CallerCount(ref.ID),
				Callees:   s.graphSearch.CalleeCount(ref.ID),
			})
		}
	}

	sortResultsByScore(results)
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

func sortResultsByScore(results []Result) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
}

func (s *Searcher) Stats() SearchResultStats {
	return SearchResultStats{
		TotalNodes: len(s.cg.Nodes),
		TotalEdges: len(s.cg.Edges),
	}
}

type SearchResultStats struct {
	TotalNodes int `json:"total_nodes"`
	TotalEdges int `json:"total_edges"`
}

func (s *Searcher) SearchWithContext(ctx context.Context, query *Query) (*SearchResult, error) {
	return s.Search(ctx, query)
}

func (s *Searcher) String() string {
	return fmt.Sprintf("Searcher{nodes=%d, edges=%d}", len(s.cg.Nodes), len(s.cg.Edges))
}
