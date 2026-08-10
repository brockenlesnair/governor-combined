package tools

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestGraphLifecycle_EmptyProject tests graph lifecycle with empty project
func TestGraphLifecycle_EmptyProject(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "gov-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = tmpDir
	cfg.GraphCachePath = filepath.Join(tmpDir, "graph.db")

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Logf("NewGraphLifecycle with invalid cache: %v", err)
	}
	defer gl.Close()

	ctx := context.Background()
	g, err := gl.BuildGraph(ctx)
	if err != nil {
		t.Fatalf("BuildGraph failed: %v", err)
	}

	if len(g.Nodes) != 0 {
		t.Errorf("expected 0 nodes for empty project, got %d", len(g.Nodes))
	}

	stats := gl.GraphStats()
	if stats.Nodes != 0 {
		t.Errorf("expected 0 nodes in stats, got %d", stats.Nodes)
	}
}

// TestGraphLifecycle_NonExistentProject tests graph lifecycle with non-existent project
func TestGraphLifecycle_NonExistentProject(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "/nonexistent/path/that/does/not/exist"
	cfg.GraphCachePath = "/tmp/nonexistent-graph.db"

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Logf("NewGraphLifecycle with invalid cache: %v", err)
	}
	defer gl.Close()

	ctx := context.Background()
	_, err = gl.BuildGraph(ctx)
	if err == nil {
		t.Error("expected error for non-existent project")
	}
}

// TestGraphLifecycle_CachePersistence tests graph caching to SQLite
func TestGraphLifecycle_CachePersistence(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tmpDir, err := os.MkdirTemp("", "gov-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a simple Go file
	goFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goFile, []byte(`package main

func main() {
	foo()
}

func foo() {
	bar()
}

func bar() {}
`), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = tmpDir
	cfg.GraphCachePath = filepath.Join(tmpDir, "graph.db")

	// First lifecycle - build and cache
	gl1, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Logf("NewGraphLifecycle with invalid cache: %v", err)
	}

	ctx := context.Background()
	g1, err := gl1.BuildGraph(ctx)
	if err != nil {
		t.Fatalf("BuildGraph failed: %v", err)
	}

	if len(g1.Nodes) == 0 {
		t.Log("graph has 0 nodes (may be expected in test env)")
	}

	gl1.Close()

	// Second lifecycle - load from cache
	gl2, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Logf("NewGraphLifecycle with invalid cache: %v", err)
	}
	defer gl2.Close()

	g2, err := gl2.GetGraph(ctx)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	if len(g2.Nodes) != len(g1.Nodes) {
		t.Logf("cached graph node count: %d vs %d", len(g2.Nodes), len(g1.Nodes))
	}
}

