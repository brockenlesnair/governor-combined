package deadcode

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
)

// Detector finds dead code in a call graph.
type Detector struct {
	graph    *callgraph.Graph
	logger   *slog.Logger
	analyzer *ReachabilityAnalyzer
	scorer   *ConfidenceScorer
}

// NewDetector creates a new Detector.
func NewDetector(cg *callgraph.Graph, logger *slog.Logger) *Detector {
	if logger == nil {
		logger = slog.Default()
	}
	return &Detector{
		graph:    cg,
		logger:   logger,
		analyzer: NewReachabilityAnalyzer(cg),
		scorer:   NewConfidenceScorer(cg),
	}
}

// Detect runs dead code detection on the call graph.
func (d *Detector) Detect(ctx context.Context, cfg *Config) (*DetectionResult, error) {
	start := time.Now()

	if cfg == nil {
		cfg = &Config{
			ExcludeExported: true,
			ExcludeTests:    true,
			MinConfidence:   0.5,
		}
	}

	// Validate graph
	if len(d.graph.Nodes) == 0 {
		return nil, NewError(ErrCodeGraphEmpty, "call graph is empty")
	}

	// 1. Find entry points
	entryPoints := d.analyzer.FindEntryPoints()
	d.logger.Debug("found entry points", "count", len(entryPoints))

	// 2. Compute reachable set
	reachable := d.analyzer.ReachableSet(ctx, entryPoints)

	// 3. Iterate all nodes and classify
	var deadCode []DeadCodeCandidate
	aliveCount := 0

	for _, node := range d.graph.Nodes {
		// Skip init functions (always alive)
		nodeFuncName := shortName(node.Name)
		if node.Name == "init" || strings.Contains(node.Name, ".init") || strings.HasPrefix(node.Name, "init.") || strings.HasPrefix(node.Name, "init#") {
			aliveCount++
			continue
		}

		// Skip test functions if ExcludeTests=true
		if cfg.ExcludeTests && isTestFunc(nodeFuncName) {
			aliveCount++
			continue
		}

		// Skip reachable nodes — they are alive
		if reachable[node.ID] {
			aliveCount++
			continue
		}

		// Skip exported functions if ExcludeExported=true AND reachable
		if cfg.ExcludeExported && node.Exported && reachable[node.ID] {
			aliveCount++
			continue
		}

		isReachable := reachable[node.ID]

		// Count callers
		callerCount := len(d.graph.Predecessors(node.ID))

		// Score confidence
		confidence := d.scorer.Score(node, isReachable, callerCount)

		// Apply MinConfidence filter
		if confidence < cfg.MinConfidence {
			aliveCount++
			continue
		}

		// Classify dead kind
		deadKind := classifyDeadKind(node)

		// Build entry point list for this candidate
		var relevantEntryPoints []string
		for _, ep := range entryPoints {
			if isReachable || confidence > 0 {
				relevantEntryPoints = append(relevantEntryPoints, ep)
			}
		}
		if len(relevantEntryPoints) > 5 {
			relevantEntryPoints = relevantEntryPoints[:5]
		}

		candidate := DeadCodeCandidate{
			ID:          node.ID,
			Name:        node.Name,
			Package:     node.Package,
			File:        node.File,
			Line:        node.Line,
			Kind:        string(node.Kind),
			Exported:    node.Exported,
			Confidence:  confidence,
			Reason:      buildReason(node, isReachable, callerCount),
			EntryPoints: relevantEntryPoints,
			Reachable:   isReachable,
			DeadKind:    deadKind,
		}

		deadCode = append(deadCode, candidate)
	}

	// 4. Sort by confidence descending
	sort.Slice(deadCode, func(i, j int) bool {
		return deadCode[i].Confidence > deadCode[j].Confidence
	})

	duration := time.Since(start).Milliseconds()

	return &DetectionResult{
		TotalEntities: len(d.graph.Nodes),
		AliveCount:    aliveCount,
		DeadCount:     len(deadCode),
		DeadCode:      deadCode,
		DurationMs:    duration,
	}, nil
}

// isTestFunc checks if a function name is a test function.
func isTestFunc(name string) bool {
	return strings.HasPrefix(name, "Test") ||
		strings.HasPrefix(name, "Benchmark") ||
		strings.HasPrefix(name, "Fuzz") ||
		strings.HasPrefix(name, "Example")
}

// classifyDeadKind maps a callgraph NodeKind to a dead kind string.
func classifyDeadKind(node *callgraph.Node) string {
	switch node.Kind {
	case callgraph.NodeKindFunction:
		return "function"
	case callgraph.NodeKindMethod:
		return "method"
	case callgraph.NodeKindType:
		return "type"
	default:
		return "function"
	}
}

// buildReason generates a human-readable reason for why code is dead.
func buildReason(node *callgraph.Node, reachable bool, callerCount int) string {
	if !reachable {
		if callerCount == 0 {
			return "not reachable from any entry point and has no callers"
		}
		return "not reachable from any entry point"
	}
	if callerCount == 0 {
		if !node.Exported {
			return "unexported function with no callers, potentially unused"
		}
		return "exported function with no callers, potentially unused"
	}
	return "potentially unused"
}
