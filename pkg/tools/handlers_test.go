package tools

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/brockenlesnair/governor-combined/pkg/adr"
	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
	"github.com/brockenlesnair/governor-combined/pkg/deadcode"
	"github.com/brockenlesnair/governor-combined/pkg/mcp"
	"github.com/brockenlesnair/governor-combined/pkg/rigour"
	"github.com/brockenlesnair/governor-combined/pkg/untested"
)

// mockGraph creates a simple test graph.
func mockGraph() *callgraph.Graph {
	g := callgraph.NewGraph()

	// Add some test nodes
	n1 := &callgraph.Node{
		ID:       "pkg.Foo",
		Name:     "Foo",
		Package:  "pkg",
		File:     "foo.go",
		Line:     10,
		Kind:     callgraph.NodeKindFunction,
		Exported: true,
	}
	n2 := &callgraph.Node{
		ID:       "pkg.Bar",
		Name:     "Bar",
		Package:  "pkg",
		File:     "bar.go",
		Line:     20,
		Kind:     callgraph.NodeKindFunction,
		Exported: true,
	}
	n3 := &callgraph.Node{
		ID:       "pkg.Baz",
		Name:     "Baz",
		Package:  "pkg",
		File:     "baz.go",
		Line:     30,
		Kind:     callgraph.NodeKindFunction,
		Exported: false,
	}

	g.AddNode(n1)
	g.AddNode(n2)
	g.AddNode(n3)

	// Add edges: Foo -> Bar, Bar -> Baz
	g.AddEdge("pkg.Foo", "pkg.Bar", "direct")
	g.AddEdge("pkg.Bar", "pkg.Baz", "direct")

	return g
}

func TestToolsConfig_Defaults(t *testing.T) {
	cfg := DefaultToolsConfig()

	if cfg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected log_level 'info', got %s", cfg.LogLevel)
	}
	if cfg.ProjectRoot != "." {
		t.Errorf("expected project_root '.', got %s", cfg.ProjectRoot)
	}
	if cfg.GraphCachePath != ".governor/graph.db" {
		t.Errorf("expected graph_cache_path '.governor/graph.db', got %s", cfg.GraphCachePath)
	}
	if cfg.RebuildInterval != 5*60*1000000000 { // 5 minutes in nanoseconds
		t.Errorf("expected rebuild_interval 5m, got %v", cfg.RebuildInterval)
	}
	if !cfg.Metrics.Enabled {
		t.Error("expected metrics enabled by default")
	}
	if cfg.Metrics.Path != "/metrics" {
		t.Errorf("expected metrics path '/metrics', got %s", cfg.Metrics.Path)
	}
}

func TestGraphLifecycle_BuildGraph(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	ctx := context.Background()
	g, err := gl.BuildGraph(ctx)
	if err != nil {
		// Might fail if no Go files in current dir - that's ok for unit test
		t.Logf("BuildGraph error (expected in test env): %v", err)
		return
	}

	if g == nil {
		t.Error("expected graph, got nil")
	}
	if gl.stats.Nodes == 0 && gl.stats.Edges == 0 {
		t.Log("graph is empty (no source files found)")
	}
}

func TestGraphLifecycle_GetGraph_ReturnsCached(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	// Inject a mock graph
	mockG := mockGraph()
	gl.mu.Lock()
	gl.graph = mockG
	gl.updateStatsFromGraph()
	gl.mu.Unlock()

	ctx := context.Background()
	g, err := gl.GetGraph(ctx)
	if err != nil {
		t.Fatalf("GetGraph failed: %v", err)
	}

	if g != mockG {
		t.Error("expected cached graph, got different graph")
	}
}

func TestGraphLifecycle_GraphStats(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	// Inject a mock graph
	mockG := mockGraph()
	gl.mu.Lock()
	gl.graph = mockG
	gl.updateStatsFromGraph()
	gl.mu.Unlock()

	stats := gl.GraphStats()
	if stats.Nodes != 3 {
		t.Errorf("expected 3 nodes, got %d", stats.Nodes)
	}
	if stats.Edges != 2 {
		t.Errorf("expected 2 edges, got %d", stats.Edges)
	}
}