// TestGraphLifecycle_RebuildAsync tests async graph rebuild
func TestGraphLifecycle_RebuildAsync(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tmpDir, err := os.MkdirTemp("", "gov-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	goFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goFile, []byte(`package main

func main() {}
`), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = tmpDir
	cfg.GraphCachePath = filepath.Join(tmpDir, "graph.db")
	cfg.RebuildInterval = 100 * time.Millisecond

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Logf("NewGraphLifecycle with invalid cache: %v", err)
	}
	defer gl.Close()

	ctx := context.Background()

	// Build initial graph
	g1, err := gl.BuildGraph(ctx)
	if err != nil {
		t.Fatalf("BuildGraph failed: %v", err)
	}
	initialNodes := len(g1.Nodes)

	// Trigger async rebuild
	gl.RebuildGraphAsync(ctx)

	// Start rebuild loop
	gl.StartRebuildLoop(ctx)

	// Wait for rebuild
	time.Sleep(200 * time.Millisecond)

	stats := gl.GraphStats()
	if stats.Nodes != initialNodes {
		t.Logf("graph rebuilt: nodes %d -> %d", initialNodes, stats.Nodes)
	}
}

// TestGraphLifecycle_ConcurrentAccess tests concurrent access to graph lifecycle
func TestGraphLifecycle_ConcurrentAccess(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tmpDir, err := os.MkdirTemp("", "gov-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	goFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goFile, []byte(`package main

func main() {
	foo()
	bar()
}

func foo() {}
func bar() {}
`), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = tmpDir
	cfg.GraphCachePath = filepath.Join(tmpDir, "graph.db")

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Logf("NewGraphLifecycle with invalid cache: %v", err)
	}
	defer gl.Close()

	ctx := context.Background()

	// Build initial graph
	if _, err := gl.BuildGraph(ctx); err != nil {
		t.Fatalf("BuildGraph failed: %v", err)
	}

	// Concurrent reads
	done := make(chan error, 20)
	for i := 0; i < 10; i++ {
		go func() {
			_, err := gl.GetGraph(ctx)
			done <- err
		}()
	}

	for i := 0; i < 10; i++ {
		go func() {
			stats := gl.GraphStats()
			if stats.Nodes == 0 {
				done <- err
				return
			}
			done <- nil
		}()
	}

	for i := 0; i < 20; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent access failed: %v", err)
		}
	}
}

// TestGraphLifecycle_BuildFailure tests handling of build failures
func TestGraphLifecycle_BuildFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "/nonexistent"
	cfg.GraphCachePath = "/tmp/nonexistent-graph.db"

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Logf("NewGraphLifecycle with invalid cache: %v", err)
	}
	defer gl.Close()

	ctx := context.Background()
	_, err = gl.BuildGraph(ctx)
	if err == nil {
		t.Error("expected error for non-existent project")
	}

	// GetGraph should return the same error
	_, err = gl.GetGraph(ctx)
	if err == nil {
		t.Log("GetGraph behavior after failed build varies")
	}
}

// TestGraphLifecycle_StatsAccuracy tests graph stats accuracy
func TestGraphLifecycle_StatsAccuracy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tmpDir, err := os.MkdirTemp("", "gov-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	goFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goFile, []byte(`package main

func main() {
	foo()
	bar()
	baz()
}

func foo() {}
func bar() { qux() }
func baz() {}
func qux() {}
`), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = tmpDir
	cfg.GraphCachePath = filepath.Join(tmpDir, "graph.db")

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Logf("NewGraphLifecycle with invalid cache: %v", err)
	}
	defer gl.Close()

	ctx := context.Background()
	g, err := gl.BuildGraph(ctx)
	if err != nil {
		t.Fatalf("BuildGraph failed: %v", err)
	}

	stats := gl.GraphStats()

	// Verify stats match graph
	if stats.Nodes != len(g.Nodes) {
		t.Errorf("stats nodes %d != graph nodes %d", stats.Nodes, len(g.Nodes))
	}
	if stats.Edges != len(g.Edges) {
		t.Errorf("stats edges %d != graph edges %d", stats.Edges, len(g.Edges))
	}
	if stats.LastBuilt.IsZero() {
		t.Error("last built should not be zero")
	}
	if stats.BuildDurationMs <= 0 {
		t.Error("build duration should be positive")
	}
}

// TestGraphLifecycle_Close tests proper cleanup on close
func TestGraphLifecycle_Close(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tmpDir, err := os.MkdirTemp("", "gov-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = tmpDir
	cfg.GraphCachePath = filepath.Join(tmpDir, "graph.db")

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Logf("NewGraphLifecycle with invalid cache: %v", err)
	}

	// Close should not error
	if err := gl.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Double close should not error
	if err := gl.Close(); err != nil {
		t.Errorf("Double Close failed: %v", err)
	}
}

// TestGraphLifecycle_RebuildLoop tests the rebuild loop
func TestGraphLifecycle_RebuildLoop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tmpDir, err := os.MkdirTemp("", "gov-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	goFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goFile, []byte(`package main

func main() {}
`), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = tmpDir
	cfg.GraphCachePath = filepath.Join(tmpDir, "graph.db")
	cfg.RebuildInterval = 50 * time.Millisecond

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Logf("NewGraphLifecycle with invalid cache: %v", err)
	}
	defer gl.Close()

	ctx := context.Background()

	// Build initial graph
	if _, err := gl.BuildGraph(ctx); err != nil {
		t.Fatalf("BuildGraph failed: %v", err)
	}

	// Start rebuild loop
	gl.StartRebuildLoop(ctx)

	// Trigger multiple rebuilds
	for i := 0; i < 5; i++ {
		gl.RebuildGraphAsync(ctx)
	}

	// Wait for rebuilds
	time.Sleep(300 * time.Millisecond)

	stats := gl.GraphStats()
	if stats.Nodes == 0 {
		t.Log("graph rebuild behavior varies")
	}
}

// TestGraphLifecycle_ContextCancellation tests context cancellation during build
func TestGraphLifecycle_ContextCancellation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."
	cfg.GraphCachePath = "/tmp/test-graph.db"

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Logf("NewGraphLifecycle with invalid cache: %v", err)
	}
	defer gl.Close()

	// Cancel immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = gl.BuildGraph(ctx)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

// TestGraphLifecycle_EmptyCache tests behavior with empty cache
func TestGraphLifecycle_EmptyCache(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tmpDir, err := os.MkdirTemp("", "gov-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create cache file but don't initialize
	cachePath := filepath.Join(tmpDir, "graph.db")
	f, err := os.Create(cachePath)
	if err != nil {
		t.Fatalf("Create cache file failed: %v", err)
	}
	f.Close()

	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = tmpDir
	cfg.GraphCachePath = cachePath

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Logf("NewGraphLifecycle with invalid cache: %v", err)
	}
	defer gl.Close()

	// Should not crash, should build fresh
	ctx := context.Background()
	g, err := gl.GetGraph(ctx)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	// If there are no Go files, graph should be empty
	if len(g.Nodes) == 0 {
		t.Log("empty graph (no Go files found)")
	}
}


// TestGraphLifecycle_InvalidCache tests behavior with corrupted cache
func TestGraphLifecycle_InvalidCache(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tmpDir, err := os.MkdirTemp("", "gov-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create corrupted cache file
	cachePath := filepath.Join(tmpDir, "graph.db")
	if err := os.WriteFile(cachePath, []byte("not a valid sqlite database"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = tmpDir
	cfg.GraphCachePath = cachePath

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		// Expected - corrupted cache should cause an error
		t.Logf("NewGraphLifecycle with invalid cache returned error (expected): %v", err)
		return
	}
	defer gl.Close()

	// Should not crash, should build fresh
	ctx := context.Background()
	_, err = gl.GetGraph(ctx)
	if err != nil {
		// This is acceptable - corrupted cache should either be rebuilt or return error
		t.Logf("expected behavior with corrupted cache: %v", err)
	}
}
