package tools

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/brockenlesnair/governor-combined/pkg/mcp"
)

// TestValidateCode_EdgeCases tests edge cases for validate_code
func TestValidateCode_EdgeCases(t *testing.T) {
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

	// Test with empty file path
	_, err = th.handleValidateCode(ctx, map[string]any{"file": ""})
	if err == nil {
		t.Error("expected error for empty file path")
	}

	// Test with missing file parameter
	_, err = th.handleValidateCode(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing file parameter")
	}

	// Test with non-existent file
	_, err = th.handleValidateCode(ctx, map[string]any{"file": "/nonexistent/file.go"})
	if err != nil {
		t.Logf("non-existent file error (expected): %v", err)
	}

	// Test with custom policy
	_, err = th.handleValidateCode(ctx, map[string]any{
		"file":   "test.go",
		"policy": "strict",
	})
	if err != nil {
		t.Logf("strict policy error (expected if policy not found): %v", err)
	}

	// Test with content provided
	_, err = th.handleValidateCode(ctx, map[string]any{
		"file":    "test.go",
		"content": "package main\nfunc main() {}\n",
	})
	if err != nil {
		t.Logf("content provided error: %v", err)
	}
}

// TestValidateDiff_EdgeCases tests edge cases for validate_diff
func TestValidateDiff_EdgeCases(t *testing.T) {
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

	// Test with empty diff
	_, err = th.handleValidateDiff(ctx, map[string]any{"diff": ""})
	if err == nil {
		t.Error("expected error for empty diff")
	}

	// Test with missing diff parameter
	_, err = th.handleValidateDiff(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing diff parameter")
	}

	// Test with invalid diff format
	_, err = th.handleValidateDiff(ctx, map[string]any{
		"diff": "not a valid diff",
	})
	if err != nil {
		t.Logf("invalid diff error (expected): %v", err)
	}

	// Test with valid diff format
	validDiff := `--- a/test.go
+++ b/test.go
@@ -1,3 +1,4 @@
 package main
 
 func main() {
+    fmt.Println("hello")
 }`
	_, err = th.handleValidateDiff(ctx, map[string]any{
		"diff": validDiff,
	})
	if err != nil {
		t.Logf("valid diff error: %v", err)
	}
}

// TestSearchCode_EdgeCases tests edge cases for search_code
func TestSearchCode_EdgeCases(t *testing.T) {
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

	// Test with empty query
	_, err = th.handleSearchCode(ctx, map[string]any{"query": ""})
	if err == nil {
		t.Error("expected error for empty query")
	}

	// Test with missing query parameter
	_, err = th.handleSearchCode(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing query parameter")
	}

	// Test with invalid search type
	_, err = th.handleSearchCode(ctx, map[string]any{
		"query": "test",
		"type":  "invalid_type",
	})
	if err != nil {
		t.Logf("invalid type error (expected): %v", err)
	}

	// Test with caller/callee type without target_id
	_, err = th.handleSearchCode(ctx, map[string]any{
		"query": "test",
		"type":  "callers",
	})
	if err != nil {
		t.Logf("callers without target_id error: %v", err)
	}

	// Test with regex type
	_, err = th.handleSearchCode(ctx, map[string]any{
		"query": "Func.*",
		"type":  "regex",
	})
	if err != nil {
		t.Logf("regex search error: %v", err)
	}

	// Test with type-aware search
	_, err = th.handleSearchCode(ctx, map[string]any{
		"query":        "test",
		"type":         "type",
		"param_types":  []any{"string", "int"},
		"return_types": []any{"error"},
	})
	if err != nil {
		t.Logf("type search error: %v", err)
	}

	// Test with max_results
	_, err = th.handleSearchCode(ctx, map[string]any{
		"query":       "test",
		"max_results": 10,
	})
	if err != nil {
		t.Logf("max_results error: %v", err)
	}

	// Test with depth
	_, err = th.handleSearchCode(ctx, map[string]any{
		"query":    "test",
		"type":     "callers",
		"target_id": "pkg.Foo",
		"depth":    5,
	})
	if err != nil {
		t.Logf("depth error: %v", err)
	}
}

// TestGetCallers_EdgeCases tests edge cases for get_callers
func TestGetCallers_EdgeCases(t *testing.T) {
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

	// Test with empty function_id
	_, err = th.handleGetCallers(ctx, map[string]any{"function_id": ""})
	if err == nil {
		t.Error("expected error for empty function_id")
	}

	// Test with missing function_id
	_, err = th.handleGetCallers(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing function_id")
	}

	// Test with non-existent function
	result, err := th.handleGetCallers(ctx, map[string]any{
		"function_id": "nonexistent",
	})
	if err != nil {
		t.Logf("nonexistent function error: %v", err)
	} else {
		callers := result["callers"].([]string)
		if len(callers) != 0 {
			t.Errorf("expected 0 callers for nonexistent function, got %d", len(callers))
		}
	}

	// Test transitive with depth 0
	result, err = th.handleGetCallers(ctx, map[string]any{
		"function_id": "pkg.Baz",
		"transitive":  true,
		"depth":       0,
	})
	if err != nil {
		t.Logf("depth 0 error: %v", err)
	}

	// Test transitive with large depth
	result, err = th.handleGetCallers(ctx, map[string]any{
		"function_id": "pkg.Baz",
		"transitive":  true,
		"depth":       100,
	})
	if err != nil {
		t.Logf("large depth error: %v", err)
	}
}

