package gateway

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brockenlesnair/governor-combined/pkg/mcp"
)

func TestGateway_New(t *testing.T) {
	cfg := &GatewayConfig{ToolDispatchUseRegistry: true}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	g := NewGateway(cfg, logger)
	if g == nil {
		t.Fatal("NewGateway returned nil")
	}
	if g.registry == nil {
		t.Error("registry not initialized")
	}
	if g.logger == nil {
		t.Error("logger not initialized")
	}
}

func TestGateway_RegisterTool(t *testing.T) {
	g := NewGateway(&GatewayConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	handler := func(ctx context.Context, args map[string]any) (map[string]any, error) {
		return map[string]any{"echo": "test"}, nil
	}

	def := mcp.ToolDefinition{
		Name:        "echo",
		Description: "Echo tool",
		InputSchema: map[string]any{"type": "object"},
	}

	if err := g.RegisterTool(def, handler); err != nil {
		t.Fatalf("RegisterTool failed: %v", err)
	}

	if !g.registry.Has("echo") {
		t.Error("tool not registered in registry")
	}
}

func TestGateway_ExecuteTool(t *testing.T) {
	g := NewGateway(&GatewayConfig{ToolDispatchUseRegistry: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	handler := func(ctx context.Context, args map[string]any) (map[string]any, error) {
		return map[string]any{"result": args["input"]}, nil
	}

	g.RegisterTool(mcp.ToolDefinition{
		Name:        "echo",
		Description: "Echo tool",
		InputSchema: map[string]any{"type": "object"},
	}, handler)

	result, err := g.ExecuteTool(context.Background(), "echo", map[string]any{"input": "hello"})
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}

	if result["result"] != "hello" {
		t.Errorf("expected 'hello', got %v", result["result"])
	}
}

func TestGateway_ExecuteTool_NotFound(t *testing.T) {
	g := NewGateway(&GatewayConfig{ToolDispatchUseRegistry: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := g.ExecuteTool(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent tool")
	}
}

func TestGateway_ListTools(t *testing.T) {
	g := NewGateway(&GatewayConfig{ToolDispatchUseRegistry: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	tools := g.ListTools()
	// Should have built-in tools: health_check, list_tools
	if len(tools) < 2 {
		t.Errorf("expected at least 2 built-in tools, got %d", len(tools))
	}

	found := make(map[string]bool)
	for _, t := range tools {
		found[t.Name] = true
	}
	if !found["health_check"] {
		t.Error("health_check not found in list")
	}
	if !found["list_tools"] {
		t.Error("list_tools not found in list")
	}
}

func TestGateway_HandleWS(t *testing.T) {
	g := NewGateway(&GatewayConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := g.HandleWS()
	if handler == nil {
		t.Fatal("HandleWS returned nil")
	}

	// Test that it's a valid http.HandlerFunc
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()
	handler(w, req)
	// WebSocket upgrade will fail on test recorder, but handler should not panic
	// Just verify it doesn't crash
}

func TestWSServer_New(t *testing.T) {
	g := NewGateway(&GatewayConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ws := NewWSServer(g, slog.New(slog.NewTextHandler(io.Discard, nil)), WSConfig{})
	if ws == nil {
		t.Fatal("NewWSServer returned nil")
	}
	if ws.gateway != g {
		t.Error("gateway not set")
	}
}

func TestWSServer_ConnectedClients(t *testing.T) {
	g := NewGateway(&GatewayConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ws := NewWSServer(g, slog.New(slog.NewTextHandler(io.Discard, nil)), WSConfig{})

	if ws.ConnectedClients() != 0 {
		t.Errorf("expected 0 clients, got %d", ws.ConnectedClients())
	}
}

func TestWSServer_Broadcast(t *testing.T) {
	g := NewGateway(&GatewayConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ws := NewWSServer(g, slog.New(slog.NewTextHandler(io.Discard, nil)), WSConfig{})

	// Should not panic with no clients
	ws.Broadcast([]byte("test message"))
}

func TestBuiltinTools_HealthCheck(t *testing.T) {
	g := NewGateway(&GatewayConfig{ToolDispatchUseRegistry: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	result, err := g.ExecuteTool(context.Background(), "health_check", nil)
	if err != nil {
		t.Fatalf("health_check failed: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", result["status"])
	}
	if _, ok := result["time"]; !ok {
		t.Error("health_check missing time field")
	}
}

func TestBuiltinTools_ListTools(t *testing.T) {
	g := NewGateway(&GatewayConfig{ToolDispatchUseRegistry: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	result, err := g.ExecuteTool(context.Background(), "list_tools", nil)
	if err != nil {
		t.Fatalf("list_tools failed: %v", err)
	}

	tools, ok := result["tools"].([]mcp.ToolDefinition)
	if !ok {
		t.Fatalf("tools field missing or wrong type: %T", result["tools"])
	}
	if len(tools) < 2 {
		t.Errorf("expected at least 2 tools, got %d", len(tools))
	}
}

func TestLegacyHandlerMap(t *testing.T) {
	g := NewGateway(&GatewayConfig{ToolDispatchUseRegistry: false}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Register a tool via legacy map
	g.legacyMu.Lock()
	g.legacyHandlers["legacy_tool"] = func(ctx context.Context, args map[string]any) (map[string]any, error) {
		return map[string]any{"legacy": true}, nil
	}
	g.legacyMu.Unlock()

	result, err := g.ExecuteTool(context.Background(), "legacy_tool", nil)
	if err != nil {
		t.Fatalf("legacy ExecuteTool failed: %v", err)
	}

	if result["legacy"] != true {
		t.Errorf("expected legacy=true, got %v", result["legacy"])
	}
}