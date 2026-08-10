package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
	"github.com/brockenlesnair/governor-combined/pkg/persist"
)

// GraphStats holds statistics about the call graph.
type GraphStats struct {
	Nodes        int       `json:"nodes"`
	Edges        int       `json:"edges"`
	LastBuilt    time.Time `json:"last_built"`
	StaleFiles   int       `json:"stale_files"`
	BuildDurationMs int64  `json:"build_duration_ms"`
}

// GraphLifecycle manages the call graph lifecycle: build, cache, rebuild.
type GraphLifecycle struct {
	mu           sync.RWMutex
	graph        *callgraph.Graph
	stats        GraphStats
	config       *ToolsConfig
	persistStore *persist.SQLiteStore
	logger       *slog.Logger
	watcherStop  chan struct{}
	rebuildCh    chan struct{}
}

// NewGraphLifecycle creates a new GraphLifecycle.
func NewGraphLifecycle(cfg *ToolsConfig, logger *slog.Logger) (*GraphLifecycle, error) {
	var store *persist.SQLiteStore
	var err error

	if cfg.GraphCachePath != "" {
		// Ensure directory exists
		dir := filepath.Dir(cfg.GraphCachePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create cache dir: %w", err)
		}

		store, err = persist.NewSQLiteStore(cfg.GraphCachePath)
		if err != nil {
			return nil, fmt.Errorf("open persist store: %w", err)
		}

		// Initialize schema
		ctx := context.Background()
		if err := store.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("initialize persist store: %w", err)
		}
	}

	gl := &GraphLifecycle{
		config:       cfg,
		persistStore: store,
		logger:       logger,
		watcherStop:  make(chan struct{}),
		rebuildCh:    make(chan struct{}, 1),
		stats: GraphStats{
			LastBuilt: time.Time{},
		},
	}

	// Try to load cached graph on startup
	if store != nil {
		if g, err := gl.loadCachedGraph(); err == nil && g != nil {
			gl.graph = g
			gl.updateStatsFromGraph()
			logger.Info("Loaded cached call graph", "nodes", gl.stats.Nodes, "edges", gl.stats.Edges)
		} else {
			logger.Info("No cached graph found, will build on first request", "error", err)
		}
	}

	return gl, nil
}

// BuildGraph parses the entire codebase and builds the call graph.
func (gl *GraphLifecycle) BuildGraph(ctx context.Context) (*callgraph.Graph, error) {
	start := time.Now()
	gl.logger.Info("Building call graph", "project_root", gl.config.ProjectRoot)

	// Create a new graph builder with multi-language support (Go + tree-sitter)
	builder := callgraph.NewGraphBuilder()

	// Build the graph
	g, err := builder.Build(ctx, gl.config.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("build graph: %w", err)
	}
	if g == nil {
		return nil, fmt.Errorf("build graph returned nil")
	}

	// Update stats
	gl.mu.Lock()
	gl.graph = g
	gl.stats.Nodes = len(g.Nodes)
	gl.stats.Edges = len(g.Edges)
	gl.stats.LastBuilt = time.Now()
	gl.stats.BuildDurationMs = time.Since(start).Milliseconds()
	gl.mu.Unlock()

	// Cache to SQLite if persist store is available
	if gl.persistStore != nil {
		if err := gl.cacheGraph(g); err != nil {
			gl.logger.Warn("Failed to cache graph", "error", err)
		} else {
			gl.logger.Info("Graph cached to SQLite")
		}
	}

	gl.logger.Info("Call graph built", "nodes", gl.stats.Nodes, "edges", gl.stats.Edges, "duration_ms", gl.stats.BuildDurationMs)
	return g, nil
}

// loadCachedGraph loads the call graph from the SQLite store.
func (gl *GraphLifecycle) loadCachedGraph() (*callgraph.Graph, error) {
	if gl.persistStore == nil {
		return nil, fmt.Errorf("no persist store")
	}

	ctx := context.Background()
	pg, err := gl.persistStore.LoadCallGraph(ctx)
	if err != nil {
		return nil, err
	}

	// Convert persist.CallGraph to callgraph.Graph
	g := callgraph.NewGraph()
	for _, n := range pg.Nodes {
		node := &callgraph.Node{
			ID:       n.ID,
			Name:     n.Name,
			Package:  n.Package,
			File:     n.File,
			Line:     n.Line,
			Kind:     callgraph.NodeKind(n.Kind),
			Receiver: n.Receiver,
			Exported: n.Exported,
		}
		g.AddNode(node)
	}
	for _, e := range pg.Edges {
		g.AddEdge(e.From, e.To, e.CallType)
	}

	gl.stats.Nodes = len(pg.Nodes)
	gl.stats.Edges = len(pg.Edges)
	return g, nil
}

// cacheGraph saves the call graph to the SQLite store.
func (gl *GraphLifecycle) cacheGraph(g *callgraph.Graph) error {
	if gl.persistStore == nil {
		return nil
	}

	ctx := context.Background()
	pg := persist.NewCallGraph()

	for _, n := range g.Nodes {
		pg.AddNode(&persist.Node{
			ID:       n.ID,
			Name:     n.Name,
			Package:  n.Package,
			File:     n.File,
			Line:     n.Line,
			Kind:     string(n.Kind),
			Receiver: n.Receiver,
			Exported: n.Exported,
		})
	}
	for _, e := range g.Edges {
		pg.AddEdge(e.From, e.To, e.CallType)
	}

	return gl.persistStore.SaveCallGraph(ctx, pg)
}

// GetGraph returns the current call graph, building it if necessary.
func (gl *GraphLifecycle) GetGraph(ctx context.Context) (*callgraph.Graph, error) {
	gl.mu.RLock()
	g := gl.graph
	gl.mu.RUnlock()

	if g != nil {
		return g, nil
	}

	// Build on first access
	return gl.BuildGraph(ctx)
}

// GraphStats returns current graph statistics.
func (gl *GraphLifecycle) GraphStats() GraphStats {
	gl.mu.RLock()
	defer gl.mu.RUnlock()
	return gl.stats
}

// RebuildGraphAsync triggers an async graph rebuild (non-blocking).
func (gl *GraphLifecycle) RebuildGraphAsync(ctx context.Context) {
	select {
	case gl.rebuildCh <- struct{}{}:
		gl.logger.Debug("Graph rebuild triggered")
	default:
		gl.logger.Debug("Graph rebuild already pending")
	}
}

// StartRebuildLoop starts a background loop that processes rebuild requests.
func (gl *GraphLifecycle) StartRebuildLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(gl.config.RebuildInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-gl.rebuildCh:
				gl.logger.Info("Rebuilding call graph due to file changes")
				if _, err := gl.BuildGraph(ctx); err != nil {
					gl.logger.Error("Graph rebuild failed", "error", err)
				}
			case <-ticker.C:
				// Periodic rebuild
				if gl.graph != nil {
					gl.logger.Debug("Periodic graph rebuild")
					if _, err := gl.BuildGraph(ctx); err != nil {
						gl.logger.Error("Periodic graph rebuild failed", "error", err)
					}
				}
			}
		}
	}()
}

// updateStatsFromGraph updates stats from the current graph (caller must hold lock).
func (gl *GraphLifecycle) updateStatsFromGraph() {
	if gl.graph != nil {
		gl.stats.Nodes = len(gl.graph.Nodes)
		gl.stats.Edges = len(gl.graph.Edges)
	}
}

// Close closes the persist store if open.
func (gl *GraphLifecycle) Close() error {
	if gl.persistStore != nil {
		return gl.persistStore.Close()
	}
	return nil
}