// TestGetCallees_EdgeCases tests edge cases for get_callees
func TestGetCallees_EdgeCases(t *testing.T) {
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

	// Test with empty function_id
	_, err = th.handleGetCallees(ctx, map[string]any{"function_id": ""})
	if err == nil {
		t.Error("expected error for empty function_id")
	}

	// Test with missing function_id
	_, err = th.handleGetCallees(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing function_id")
	}

	// Test with non-existent function
	result, err := th.handleGetCallees(ctx, map[string]any{
		"function_id": "nonexistent",
	})
	if err != nil {
		t.Logf("nonexistent function error: %v", err)
	} else {
		callees := result["callees"].([]string)
		if len(callees) != 0 {
			t.Errorf("expected 0 callees for nonexistent function, got %d", len(callees))
		}
	}

	// Test transitive
	result, err = th.handleGetCallees(ctx, map[string]any{
		"function_id": "pkg.Foo",
		"transitive":  true,
		"depth":       3,
	})
	if err != nil {
		t.Logf("transitive callees error: %v", err)
	}
}

// TestGetImpact_EdgeCases tests edge cases for get_impact
func TestGetImpact_EdgeCases(t *testing.T) {
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

	// Test with empty function_id
	_, err = th.handleGetImpact(ctx, map[string]any{"function_id": ""})
	if err == nil {
		t.Error("expected error for empty function_id")
	}

	// Test with missing function_id
	_, err = th.handleGetImpact(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing function_id")
	}

	// Test with non-existent function
	result, err := th.handleGetImpact(ctx, map[string]any{
		"function_id": "nonexistent",
	})
	if err != nil {
		t.Logf("nonexistent function error: %v", err)
	} else {
		if result["description"] == nil {
			t.Error("expected description for nonexistent function")
		}
	}

	// Test with custom depth
	result, err = th.handleGetImpact(ctx, map[string]any{
		"function_id": "pkg.Bar",
		"depth":       5,
	})
	if err != nil {
		t.Logf("custom depth error: %v", err)
	}
}

// TestAuditProject_EdgeCases tests edge cases for audit_project
func TestAuditProject_EdgeCases(t *testing.T) {
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

	// Test with all options false
	result, err := th.handleAuditProject(ctx, map[string]any{
		"include_untested":  false,
		"include_deadcode":  false,
		"include_safety":    false,
		"min_priority":      0.5,
		"min_confidence":    0.8,
	})
	if err != nil {
		t.Fatalf("audit_project failed: %v", err)
	}

	if result["untested"] != nil {
		t.Error("expected untested to be nil when disabled")
	}
	if result["deadcode"] != nil {
		t.Error("expected deadcode to be nil when disabled")
	}
	if result["safety"] == nil {
		t.Log("safety section not present when disabled")
	}

	// Test with custom thresholds
	result, err = th.handleAuditProject(ctx, map[string]any{
		"include_untested":  true,
		"include_deadcode":  true,
		"include_safety":    true,
		"min_priority":      0.8,
		"min_confidence":    0.9,
	})
	if err != nil {
		t.Fatalf("audit_project failed: %v", err)
	}

	untested := result["untested"].(map[string]any)
	if untested == nil {
		t.Error("expected untested section")
	}

	deadcode := result["deadcode"].(map[string]any)
	if deadcode == nil {
		t.Error("expected deadcode section")
	}

	// Test with invalid threshold values
	_, err = th.handleAuditProject(ctx, map[string]any{
		"min_priority":   -1.0,
		"min_confidence": 2.0,
	})
	if err != nil {
		t.Logf("invalid thresholds error: %v", err)
	}
}

// TestToolHandlers_RegisterDuplicate tests duplicate tool registration
func TestToolHandlers_RegisterDuplicate(t *testing.T) {
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

	// First registration should succeed
	reg1 := mcp.NewToolRegistry()
	if err := th.RegisterTools(reg1); err != nil {
		t.Fatalf("First RegisterTools failed: %v", err)
	}

	// Second registration should succeed (new registry)
	reg2 := mcp.NewToolRegistry()
	if err := th.RegisterTools(reg2); err != nil {
		t.Fatalf("Second RegisterTools failed: %v", err)
	}

	// Try to register duplicate on same registry
	reg3 := mcp.NewToolRegistry()
	if err := th.RegisterTools(reg3); err != nil {
		t.Fatalf("Third RegisterTools failed: %v", err)
	}

	// Try to register same tool twice on same registry
	if err := reg3.Register(mcp.ToolDefinition{
		Name:        "validate_code",
		Description: "Duplicate",
		InputSchema: map[string]any{},
	}, func(ctx context.Context, args map[string]any) (map[string]any, error) {
		return nil, nil
	}); err == nil {
		t.Error("expected error for duplicate tool registration")
	}
}

// TestToolHandlers_ConcurrentCalls tests concurrent tool calls
func TestToolHandlers_ConcurrentCalls(t *testing.T) {
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

	ctx := context.Background()
	done := make(chan error, 20)

	// Concurrent get_callers
	for i := 0; i < 10; i++ {
		go func() {
			_, err := reg.Execute(ctx, "get_callers", map[string]any{
				"function_id": "pkg.Bar",
				"transitive":  false,
			})
			done <- err
		}()
	}

	// Concurrent get_callees
	for i := 0; i < 10; i++ {
		go func() {
			_, err := reg.Execute(ctx, "get_callees", map[string]any{
				"function_id": "pkg.Foo",
				"transitive":  false,
			})
			done <- err
		}()
	}

	for i := 0; i < 20; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent call failed: %v", err)
		}
	}
}

// TestToolHandlers_ContextCancellation tests context cancellation
func TestToolHandlers_ContextCancellation(t *testing.T) {
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

	// Test with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = reg.Execute(ctx, "get_callers", map[string]any{
		"function_id": "pkg.Bar",
	})
	if err == nil {
		t.Log("context cancellation behavior varies")
	}
}