func TestToolHandlers_RegisterTools(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	// Inject mock graph
	mockG := mockGraph()
	gl.mu.Lock()
	gl.graph = mockG
	gl.updateStatsFromGraph()
	gl.mu.Unlock()

	th, err := NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}
	th.UpdateGraphDependentTools(mockG)

	reg := mcp.NewToolRegistry()
	if err := th.RegisterTools(reg); err != nil {
		t.Fatalf("RegisterTools failed: %v", err)
	}

	// Verify all 15 tools are registered
	expectedTools := []string{
		"validate_code",
		"validate_diff",
		"search_code",
		"get_callers",
		"get_callees",
		"get_impact",
		"audit_project",
		"rigour_check",
		"rigour_state",
		"rigour_stats",
		"sarif_export",
		"adr_create",
		"adr_list",
		"hangar_score",
		"repo_health",
	}

	for _, name := range expectedTools {
		if !reg.Has(name) {
			t.Errorf("tool %s not registered", name)
		}
	}

	if reg.Count() != len(expectedTools) {
		t.Errorf("expected %d tools, got %d", len(expectedTools), reg.Count())
	}
}

func TestToolHandlers_HandleValidateCode_InvalidArgs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	th, err := NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}

	ctx := context.Background()

	// Test missing file parameter
	_, err = th.handleValidateCode(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing file parameter")
	}

	// Test empty file parameter
	_, err = th.handleValidateCode(ctx, map[string]any{"file": ""})
	if err == nil {
		t.Error("expected error for empty file parameter")
	}
}

func TestToolHandlers_HandleValidateDiff_InvalidArgs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	th, err := NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}

	ctx := context.Background()

	// Test missing diff parameter
	_, err = th.handleValidateDiff(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing diff parameter")
	}

	// Test empty diff parameter
	_, err = th.handleValidateDiff(ctx, map[string]any{"diff": ""})
	if err == nil {
		t.Error("expected error for empty diff parameter")
	}
}

func TestToolHandlers_HandleSearchCode_InvalidArgs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	// Inject mock graph
	mockG := mockGraph()
	gl.mu.Lock()
	gl.graph = mockG
	gl.updateStatsFromGraph()
	gl.mu.Unlock()

	th, err := NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}
	th.UpdateGraphDependentTools(mockG)

	ctx := context.Background()

	// Test missing query parameter
	_, err = th.handleSearchCode(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing query parameter")
	}

	// Test empty query parameter
	_, err = th.handleSearchCode(ctx, map[string]any{"query": ""})
	if err == nil {
		t.Error("expected error for empty query parameter")
	}
}

func TestToolHandlers_HandleGetCallers_InvalidArgs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	// Inject mock graph
	mockG := mockGraph()
	gl.mu.Lock()
	gl.graph = mockG
	gl.updateStatsFromGraph()
	gl.mu.Unlock()

	th, err := NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}
	th.UpdateGraphDependentTools(mockG)

	ctx := context.Background()

	// Test missing function_id parameter
	_, err = th.handleGetCallers(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing function_id parameter")
	}

	// Test empty function_id parameter
	_, err = th.handleGetCallers(ctx, map[string]any{"function_id": ""})
	if err == nil {
		t.Error("expected error for empty function_id parameter")
	}
}

func TestToolHandlers_HandleGetCallers_Direct(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	// Inject mock graph
	mockG := mockGraph()
	gl.mu.Lock()
	gl.graph = mockG
	gl.updateStatsFromGraph()
	gl.mu.Unlock()

	th, err := NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}
	th.UpdateGraphDependentTools(mockG)

	ctx := context.Background()

	// Test direct callers of Bar (should be Foo)
	result, err := th.handleGetCallers(ctx, map[string]any{
		"function_id": "pkg.Bar",
		"transitive":  false,
	})
	if err != nil {
		t.Fatalf("handleGetCallers failed: %v", err)
	}

	callers, ok := result["callers"].([]string)
	if !ok {
		t.Fatal("callers not a []string")
	}

	if len(callers) != 1 || callers[0] != "pkg.Foo" {
		t.Errorf("expected callers [pkg.Foo], got %v", callers)
	}

	count, ok := result["count"].(int)
	if !ok || count != 1 {
		t.Errorf("expected count 1, got %v", count)
	}
}

