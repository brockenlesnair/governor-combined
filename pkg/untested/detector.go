package untested

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
)

// UntestedDetector finds functions with zero test coverage.
type UntestedDetector struct {
	cg     *callgraph.Graph
	logger *slog.Logger
	scorer *PriorityScorer
}

// NewDetector creates an UntestedDetector backed by the given call graph.
func NewDetector(cg *callgraph.Graph, logger *slog.Logger) *UntestedDetector {
	if logger == nil {
		logger = slog.Default()
	}
	return &UntestedDetector{
		cg:     cg,
		logger: logger,
		scorer: NewPriorityScorer(cg),
	}
}

// Detect analyses the call graph and returns functions that are not
// reachable from any test, benchmark, fuzz, or example function.
func (d *UntestedDetector) Detect(ctx context.Context, cfg *Config) (*DetectionResult, error) {
	start := time.Now()

	if cfg == nil {
		cfg = &Config{}
	}

	if d.cg == nil || len(d.cg.Nodes) == 0 {
		return nil, NewError(ErrCodeGraphEmpty, "call graph is nil or empty")
	}

	// 1. Build the set of node IDs reachable from test functions.
	tested := d.buildTestedSet(cfg)

	// 2. Iterate all nodes; collect untested ones.
	var untested []UntestedFunc
	totalFuncs := 0

	for _, node := range d.cg.Nodes {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Skip test functions themselves.
		if isTestFunc(node.Name) {
			continue
		}
		// Skip init functions.
		if node.Name == "init" || strings.HasSuffix(node.Name, ".init") {
			continue
		}

		// Apply filters.
		if !cfg.IncludeExported && node.Exported {
			continue
		}
		if !cfg.IncludeMethods && node.Kind == callgraph.NodeKindMethod {
			continue
		}

		// Only consider function/method nodes.
		if node.Kind != callgraph.NodeKindFunction && node.Kind != callgraph.NodeKindMethod {
			continue
		}

		totalFuncs++

		if tested[node.ID] {
			continue
		}

		uf := UntestedFunc{
			ID:          node.ID,
			Name:        node.Name,
			Package:     node.Package,
			File:        node.File,
			Line:        node.Line,
			Kind:        string(node.Kind),
			Exported:    node.Exported,
			Priority:    d.scorer.Score(node),
			CallerCount: len(d.cg.Callers(node.ID)),
			CalleeCount: len(d.cg.Callees(node.ID)),
			Reason:      "not reachable from any test function",
			HasTestFile: hasTestFile(node.File),
		}
		untested = append(untested, uf)
	}

	// 3. Apply MinPriority filter.
	if cfg.MinPriority > 0 {
		filtered := make([]UntestedFunc, 0, len(untested))
		for _, uf := range untested {
			if uf.Priority >= cfg.MinPriority {
				filtered = append(filtered, uf)
			}
		}
		untested = filtered
	}

	// 4. Sort by priority descending (stable sort for determinism).
	sortUntested(untested)

	elapsed := time.Since(start).Milliseconds()

	testedCount := totalFuncs - len(untested)
	coveragePct := 0.0
	if totalFuncs > 0 {
		coveragePct = float64(testedCount) / float64(totalFuncs) * 100.0
	}

	return &DetectionResult{
		TotalFunctions: totalFuncs,
		TestedCount:    testedCount,
		UntestedCount:  len(untested),
		CoveragePct:    coveragePct,
		Untested:       untested,
		DurationMs:     elapsed,
	}, nil
}

// DetectWithCoverage runs Detect and then enriches each UntestedFunc
// with file-level coverage information from a cover profile.
// If the profile cannot be parsed, a warning is logged and graph-only
// results are returned (graceful degradation).
func (d *UntestedDetector) DetectWithCoverage(
	ctx context.Context,
	cfg *Config,
	coverProfile string,
) (*DetectionResult, error) {
	result, err := d.Detect(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Attempt to parse the cover profile.
	cd, err := ParseCoverProfile(coverProfile)
	if err != nil {
		d.logger.Warn("failed to parse cover profile, returning graph-only results",
			"error", err)
		return result, nil
	}

	// Enrich each untested function with file-level coverage.
	for i := range result.Untested {
		uf := &result.Untested[i]
		if pct, ok := cd.FileCoverage[uf.File]; ok {
			uf.CoveragePct = pct
		}
	}

	return result, nil
}

// buildTestedSet returns the set of node IDs that are reachable (transitively)
// from any test function.
func (d *UntestedDetector) buildTestedSet(cfg *Config) map[string]bool {
	tested := make(map[string]bool)

	for _, node := range d.cg.Nodes {
		if !isTestFunc(node.Name) {
			continue
		}

		// Mark the test function itself.
		tested[node.ID] = true

		// Get all transitively reachable nodes from this test.
		edges := d.cg.TransitiveCallees(node.ID, 10)
		for _, edge := range edges {
			tested[edge.To] = true
		}
	}

	return tested
}

// isTestFunc returns true if the function name follows Go test conventions.
func isTestFunc(name string) bool {
	base := name
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		base = name[idx+1:]
	}
	return strings.HasPrefix(base, "Test") ||
		strings.HasPrefix(base, "Benchmark") ||
		strings.HasPrefix(base, "Fuzz") ||
		strings.HasPrefix(base, "Example")
}

// hasTestFile checks whether the given source file has a corresponding _test.go
// sibling in the same directory.
func hasTestFile(file string) bool {
	if file == "" {
		return false
	}
	dir := filepath.Dir(file)
	base := filepath.Base(file)
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	testFile := filepath.Join(dir, strings.TrimSuffix(base, ".go")+"_test.go")
	info, err := os.Stat(testFile)
	return err == nil && !info.IsDir()
}

// sortUntested sorts untested functions by priority descending, then by name.
func sortUntested(fns []UntestedFunc) {
	sort.SliceStable(fns, func(i, j int) bool {
		if fns[i].Priority != fns[j].Priority {
			return fns[i].Priority > fns[j].Priority
		}
		return fns[i].Name < fns[j].Name
	})
}