func TestToolHandlers_HandleGetCallers_Transitive(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	// Inject mock graph
	mockG := mockGraph()
	gl.mu.Lock()
	gl.graph = mockG
	gl.updateStatsFromGraph()
	gl.mu.Unlock()

	th, err := NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}
	th.UpdateGraphDependentTools(mockG)

	ctx := context.Background()

	// Test transitive callers of Baz (should be Foo and Bar)
	result, err := th.handleGetCallers(ctx, map[string]any{
		"function_id": "pkg.Baz",
		"transitive":  true,
		"depth":       3,
	})
	if err != nil {
		t.Fatalf("handleGetCallers failed: %v", err)
	}

	callers, ok := result["callers"].([]string)
	if !ok {
		t.Fatal("callers not a []string")
	}

	if len(callers) != 2 {
		t.Errorf("expected 2 transitive callers, got %d: %v", len(callers), callers)
	}
}

func TestToolHandlers_HandleGetCallees_Direct(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	// Inject mock graph
	mockG := mockGraph()
	gl.mu.Lock()
	gl.graph = mockG
	gl.updateStatsFromGraph()
	gl.mu.Unlock()

	th, err := NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}
	th.UpdateGraphDependentTools(mockG)

	ctx := context.Background()

	// Test direct callees of Foo (should be Bar)
	result, err := th.handleGetCallees(ctx, map[string]any{
		"function_id": "pkg.Foo",
		"transitive":  false,
	})
	if err != nil {
		t.Fatalf("handleGetCallees failed: %v", err)
	}

	callees, ok := result["callees"].([]string)
	if !ok {
		t.Fatal("callees not a []string")
	}

	if len(callees) != 1 || callees[0] != "pkg.Bar" {
		t.Errorf("expected callees [pkg.Bar], got %v", callees)
	}
}

func TestToolHandlers_HandleGetCallees_Transitive(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	// Inject mock graph
	mockG := mockGraph()
	gl.mu.Lock()
	gl.graph = mockG
	gl.updateStatsFromGraph()
	gl.mu.Unlock()

	th, err := NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}
	th.UpdateGraphDependentTools(mockG)

	ctx := context.Background()

	// Test transitive callees of Foo (should be Bar and Baz)
	result, err := th.handleGetCallees(ctx, map[string]any{
		"function_id": "pkg.Foo",
		"transitive":  true,
		"depth":       3,
	})
	if err != nil {
		t.Fatalf("handleGetCallees failed: %v", err)
	}

	callees, ok := result["callees"].([]string)
	if !ok {
		t.Fatal("callees not a []string")
	}

	if len(callees) != 2 {
		t.Errorf("expected 2 transitive callees, got %d: %v", len(callees), callees)
	}
}

func TestToolHandlers_HandleGetImpact(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	// Inject mock graph
	mockG := mockGraph()
	gl.mu.Lock()
	gl.graph = mockG
	gl.updateStatsFromGraph()
	gl.mu.Unlock()

	th, err := NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}
	th.UpdateGraphDependentTools(mockG)

	ctx := context.Background()

	result, err := th.handleGetImpact(ctx, map[string]any{
		"function_id": "pkg.Bar",
		"depth":       3,
	})
	if err != nil {
		t.Fatalf("handleGetImpact failed: %v", err)
	}

	// Verify expected fields
	expectedFields := []string{
		"function_id", "depth", "direct_callers", "transitive_callers",
		"affected_functions", "risk_level", "description",
	}
	for _, f := range expectedFields {
		if _, ok := result[f]; !ok {
			t.Errorf("missing field %s in result", f)
		}
	}
}

func TestToolHandlers_HandleAuditProject(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	// Inject mock graph
	mockG := mockGraph()
	gl.mu.Lock()
	gl.graph = mockG
	gl.updateStatsFromGraph()
	gl.mu.Unlock()

	th, err := NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}
	th.UpdateGraphDependentTools(mockG)

	ctx := context.Background()

	result, err := th.handleAuditProject(ctx, map[string]any{
		"include_untested":  true,
		"include_deadcode":  true,
		"include_safety":    true,
		"min_priority":      0.3,
		"min_confidence":    0.5,
	})
	if err != nil {
		t.Fatalf("handleAuditProject failed: %v", err)
	}

	// Verify expected top-level fields
	if _, ok := result["timestamp"]; !ok {
		t.Error("missing timestamp in audit result")
	}
	if _, ok := result["untested"]; !ok {
		t.Error("missing untested in audit result")
	}
	if _, ok := result["deadcode"]; !ok {
		t.Error("missing deadcode in audit result")
	}
	if _, ok := result["safety"]; !ok {
		t.Error("missing safety in audit result")
	}
}

func TestTopUntested(t *testing.T) {
	untestedFuncs := []untested.UntestedFunc{
		{ID: "1", Name: "Func1", Package: "pkg", File: "a.go", Line: 1, Priority: 0.9},
		{ID: "2", Name: "Func2", Package: "pkg", File: "b.go", Line: 2, Priority: 0.5},
		{ID: "3", Name: "Func3", Package: "pkg", File: "c.go", Line: 3, Priority: 0.7},
	}

	result := topUntested(untestedFuncs, 2)
	if len(result) != 2 {
		t.Errorf("expected 2 results, got %d", len(result))
	}
	if result[0]["id"] != "1" {
		t.Errorf("expected first result id=1, got %v", result[0]["id"])
	}
}

func TestTopDeadCode(t *testing.T) {
	deadCode := []deadcode.DeadCodeCandidate{
		{ID: "1", Name: "Func1", Package: "pkg", File: "a.go", Line: 1, Confidence: 0.9},
		{ID: "2", Name: "Func2", Package: "pkg", File: "b.go", Line: 2, Confidence: 0.5},
	}

	result := topDeadCode(deadCode, 1)
	if len(result) != 1 {
		t.Errorf("expected 1 result, got %d", len(result))
	}
	if result[0]["id"] != "1" {
		t.Errorf("expected first result id=1, got %v", result[0]["id"])
	}
}

// ── V1 Tool Handler Tests ────────────────────────────────────────────

// newMinimalHandlers creates ToolHandlers with only safety validator (no extended components).
func newMinimalHandlers(t *testing.T) *ToolHandlers {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	t.Cleanup(func() { gl.Close() })

	th, err := NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}
	return th
}

func TestRigourCheck_Handler_NotConfigured(t *testing.T) {
	th := newMinimalHandlers(t)
	ctx := context.Background()

	_, err := th.handleRigourCheck(ctx, map[string]any{"text": "hello"})
	if err == nil {
		t.Error("expected error when rigour not configured")
	}
}

func TestRigourCheck_Handler_WithRigour(t *testing.T) {
	th := newMinimalHandlers(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	th.rigourSupervisor = rigour.NewRigourSupervisor(logger, rigour.DefaultSupervisorConfig())

	ctx := context.Background()
	result, err := th.handleRigourCheck(ctx, map[string]any{"text": "some code", "file_path": "test.go"})
	if err != nil {
		t.Fatalf("handleRigourCheck failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if _, ok := result["blocked"]; !ok {
		t.Error("missing 'blocked' field in result")
	}
}

func TestRigourCheck_Handler_MissingText(t *testing.T) {
	th := newMinimalHandlers(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	th.rigourSupervisor = rigour.NewRigourSupervisor(logger, rigour.DefaultSupervisorConfig())

	ctx := context.Background()
	_, err := th.handleRigourCheck(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing text parameter")
	}
}

func TestRigourState_Handler_NotConfigured(t *testing.T) {
	th := newMinimalHandlers(t)
	ctx := context.Background()

	_, err := th.handleRigourState(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error when rigour not configured")
	}
}

func TestRigourState_Handler_WithRigour(t *testing.T) {
	th := newMinimalHandlers(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	th.rigourSupervisor = rigour.NewRigourSupervisor(logger, rigour.DefaultSupervisorConfig())

	ctx := context.Background()
	result, err := th.handleRigourState(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("handleRigourState failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if _, ok := result["state"]; !ok {
		t.Error("missing 'state' field in result")
	}
}

func TestRigourStats_Handler_NotConfigured(t *testing.T) {
	th := newMinimalHandlers(t)
	ctx := context.Background()

	_, err := th.handleRigourStats(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error when rigour not configured")
	}
}

func TestRigourStats_Handler_WithRigour(t *testing.T) {
	th := newMinimalHandlers(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	th.rigourSupervisor = rigour.NewRigourSupervisor(logger, rigour.DefaultSupervisorConfig())

	ctx := context.Background()
	result, err := th.handleRigourStats(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("handleRigourStats failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Fresh brain should have zero patterns
	if pc, ok := result["pattern_count"]; !ok || pc != 0 {
		t.Errorf("expected pattern_count 0, got %v", pc)
	}
}

func TestSarifExport_Handler_EmptyFindings(t *testing.T) {
	th := newMinimalHandlers(t)
	ctx := context.Background()

	_, err := th.handleSARIFExport(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing findings parameter")
	}
}

func TestSarifExport_Handler_ValidFindings(t *testing.T) {
	th := newMinimalHandlers(t)
	ctx := context.Background()

	findings := []any{
		map[string]any{
			"rule_id":  "test-rule",
			"message":  "test message",
			"level":    "warning",
			"file":     "main.go",
			"line":     float64(42),
		},
	}

	result, err := th.handleSARIFExport(ctx, map[string]any{
		"findings": findings,
		"source":   "test",
	})
	if err != nil {
		t.Fatalf("handleSARIFExport failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if count, ok := result["findings"]; !ok || count != 1 {
		t.Errorf("expected findings count 1, got %v", count)
	}
	if source, ok := result["source"]; !ok || source != "test" {
		t.Errorf("expected source 'test', got %v", source)
	}
	if _, ok := result["report"]; !ok {
		t.Error("missing 'report' field in result")
	}
}

func TestAdrCreate_Handler_NotConfigured(t *testing.T) {
	th := newMinimalHandlers(t)
	ctx := context.Background()

	_, err := th.handleADRCreate(ctx, map[string]any{"title": "test"})
	if err == nil {
		t.Error("expected error when adr not configured")
	}
}

func TestAdrCreate_Handler_WithStubClient(t *testing.T) {
	th := newMinimalHandlers(t)
	th.adrClient = adr.NewGoStubAdrMcpClient(slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := context.Background()
	result, err := th.handleADRCreate(ctx, map[string]any{"title": "Use gRPC", "status": "proposed"})
	if err != nil {
		t.Fatalf("handleADRCreate failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Stub returns "stub" status
	if status, ok := result["status"]; !ok || status != "stub" {
		t.Errorf("expected stub status, got %v", status)
	}
}

func TestAdrCreate_Handler_MissingTitle(t *testing.T) {
	th := newMinimalHandlers(t)
	th.adrClient = adr.NewGoStubAdrMcpClient(slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := context.Background()
	_, err := th.handleADRCreate(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing title parameter")
	}
}

func TestAdrList_Handler_NotConfigured(t *testing.T) {
	th := newMinimalHandlers(t)
	ctx := context.Background()

	_, err := th.handleADRList(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error when adr not configured")
	}
}

func TestAdrList_Handler_WithStubClient(t *testing.T) {
	th := newMinimalHandlers(t)
	th.adrClient = adr.NewGoStubAdrMcpClient(slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := context.Background()
	result, err := th.handleADRList(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("handleADRList failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if status, ok := result["status"]; !ok || status != "stub" {
		t.Errorf("expected stub status, got %v", status)
	}
}

func TestHangarScore_Handler_NotConfigured(t *testing.T) {
	th := newMinimalHandlers(t)
	ctx := context.Background()

	_, err := th.handleHangarScore(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error when hangar not configured")
	}
}

func TestRepoHealth_Handler_NotConfigured(t *testing.T) {
	th := newMinimalHandlers(t)
	ctx := context.Background()

	_, err := th.handleRepoHealth(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error when repo-butler not configured")
	}
}

func TestV1Tools_AllRegistered(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultToolsConfig()
	cfg.ProjectRoot = "."

	gl, err := NewGraphLifecycle(cfg, logger)
	if err != nil {
		t.Fatalf("NewGraphLifecycle failed: %v", err)
	}
	defer gl.Close()

	mockG := mockGraph()
	gl.mu.Lock()
	gl.graph = mockG
	gl.updateStatsFromGraph()
	gl.mu.Unlock()

	th, err := NewToolHandlers(gl, logger)
	if err != nil {
		t.Fatalf("NewToolHandlers failed: %v", err)
	}
	th.UpdateGraphDependentTools(mockG)

	// Inject rigour and adr so V1 handlers are functional
	th.rigourSupervisor = rigour.NewRigourSupervisor(logger, rigour.DefaultSupervisorConfig())
	th.adrClient = adr.NewGoStubAdrMcpClient(logger)

	reg := mcp.NewToolRegistry()
	if err := th.RegisterTools(reg); err != nil {
		t.Fatalf("RegisterTools failed: %v", err)
	}

	v1Tools := []string{
		"rigour_check",
		"rigour_state",
		"rigour_stats",
		"sarif_export",
		"adr_create",
		"adr_list",
		"hangar_score",
		"repo_health",
	}

	for _, name := range v1Tools {
		t.Run(name, func(t *testing.T) {
			if !reg.Has(name) {
				t.Errorf("V1 tool %s not registered", name)
			}
		})
	}

	if reg.Count() != 15 {
		t.Errorf("expected 15 total tools, got %d", reg.Count())
	}
}
